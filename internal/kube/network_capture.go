package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	// DefaultNetworkDebugImage is the immutable multi-architecture netshoot image used for Pod capture.
	DefaultNetworkDebugImage = "nicolaka/netshoot@sha256:b09d9b21381f47a79b3cbcb30da25266dc17186ea00ae65e99fdc51396f48e70"
	// NetworkDebugContainerName is the stable ephemeral-container name reused by live capture sessions.
	NetworkDebugContainerName = "kube-haggis-net"
	// NetworkCaptureMaxDuration bounds a live capture when the browser disappears without cleanup.
	NetworkCaptureMaxDuration = 10 * time.Minute
	// networkDebugContainerStartTimeout bounds image pull and ephemeral-container startup before reporting a setup failure.
	networkDebugContainerStartTimeout = 2 * time.Minute
)

var networkCaptureCommand = []string{"/bin/sh", "-c", networkCaptureShell}

const networkCaptureShell = `set +e
stats_pid=""
emit_stats() {
  rx=0; rxp=0; rxe=0; rxd=0; tx=0; txp=0; txe=0; txd=0
  while IFS= read -r line; do
    case "$line" in
      *:*)
        interface=${line%%:*}
        interface=${interface##* }
        [ "$interface" = "lo" ] && continue
        values=${line#*:}
        set -- $values
        [ "$#" -lt 12 ] && continue
        rx=$((rx + ${1:-0})); rxp=$((rxp + ${2:-0})); rxe=$((rxe + ${3:-0})); rxd=$((rxd + ${4:-0}))
        tx=$((tx + ${9:-0})); txp=$((txp + ${10:-0})); txe=$((txe + ${11:-0})); txd=$((txd + ${12:-0}))
        ;;
    esac
  done < /proc/net/dev
  printf 'S\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$rx" "$rxp" "$rxe" "$rxd" "$tx" "$txp" "$txe" "$txd"
}
emit_stats
while :; do
  emit_stats
  sleep 1
done &
stats_pid=$!
trap 'kill "$stats_pid" 2>/dev/null; exit 0' TERM INT HUP
tcpdump -i any -p -nn -tt -l -q 'ip or ip6' 2>&1 | while IFS= read -r line; do printf 'P\t%s\n' "$line"; done
status=$?
kill "$stats_pid" 2>/dev/null
exit "$status"`

// NetworkDebugContainerResult describes the reusable debug container selected for capture.
type NetworkDebugContainerResult struct {
	Name    string
	Image   string
	Created bool
}

// NetworkPodIdentity identifies a Pod IP that can be resolved in the live peer map.
type NetworkPodIdentity struct {
	IP        string `json:"ip"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// EnsureNetworkDebugContainer creates the stable capture container only when the Pod does not already have it.
func (s *Service) EnsureNetworkDebugContainer(ctx context.Context, contextName, namespace, podName, image string) (*NetworkDebugContainerResult, error) {
	namespace = strings.TrimSpace(namespace)
	podName = strings.TrimSpace(podName)
	image = strings.TrimSpace(image)
	if namespace == "" || namespace == "all" {
		return nil, fmt.Errorf("namespace is required for network capture")
	}
	if podName == "" {
		return nil, fmt.Errorf("Pod name is required for network capture")
	}
	if image == "" {
		return nil, fmt.Errorf("network debug image is required")
	}
	clientset, _, err := s.typedClient(contextName)
	if err != nil {
		return nil, err
	}
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("Kubernetes API timed out while reading the Pod for network capture; check cluster connectivity")
		}
		return nil, describePodReadError("read Pod for network capture", err)
	}
	if pod.Spec.OS != nil && strings.EqualFold(string(pod.Spec.OS.Name), "windows") {
		return nil, fmt.Errorf("network capture capability preflight unavailable: Windows Pods do not support the Linux tcpdump capture path")
	}
	if pod.Spec.HostNetwork {
		return nil, fmt.Errorf("network capture capability preflight unavailable: hostNetwork Pods do not have an isolated Pod network namespace")
	}
	if pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.RunAsNonRoot != nil && *pod.Spec.SecurityContext.RunAsNonRoot && image == DefaultNetworkDebugImage {
		return nil, fmt.Errorf("network capture capability preflight unavailable: the Pod requires non-root containers, but the default netshoot image runs as root; provide a non-root-compatible network debug image")
	}
	for _, existing := range pod.Spec.EphemeralContainers {
		if existing.Name == NetworkDebugContainerName {
			if failure := networkDebugContainerStatusFailure(ctx, clientset, namespace, podName); failure != nil {
				return nil, failure
			}
			if existing.Image != "" && existing.Image != image {
				return nil, fmt.Errorf("network debug container already exists with a different image")
			}
			if !networkDebugContainerHasCaptureCapability(existing) {
				return nil, fmt.Errorf("network capture capability preflight unavailable: the attached debug container does not declare NET_RAW; recreate the Pod before capturing")
			}
			return &NetworkDebugContainerResult{Name: existing.Name, Image: existing.Image}, nil
		}
	}
	pod.ManagedFields = nil
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, networkDebugContainerSpec(image))
	if _, err = clientset.CoreV1().Pods(namespace).UpdateEphemeralContainers(ctx, pod.Name, pod, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: "kube-haggis"}); err != nil {
		return nil, describeNetworkCapabilityPreflightError(err)
	}
	_, err = clientset.CoreV1().Pods(namespace).UpdateEphemeralContainers(ctx, pod.Name, pod, metav1.UpdateOptions{FieldManager: "kube-haggis"})
	if err != nil {
		return nil, describeEphemeralContainerUpdateError(err)
	}
	return &NetworkDebugContainerResult{Name: NetworkDebugContainerName, Image: image, Created: true}, nil
}

// networkDebugContainerSpec builds the stable ephemeral container with the minimum raw-socket capability.
func networkDebugContainerSpec(image string) corev1.EphemeralContainer {
	return corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
		Name:    NetworkDebugContainerName,
		Image:   image,
		Command: []string{"/bin/sh", "-c", "while :; do sleep 3600; done"},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_RAW"}},
		},
	}}
}

// networkDebugContainerHasCaptureCapability verifies an attached container explicitly retains raw packet access.
func networkDebugContainerHasCaptureCapability(container corev1.EphemeralContainer) bool {
	security := container.SecurityContext
	if security == nil {
		return false
	}
	if security.Privileged != nil && *security.Privileged {
		return true
	}
	if security.Capabilities == nil {
		return false
	}
	for _, capability := range security.Capabilities.Drop {
		if capability == corev1.Capability("NET_RAW") {
			return false
		}
	}
	for _, capability := range security.Capabilities.Add {
		if capability == corev1.Capability("NET_RAW") {
			return true
		}
	}
	return false
}

// describeNetworkCapabilityPreflightError converts dry-run admission failures into safe capability guidance.
func describeNetworkCapabilityPreflightError(err error) error {
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("network capture capability preflight denied by Kubernetes RBAC or Pod Security; NET_RAW is not permitted")
	case apierrors.IsMethodNotSupported(err), apierrors.IsNotAcceptable(err):
		return fmt.Errorf("network capture capability preflight is not supported by this Kubernetes API server")
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		return fmt.Errorf("network capture capability preflight rejected the requested NET_RAW capability")
	default:
		return fmt.Errorf("network capture capability preflight could not be completed; check cluster connectivity")
	}
}

// NetworkPodIdentities returns a bounded namespace-wide IP map for peer labels in the browser graph.
func (s *Service) NetworkPodIdentities(ctx context.Context, contextName, namespace string) (map[string]NetworkPodIdentity, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || namespace == "all" {
		return nil, fmt.Errorf("namespace is required for network peer discovery")
	}
	clientset, _, err := s.typedClient(contextName)
	if err != nil {
		return nil, err
	}
	list, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 2048})
	if err != nil {
		return nil, describePodReadError("list Pods for network peer discovery", err)
	}
	identities := make(map[string]NetworkPodIdentity)
	for _, item := range list.Items {
		ips := []string{item.Status.PodIP}
		for _, podIP := range item.Status.PodIPs {
			ips = append(ips, podIP.IP)
		}
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip == "" {
				continue
			}
			identities[ip] = NetworkPodIdentity{IP: ip, Name: item.Name, Namespace: item.Namespace}
		}
	}
	return identities, nil
}

// StreamPodNetworkCapture executes the fixed header-only capture command in the reusable debug container.
func (s *Service) StreamPodNetworkCapture(ctx context.Context, contextName, namespace, podName, container string, stdout, stderr io.Writer) error {
	namespace = strings.TrimSpace(namespace)
	podName = strings.TrimSpace(podName)
	container = strings.TrimSpace(container)
	if namespace == "" || namespace == "all" {
		return fmt.Errorf("namespace is required for network capture")
	}
	if podName == "" {
		return fmt.Errorf("Pod name is required for network capture")
	}
	if container == "" {
		return fmt.Errorf("network debug container is required")
	}
	clientset, restConfig, err := s.typedClient(contextName)
	if err != nil {
		return err
	}
	startupDeadline := time.NewTimer(networkDebugContainerStartTimeout)
	defer startupDeadline.Stop()
	for {
		err = streamPodNetworkCaptureOnce(ctx, clientset, restConfig, namespace, podName, container, stdout, stderr)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return fmt.Errorf("network capture was denied by Kubernetes RBAC: %w", err)
		}
		if !networkCaptureContainerStarting(err) {
			return fmt.Errorf("network capture stream failed: %w", err)
		}
		if failure := networkDebugContainerStatusFailure(ctx, clientset, namespace, podName); failure != nil {
			return failure
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-startupDeadline.C:
			return fmt.Errorf("the network debug container did not become ready before the startup wait expired; check image pull, Pod Security, and runtime events")
		case <-time.After(time.Second):
		}
	}
}

// streamPodNetworkCaptureOnce opens one native exec attempt for the fixed capture command.
func streamPodNetworkCaptureOnce(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, container string, stdout, stderr io.Writer) error {
	executor, err := newPodRemoteExecutor(restConfig, clientset, namespace, podName, container, networkCaptureCommand, false)
	if err != nil {
		return fmt.Errorf("create network capture executor: %w", err)
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: stdout, Stderr: stderr, Tty: false})
}

// networkCaptureContainerStarting reports errors that are safe to retry while the debug container starts.
func networkCaptureContainerStarting(err error) bool {
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"container is not running", "container not found", "container is waiting", "has not started", "pod sandbox"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// networkDebugContainerStatusFailure reads a short-lived kubelet waiting or terminated state without blocking capture startup.
func networkDebugContainerStatusFailure(ctx context.Context, clientset kubernetes.Interface, namespace, podName string) error {
	statusContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pod, err := clientset.CoreV1().Pods(namespace).Get(statusContext, podName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.Name != NetworkDebugContainerName {
			continue
		}
		if status.State.Terminated != nil {
			return fmt.Errorf("network debug container failed to start: the container terminated; inspect Pod Events and security settings")
		}
		if status.State.Waiting != nil {
			return networkDebugContainerWaitingFailure(status.State.Waiting.Reason, status.State.Waiting.Message)
		}
	}
	return nil
}

// networkDebugContainerWaitingFailure translates kubelet startup reasons into safe operator guidance.
func networkDebugContainerWaitingFailure(reason, message string) error {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "createcontainerconfigerror":
		lowerMessage := strings.ToLower(message)
		if strings.Contains(lowerMessage, "runasnonroot") || strings.Contains(lowerMessage, "non-numeric user") {
			return fmt.Errorf("network capture cannot start: the Pod requires a non-root container, but the configured debug image resolves to root; provide a non-root-compatible network debug image")
		}
		return fmt.Errorf("network capture cannot start: the kubelet rejected the debug-container configuration; inspect Pod Events and security settings")
	case "errimagepull", "imagepullbackoff":
		return fmt.Errorf("network capture cannot start: the debug image could not be pulled; check image access and cluster egress")
	case "crashloopbackoff", "runcontainererror", "createcontainererror":
		return fmt.Errorf("network capture cannot start: the debug container exited during startup; inspect Pod Events and security settings")
	case "containercreating", "":
		return nil
	default:
		return fmt.Errorf("network capture cannot start: the debug container is waiting on kubelet startup (%s); inspect Pod Events", strings.ToLower(strings.TrimSpace(reason)))
	}
}
