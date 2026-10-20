// Package kube provides discovery-driven access to Kubernetes clusters.
package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"
)

const (
	metricsGroupVersion          = "metrics.k8s.io/v1beta1"
	metricsStaleAfter            = 5 * time.Minute
	metricsDisplayLimit          = 8
	metricsStateAvailable        = "available"
	metricsStateUnavailable      = "unavailable"
	metricsStateEmpty            = "empty"
	metricsStatePermissionDenied = "permission-denied"
	metricsStateError            = "error"
	metricsStateStale            = "stale"
)

// ContextSummary describes a kubeconfig context in a form suitable for the UI.
type ContextSummary struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Server    string `json:"server"`
	IsCurrent bool   `json:"isCurrent"`
	Reachable bool   `json:"reachable"`
	LastError string `json:"lastError,omitempty"`
}

// ResourceDefinition describes a discovered Kubernetes resource kind.
type ResourceDefinition struct {
	Group          string   `json:"group"`
	Version        string   `json:"version"`
	Resource       string   `json:"resource"`
	Kind           string   `json:"kind"`
	Namespaced     bool     `json:"namespaced"`
	CustomResource bool     `json:"customResource"`
	Verbs          []string `json:"verbs"`
	ShortNames     []string `json:"shortNames,omitempty"`
	Categories     []string `json:"categories,omitempty"`
}

// ResourceSummary contains the common fields shown in resource tables.
type ResourceSummary struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	Kind            string            `json:"kind"`
	UID             string            `json:"uid"`
	CreatedAt       time.Time         `json:"createdAt"`
	Status          string            `json:"status"`
	Ready           string            `json:"ready,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	ResourceVersion string            `json:"resourceVersion"`
}

// ResourceList is the generic response returned for a resource collection.
type ResourceList struct {
	Definition      ResourceDefinition `json:"definition"`
	Items           []ResourceSummary  `json:"items"`
	Continue        string             `json:"continue,omitempty"`
	ResourceVersion string             `json:"resourceVersion"`
}

// NodeOverview summarizes node readiness and scheduling state.
type NodeOverview struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	NotReady int `json:"notReady"`
	Cordoned int `json:"cordoned"`
	Pressure int `json:"pressure"`
}

// WorkloadOverview summarizes the workload families used by the dashboard.
type WorkloadOverview struct {
	Deployments         int `json:"deployments"`
	HealthyDeployments  int `json:"healthyDeployments"`
	StatefulSets        int `json:"statefulSets"`
	HealthyStatefulSets int `json:"healthyStatefulSets"`
	DaemonSets          int `json:"daemonSets"`
	HealthyDaemonSets   int `json:"healthyDaemonSets"`
	Jobs                int `json:"jobs"`
	FailedJobs          int `json:"failedJobs"`
	CronJobs            int `json:"cronJobs"`
}

// PodOverview summarizes Pod phase and restart health.
type PodOverview struct {
	Total      int `json:"total"`
	Running    int `json:"running"`
	Pending    int `json:"pending"`
	Failed     int `json:"failed"`
	Restarting int `json:"restarting"`
}

// StorageOverview summarizes PersistentVolumeClaim phases.
type StorageOverview struct {
	Total   int `json:"total"`
	Bound   int `json:"bound"`
	Pending int `json:"pending"`
	Lost    int `json:"lost"`
}

// MetricUsage contains normalized CPU cores and memory bytes from metrics.k8s.io.
type MetricUsage struct {
	CPUCores    float64 `json:"cpuCores"`
	MemoryBytes int64   `json:"memoryBytes"`
}

// NodeMetric contains one node's native usage and capacity utilization.
type NodeMetric struct {
	Name          string      `json:"name"`
	Usage         MetricUsage `json:"usage"`
	CPUPercent    float64     `json:"cpuPercent"`
	MemoryPercent float64     `json:"memoryPercent"`
}

// PodMetric contains one Pod's usage after aggregating all of its containers.
type PodMetric struct {
	Name      string      `json:"name"`
	Namespace string      `json:"namespace,omitempty"`
	Usage     MetricUsage `json:"usage"`
}

// MetricsOverview contains native node and Pod usage plus explicit source states.
type MetricsOverview struct {
	State         string       `json:"state"`
	Message       string       `json:"message,omitempty"`
	UpdatedAt     *time.Time   `json:"updatedAt,omitempty"`
	NodeState     string       `json:"nodeState"`
	NodeMessage   string       `json:"nodeMessage,omitempty"`
	NodeUpdatedAt *time.Time   `json:"nodeUpdatedAt,omitempty"`
	NodeCount     int          `json:"nodeCount"`
	Nodes         []NodeMetric `json:"nodes"`
	PodState      string       `json:"podState"`
	PodMessage    string       `json:"podMessage,omitempty"`
	PodUpdatedAt  *time.Time   `json:"podUpdatedAt,omitempty"`
	PodCount      int          `json:"podCount"`
	Pods          []PodMetric  `json:"pods"`
}

// EventSummary contains the useful, compact portion of a Kubernetes Event.
type EventSummary struct {
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	Object    string    `json:"object"`
	Count     int64     `json:"count"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// ClusterOverview is the aggregate model used by the cluster landing page.
type ClusterOverview struct {
	Context          ContextSummary   `json:"context"`
	Namespaces       int              `json:"namespaces"`
	Nodes            NodeOverview     `json:"nodes"`
	Workloads        WorkloadOverview `json:"workloads"`
	Pods             PodOverview      `json:"pods"`
	Storage          StorageOverview  `json:"storage"`
	Warnings         []EventSummary   `json:"warnings"`
	MetricsAvailable bool             `json:"metricsAvailable"`
	Metrics          MetricsOverview  `json:"metrics"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

// KubeconfigReloadStatus reports safe kubeconfig source state without exposing file contents.
type KubeconfigReloadStatus struct {
	State    string `json:"state"`
	Revision uint64 `json:"revision"`
	Reloaded bool   `json:"reloaded"`
	Error    string `json:"error,omitempty"`
}

// ContextSnapshot contains the current kubeconfig contexts and safe reload state.
type ContextSnapshot struct {
	Contexts       []ContextSummary
	CurrentContext string
	Kubeconfig     KubeconfigReloadStatus
}

// configSourceFingerprint stores metadata for every kubeconfig source in loading order.
type configSourceFingerprint struct {
	Files []configFileFingerprint
}

// configFileFingerprint stores non-content metadata for one kubeconfig source.
type configFileFingerprint struct {
	Path          string
	State         uint8
	Size          int64
	ModTime       int64
	Mode          uint32
	TargetState   uint8
	TargetSize    int64
	TargetModTime int64
	TargetMode    uint32
}

// configLoadResult describes whether the current request observed a reload attempt.
type configLoadResult struct {
	Reloaded bool
	Failed   bool
}

// Service owns kubeconfig loading and creates dynamic clients for contexts.
type Service struct {
	kubeconfigPath  string
	mu              sync.Mutex
	rules           *clientcmd.ClientConfigLoadingRules
	config          *clientcmdapi.Config
	fingerprint     configSourceFingerprint
	loaded          bool
	revision        uint64
	lastReloadError string
}

const (
	kubeconfigStateReady   = "ready"
	kubeconfigStateChanged = "changed"
	kubeconfigStateError   = "error"
	sourceFileMissing      = 1
	sourceFilePresent      = 2
	sourceFileUnreadable   = 3
)

// NewService creates a Kubernetes service using standard kubeconfig rules.
func NewService(kubeconfigPath string) *Service {
	return &Service{kubeconfigPath: kubeconfigPath}
}

// Contexts returns all contexts and the kubeconfig current-context value.
func (s *Service) Contexts() (ContextSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, result, err := s.ensureConfigLocked(false)
	return s.contextSnapshotLocked(config, result, err), err
}

// ReloadContexts forces a safe kubeconfig reload and returns the resulting context snapshot.
func (s *Service) ReloadContexts() (ContextSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, result, err := s.ensureConfigLocked(true)
	return s.contextSnapshotLocked(config, result, err), err
}

// Discovery returns the preferred resource definitions advertised by a cluster.
func (s *Service) Discovery(ctx context.Context, contextName string) ([]ResourceDefinition, error) {
	dynamicClient, discoveryClient, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	definitions, discoveryErr := discoverDefinitions(discoveryClient)
	markCustomResourceDefinitions(ctx, dynamicClient, definitions)
	if discoveryErr != nil && len(definitions) == 0 {
		return nil, discoveryErr
	}
	return definitions, nil
}

// Overview gathers the data needed by the cluster landing page.
func (s *Service) Overview(ctx context.Context, contextName string) (ClusterOverview, error) {
	contextSnapshot, err := s.Contexts()
	if err != nil {
		return ClusterOverview{}, err
	}
	contexts := contextSnapshot.Contexts
	currentContext := contextSnapshot.CurrentContext
	if strings.TrimSpace(contextName) == "" {
		contextName = currentContext
	}
	contextSummary, found := findContext(contexts, contextName)
	if !found {
		return ClusterOverview{}, fmt.Errorf("context %q was not found", contextName)
	}

	dynamicClient, discoveryClient, _, err := s.clients(contextName)
	if err != nil {
		return ClusterOverview{}, err
	}

	nodes, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("nodes"))
	if err != nil {
		return ClusterOverview{}, err
	}
	namespaces, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("namespaces"))
	if err != nil {
		return ClusterOverview{}, err
	}
	pods, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("pods"))
	if err != nil {
		return ClusterOverview{}, err
	}
	deployments, err := s.listOverviewResource(ctx, dynamicClient, appsGVR("deployments"))
	if err != nil {
		return ClusterOverview{}, err
	}
	statefulSets, err := s.listOverviewResource(ctx, dynamicClient, appsGVR("statefulsets"))
	if err != nil {
		return ClusterOverview{}, err
	}
	daemonSets, err := s.listOverviewResource(ctx, dynamicClient, appsGVR("daemonsets"))
	if err != nil {
		return ClusterOverview{}, err
	}
	jobs, err := s.listOverviewResource(ctx, dynamicClient, batchGVR("jobs"))
	if err != nil {
		return ClusterOverview{}, err
	}
	cronJobs, err := s.listOverviewResource(ctx, dynamicClient, batchGVR("cronjobs"))
	if err != nil {
		return ClusterOverview{}, err
	}
	claims, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("persistentvolumeclaims"))
	if err != nil {
		return ClusterOverview{}, err
	}
	events, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("events"))
	if err != nil {
		return ClusterOverview{}, err
	}

	metricsAvailable := false
	metrics := unavailableMetricsOverview("metrics-server is not installed or metrics.k8s.io is not registered with this cluster.")
	if _, metricsErr := discoveryClient.ServerResourcesForGroupVersion(metricsGroupVersion); metricsErr == nil {
		metricsAvailable = true
		metrics = collectMetrics(ctx, dynamicClient, nodes.Items, time.Now().UTC())
	} else if apierrors.IsForbidden(metricsErr) {
		message := "Kubernetes RBAC denied discovery of metrics.k8s.io."
		metrics = unavailableMetricsOverview(message)
		metrics.State = metricsStatePermissionDenied
		metrics.Message = message
		metrics.NodeState = metricsStatePermissionDenied
		metrics.NodeMessage = message
		metrics.PodState = metricsStatePermissionDenied
		metrics.PodMessage = message
	} else if !apierrors.IsNotFound(metricsErr) {
		message := fmt.Sprintf("Could not discover metrics.k8s.io: %v", metricsErr)
		metrics = unavailableMetricsOverview(message)
		metrics.State = metricsStateError
		metrics.Message = message
		metrics.NodeState = metricsStateError
		metrics.NodeMessage = message
		metrics.PodState = metricsStateError
		metrics.PodMessage = message
	}

	return ClusterOverview{
		Context:          contextSummary,
		Namespaces:       len(namespaces.Items),
		Nodes:            buildNodeOverview(nodes.Items),
		Workloads:        buildWorkloadOverview(deployments.Items, statefulSets.Items, daemonSets.Items, jobs.Items, cronJobs.Items),
		Pods:             buildPodOverview(pods.Items),
		Storage:          buildStorageOverview(claims.Items),
		Warnings:         buildWarningEvents(events.Items),
		MetricsAvailable: metricsAvailable,
		Metrics:          metrics,
		UpdatedAt:        time.Now().UTC(),
	}, nil
}

// ListResources lists any discovered resource using a group-version-resource reference.
func (s *Service) ListResources(ctx context.Context, contextName, group, version, resource, namespace string, namespaced bool, limit int, labelSelector string) (ResourceList, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return ResourceList{}, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	options := metav1.ListOptions{LabelSelector: labelSelector}
	if limit > 0 {
		options.Limit = int64(limit)
	}
	items, err := listResource(ctx, dynamicClient, gvr, namespace, namespaced, options)
	if err != nil {
		return ResourceList{}, err
	}

	definition := ResourceDefinition{Group: group, Version: version, Resource: resource, Namespaced: namespaced}
	if definitions, discoveryErr := s.Discovery(ctx, contextName); discoveryErr == nil {
		for _, candidate := range definitions {
			if candidate.Group == group && candidate.Version == version && candidate.Resource == resource {
				definition = candidate
				break
			}
		}
	}

	result := ResourceList{
		Definition:      definition,
		Items:           make([]ResourceSummary, 0, len(items.Items)),
		Continue:        items.GetContinue(),
		ResourceVersion: items.GetResourceVersion(),
	}
	for index := range items.Items {
		result.Items = append(result.Items, summarizeResource(&items.Items[index]))
	}
	return result, nil
}

// WatchResources opens a Kubernetes watch for a discovered resource.
func (s *Service) WatchResources(ctx context.Context, contextName, group, version, resource, namespace string, namespaced bool, resourceVersion string) (watch.Interface, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	options := metav1.ListOptions{ResourceVersion: resourceVersion, AllowWatchBookmarks: true}
	return watchResource(ctx, dynamicClient, gvr, namespace, namespaced, options)
}

// GetResource returns the complete unstructured object for a resource name.
func (s *Service) GetResource(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (*unstructured.Unstructured, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	resourceClient, err := resourceInterface(dynamicClient, gvr, namespace, namespaced, false)
	if err != nil {
		return nil, err
	}
	return resourceClient.Get(ctx, name, metav1.GetOptions{})
}

// ApplyResource creates or client-side updates a YAML or JSON resource document.
func (s *Service) ApplyResource(ctx context.Context, contextName, group, version, resource, namespace string, namespaced, dryRun bool, body []byte) (*unstructured.Unstructured, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	var object map[string]interface{}
	if err := yaml.Unmarshal(body, &object); err != nil {
		return nil, fmt.Errorf("decode resource document: %w", err)
	}
	unstructured.RemoveNestedField(object, "metadata", "managedFields")
	unstructuredObject := &unstructured.Unstructured{Object: object}
	if namespace == "" || namespace == "all" {
		namespace = unstructuredObject.GetNamespace()
	}
	if namespaced && namespace == "" {
		return nil, fmt.Errorf("namespace is required for namespaced resources")
	}
	resourceClient, err := resourceInterface(dynamicClient, schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, namespace, namespaced, true)
	if err != nil {
		return nil, err
	}
	dryRunValues := []string(nil)
	if dryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	name := unstructuredObject.GetName()
	if name == "" {
		return resourceClient.Create(ctx, unstructuredObject, metav1.CreateOptions{DryRun: dryRunValues, FieldManager: "kube-haggis"})
	}
	if unstructuredObject.GetResourceVersion() == "" {
		current, getErr := resourceClient.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return nil, fmt.Errorf("read current resource version: %w", getErr)
		}
		unstructuredObject.SetResourceVersion(current.GetResourceVersion())
	}
	return resourceClient.Update(ctx, unstructuredObject, metav1.UpdateOptions{DryRun: dryRunValues, FieldManager: "kube-haggis"})
}

// DeleteResource deletes a resource with optional dry-run and force behavior.
func (s *Service) DeleteResource(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced, dryRun, force bool) error {
	return s.DeleteResourceWithOptions(ctx, contextName, group, version, resource, namespace, name, namespaced, ResourceDeleteOptions{DryRun: dryRun, Force: force})
}

// DeleteResourceWithOptions deletes one resource with optional dry-run, force, and resource-version preconditions.
func (s *Service) DeleteResourceWithOptions(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool, options ResourceDeleteOptions) error {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return err
	}
	return deleteResource(ctx, dynamicClient, schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, namespace, name, namespaced, options)
}

// PodLogs opens a native Kubernetes Pod log stream for one container.
func (s *Service) PodLogs(ctx context.Context, contextName, namespace, pod, container string, follow, previous bool, tailLines *int64) (io.ReadCloser, error) {
	_, _, restConfig, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create typed Kubernetes client: %w", err)
	}
	options := &corev1.PodLogOptions{
		Container:  container,
		Follow:     follow,
		Previous:   previous,
		TailLines:  tailLines,
		Timestamps: true,
	}
	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(pod, options).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("open logs for Pod %q: %w", pod, err)
	}
	return stream, nil
}

// PodSessionMode identifies the native Kubernetes Pod stream operation.
type PodSessionMode string

const (
	// PodSessionExec starts a command inside a running container.
	PodSessionExec PodSessionMode = "exec"
	// PodSessionAttach connects to the existing process streams in a container.
	PodSessionAttach PodSessionMode = "attach"
)

// StreamPodSession opens a TTY-backed native exec or attach session.
func (s *Service) StreamPodSession(ctx context.Context, contextName, namespace, pod, container string, mode PodSessionMode, command []string, stdin io.Reader, stdout io.Writer, sizes remotecommand.TerminalSizeQueue) error {
	if strings.TrimSpace(namespace) == "" || namespace == "all" {
		return fmt.Errorf("namespace is required for Pod sessions")
	}
	if strings.TrimSpace(pod) == "" {
		return fmt.Errorf("Pod name is required for Pod sessions")
	}

	discoveryMode := ""
	switch mode {
	case PodSessionExec:
		if len(command) == 0 {
			return fmt.Errorf("command is required for exec sessions")
		}
		discoveryMode = "exec"
	case PodSessionAttach:
		discoveryMode = "attach"
	default:
		return fmt.Errorf("unsupported Pod session mode %q", mode)
	}

	_, _, restConfig, err := s.clients(contextName)
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create typed Kubernetes client: %w", err)
	}
	request := clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource(discoveryMode)
	if mode == PodSessionExec {
		request.VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			TTY:       true,
		}, scheme.ParameterCodec)
	} else {
		request.VersionedParams(&corev1.PodAttachOptions{
			Container: container,
			Stdin:     true,
			Stdout:    true,
			TTY:       true,
		}, scheme.ParameterCodec)
	}

	executor, err := remotecommand.NewSPDYExecutor(restConfig, http.MethodPost, request.URL())
	if err != nil {
		return fmt.Errorf("create Pod %s executor: %w", mode, err)
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Tty:               true,
		TerminalSizeQueue: sizes,
	})
}

// EventsForResource returns Events whose involvedObject UID matches a resource.
func (s *Service) EventsForResource(ctx context.Context, contextName, namespace, uid string) ([]EventSummary, string, error) {
	if strings.TrimSpace(uid) == "" {
		return nil, "", fmt.Errorf("resource UID is required to list Events")
	}
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, "", err
	}
	items, err := listResource(ctx, dynamicClient, coreGVR("events"), namespace, strings.TrimSpace(namespace) != "", eventOptions(uid))
	if err != nil {
		return nil, "", err
	}
	return buildEventSummaries(items.Items, false), items.GetResourceVersion(), nil
}

// WatchEventsForResource opens an Event watch scoped to a resource UID.
func (s *Service) WatchEventsForResource(ctx context.Context, contextName, namespace, uid, resourceVersion string) (watch.Interface, error) {
	if strings.TrimSpace(uid) == "" {
		return nil, fmt.Errorf("resource UID is required to watch Events")
	}
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	options := eventOptions(uid)
	options.ResourceVersion = resourceVersion
	return watchResource(ctx, dynamicClient, coreGVR("events"), namespace, strings.TrimSpace(namespace) != "", options)
}

// loadConfig loads the merged kubeconfig from the configured loading rules.
func (s *Service) loadConfig() (*clientcmdapi.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, _, err := s.ensureConfigLocked(false)
	return config, err
}

// clients creates dynamic and discovery clients for one kubeconfig context.
func (s *Service) clients(contextName string) (dynamic.Interface, discovery.DiscoveryInterface, *rest.Config, error) {
	s.mu.Lock()
	config, _, err := s.ensureConfigLocked(false)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, nil, err
	}
	if strings.TrimSpace(contextName) == "" {
		contextName = config.CurrentContext
	}
	if contextName == "" {
		s.mu.Unlock()
		return nil, nil, nil, fmt.Errorf("kubeconfig has no current context")
	}
	if _, found := config.Contexts[contextName]; !found {
		s.mu.Unlock()
		return nil, nil, nil, fmt.Errorf("context %q was not found", contextName)
	}
	configCopy := *config
	rules := s.rules
	s.mu.Unlock()

	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	clientConfig := clientcmd.NewNonInteractiveClientConfig(configCopy, contextName, overrides, rules)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build client config for %q: %w", contextName, err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create discovery client: %w", err)
	}
	return dynamicClient, discoveryClient, restConfig, nil
}

// ensureConfigLocked loads a new snapshot when source metadata changes or a reload is forced.
func (s *Service) ensureConfigLocked(force bool) (*clientcmdapi.Config, configLoadResult, error) {
	rules := s.newLoadingRules()
	fingerprint := fingerprintLoadingRules(rules)
	if !force && s.loaded && s.config != nil && s.fingerprint.equal(fingerprint) {
		return s.config, configLoadResult{}, nil
	}

	config, err := rules.Load()
	if err != nil {
		safeErr := safeKubeconfigError(err)
		s.lastReloadError = safeErr.Error()
		if s.loaded && s.config != nil {
			return s.config, configLoadResult{Failed: true}, nil
		}
		return nil, configLoadResult{Failed: true}, safeErr
	}

	wasLoaded := s.loaded
	s.rules = rules
	s.config = config
	s.fingerprint = fingerprint
	s.loaded = true
	s.revision++
	s.lastReloadError = ""
	return config, configLoadResult{Reloaded: wasLoaded || force}, nil
}

// newLoadingRules constructs standard client-go rules for the current explicit path or environment.
func (s *Service) newLoadingRules() *clientcmd.ClientConfigLoadingRules {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if strings.TrimSpace(s.kubeconfigPath) != "" {
		rules.ExplicitPath = s.kubeconfigPath
	}
	return rules
}

// contextSnapshotLocked converts a loaded config into a UI-safe context snapshot.
func (s *Service) contextSnapshotLocked(config *clientcmdapi.Config, result configLoadResult, loadErr error) ContextSnapshot {
	status := KubeconfigReloadStatus{
		State:    kubeconfigStateReady,
		Revision: s.revision,
		Reloaded: result.Reloaded,
	}
	if result.Failed || loadErr != nil {
		status.State = kubeconfigStateError
		status.Error = s.lastReloadError
	}
	if result.Reloaded && status.State == kubeconfigStateReady {
		status.State = kubeconfigStateChanged
	}

	snapshot := ContextSnapshot{Kubeconfig: status}
	if config == nil {
		return snapshot
	}
	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	snapshot.Contexts = make([]ContextSummary, 0, len(names))
	for _, name := range names {
		snapshot.Contexts = append(snapshot.Contexts, summarizeContext(config, name))
	}
	snapshot.CurrentContext = config.CurrentContext
	return snapshot
}

// fingerprintLoadingRules records source metadata without reading source contents.
func fingerprintLoadingRules(rules *clientcmd.ClientConfigLoadingRules) configSourceFingerprint {
	paths := rules.GetLoadingPrecedence()
	fingerprint := configSourceFingerprint{Files: make([]configFileFingerprint, 0, len(paths))}
	for _, path := range paths {
		fingerprint.Files = append(fingerprint.Files, fingerprintConfigFile(path))
	}
	return fingerprint
}

// fingerprintConfigFile records the source and resolved target metadata for one path.
func fingerprintConfigFile(path string) configFileFingerprint {
	fingerprint := configFileFingerprint{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			fingerprint.State = sourceFileMissing
		} else {
			fingerprint.State = sourceFileUnreadable
		}
		return fingerprint
	}
	fingerprint.State = sourceFilePresent
	fingerprint.Size = info.Size()
	fingerprint.ModTime = info.ModTime().UnixNano()
	fingerprint.Mode = uint32(info.Mode())

	target, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			fingerprint.TargetState = sourceFileMissing
		} else {
			fingerprint.TargetState = sourceFileUnreadable
		}
		return fingerprint
	}
	fingerprint.TargetState = sourceFilePresent
	fingerprint.TargetSize = target.Size()
	fingerprint.TargetModTime = target.ModTime().UnixNano()
	fingerprint.TargetMode = uint32(target.Mode())
	return fingerprint
}

// equal reports whether two source fingerprints contain identical metadata and ordering.
func (fingerprint configSourceFingerprint) equal(other configSourceFingerprint) bool {
	if len(fingerprint.Files) != len(other.Files) {
		return false
	}
	for index := range fingerprint.Files {
		if fingerprint.Files[index] != other.Files[index] {
			return false
		}
	}
	return true
}

// safeKubeconfigError prevents source paths and file contents from reaching the browser.
func safeKubeconfigError(error) error {
	return errors.New("load kubeconfig: configured source files could not be read or parsed")
}

// summarizeContext converts raw kubeconfig data into a UI-safe context summary.
func summarizeContext(config *clientcmdapi.Config, name string) ContextSummary {
	contextEntry := config.Contexts[name]
	if contextEntry == nil {
		return ContextSummary{Name: name, IsCurrent: config.CurrentContext == name}
	}
	cluster := config.Clusters[contextEntry.Cluster]
	server := ""
	if cluster != nil {
		server = safeServerAddress(cluster.Server)
	}
	return ContextSummary{
		Name:      name,
		Cluster:   contextEntry.Cluster,
		User:      contextEntry.AuthInfo,
		Server:    server,
		IsCurrent: config.CurrentContext == name,
	}
}

// safeServerAddress removes optional URL user information from a context summary.
func safeServerAddress(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User == nil {
		return address
	}
	parsed.User = nil
	return parsed.String()
}

// findContext finds a context summary by kubeconfig context name.
func findContext(contexts []ContextSummary, name string) (ContextSummary, bool) {
	for _, item := range contexts {
		if item.Name == name {
			return item, true
		}
	}
	return ContextSummary{}, false
}

// coreGVR builds a GroupVersionResource for a core API resource.
func coreGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Version: "v1", Resource: resource}
}

// appsGVR builds a GroupVersionResource for an apps API resource.
func appsGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resource}
}

// batchGVR builds a GroupVersionResource for a batch API resource.
func batchGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: resource}
}

// listOverviewResource lists an all-namespace resource for dashboard aggregation.
func (s *Service) listOverviewResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	items, err := client.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil && apierrors.IsNotFound(err) {
		return &unstructured.UnstructuredList{}, nil
	}
	return items, err
}

// listResource lists a namespaced or cluster-scoped resource according to discovery.
func listResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced bool, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if namespaced && strings.TrimSpace(namespace) != "" && namespace != "all" {
		return client.Resource(gvr).Namespace(namespace).List(ctx, options)
	}
	return client.Resource(gvr).List(ctx, options)
}

// watchResource opens a namespaced or cluster-scoped watch.
func watchResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced bool, options metav1.ListOptions) (watch.Interface, error) {
	if namespaced && strings.TrimSpace(namespace) != "" && namespace != "all" {
		return client.Resource(gvr).Namespace(namespace).Watch(ctx, options)
	}
	return client.Resource(gvr).Watch(ctx, options)
}

// resourceInterface returns the dynamic resource client for a mutation or get.
func resourceInterface(client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced, requireNamespace bool) (dynamic.ResourceInterface, error) {
	if namespaced && requireNamespace && (strings.TrimSpace(namespace) == "" || namespace == "all") {
		return nil, fmt.Errorf("namespace is required for namespaced resources")
	}
	if namespaced && strings.TrimSpace(namespace) != "" && namespace != "all" {
		return client.Resource(gvr).Namespace(namespace), nil
	}
	return client.Resource(gvr), nil
}

// deleteResource sends one generic delete with Kubernetes dry-run and concurrency options.
func deleteResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string, namespaced bool, options ResourceDeleteOptions) error {
	resourceClient, err := resourceInterface(client, gvr, namespace, namespaced, true)
	if err != nil {
		return err
	}
	deleteOptions := metav1.DeleteOptions{}
	if options.DryRun {
		deleteOptions.DryRun = []string{metav1.DryRunAll}
	}
	if options.Force {
		gracePeriod := int64(0)
		deleteOptions.GracePeriodSeconds = &gracePeriod
	}
	if resourceVersion := strings.TrimSpace(options.ExpectedResourceVersion); resourceVersion != "" {
		deleteOptions.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return resourceClient.Delete(ctx, name, deleteOptions)
}

// metricsSourceResult contains one metrics.k8s.io collection result before it is added to the overview.
type metricsSourceResult struct {
	state     string
	message   string
	updatedAt time.Time
	count     int
	nodeItems []NodeMetric
	podItems  []PodMetric
}

// metricsGVR builds a GroupVersionResource for a metrics.k8s.io resource.
func metricsGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: resource}
}

// unavailableMetricsOverview creates a consistent response when metrics-server is absent.
func unavailableMetricsOverview(message string) MetricsOverview {
	return MetricsOverview{
		State:       metricsStateUnavailable,
		Message:     message,
		NodeState:   metricsStateUnavailable,
		NodeMessage: message,
		Nodes:       []NodeMetric{},
		PodState:    metricsStateUnavailable,
		PodMessage:  message,
		Pods:        []PodMetric{},
	}
}

// collectMetrics reads native node and Pod metrics through the existing dynamic client.
func collectMetrics(ctx context.Context, client dynamic.Interface, nodes []unstructured.Unstructured, now time.Time) MetricsOverview {
	nodeResult := collectNodeMetrics(ctx, client, nodes, now)
	podResult := collectPodMetrics(ctx, client, now)
	state, message := summarizeMetricsState(nodeResult, podResult)
	nodeItems := nodeResult.nodeItems
	if nodeItems == nil {
		nodeItems = []NodeMetric{}
	}
	podItems := podResult.podItems
	if podItems == nil {
		podItems = []PodMetric{}
	}
	updatedAt := nodeResult.updatedAt
	if podResult.updatedAt.After(updatedAt) {
		updatedAt = podResult.updatedAt
	}
	return MetricsOverview{
		State:         state,
		Message:       message,
		UpdatedAt:     metricTimePointer(updatedAt),
		NodeState:     nodeResult.state,
		NodeMessage:   nodeResult.message,
		NodeUpdatedAt: metricTimePointer(nodeResult.updatedAt),
		NodeCount:     nodeResult.count,
		Nodes:         nodeItems,
		PodState:      podResult.state,
		PodMessage:    podResult.message,
		PodUpdatedAt:  metricTimePointer(podResult.updatedAt),
		PodCount:      podResult.count,
		Pods:          podItems,
	}
}

// metricTimePointer omits zero timestamps from metrics API responses.
func metricTimePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

// collectNodeMetrics reads and normalizes the node metrics collection.
func collectNodeMetrics(ctx context.Context, client dynamic.Interface, nodes []unstructured.Unstructured, now time.Time) metricsSourceResult {
	items, err := client.Resource(metricsGVR("nodes")).List(ctx, metav1.ListOptions{})
	if err != nil {
		return metricsSourceError(err, "node")
	}
	parsed, updatedAt, err := parseNodeMetrics(items.Items, nodeCapacities(nodes))
	if err != nil {
		return metricsSourceResult{state: metricsStateError, message: err.Error()}
	}
	state, message := metricSampleState(len(parsed), updatedAt, now, "node")
	return metricsSourceResult{
		state:     state,
		message:   message,
		updatedAt: updatedAt,
		count:     len(parsed),
		nodeItems: topNodeMetrics(parsed),
	}
}

// collectPodMetrics reads and aggregates the Pod metrics collection.
func collectPodMetrics(ctx context.Context, client dynamic.Interface, now time.Time) metricsSourceResult {
	items, err := client.Resource(metricsGVR("pods")).List(ctx, metav1.ListOptions{})
	if err != nil {
		return metricsSourceError(err, "Pod")
	}
	parsed, updatedAt, err := parsePodMetrics(items.Items)
	if err != nil {
		return metricsSourceResult{state: metricsStateError, message: err.Error()}
	}
	state, message := metricSampleState(len(parsed), updatedAt, now, "Pod")
	return metricsSourceResult{
		state:     state,
		message:   message,
		updatedAt: updatedAt,
		count:     len(parsed),
		podItems:  topPodMetrics(parsed),
	}
}

// metricsSourceError classifies a metrics collection error without failing the whole overview.
func metricsSourceError(err error, resourceName string) metricsSourceResult {
	state := metricsStateError
	message := fmt.Sprintf("Could not read %s metrics from metrics.k8s.io: %v", resourceName, err)
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		state = metricsStatePermissionDenied
		message = fmt.Sprintf("Kubernetes RBAC denied access to %s metrics.", resourceName)
	} else if apierrors.IsNotFound(err) {
		state = metricsStateUnavailable
		message = fmt.Sprintf("metrics.k8s.io does not expose %s metrics on this cluster.", resourceName)
	}
	return metricsSourceResult{state: state, message: message}
}

// summarizeMetricsState derives an overall state while preserving per-source failures.
func summarizeMetricsState(nodes, pods metricsSourceResult) (string, string) {
	if nodes.count > 0 || pods.count > 0 {
		if nodes.state == metricsStateStale || pods.state == metricsStateStale {
			return metricsStateStale, "At least one native metrics source returned a stale sample."
		}
		if nodes.state == metricsStateError || nodes.state == metricsStatePermissionDenied || pods.state == metricsStateError || pods.state == metricsStatePermissionDenied {
			return metricsStateAvailable, "Some metrics sources could not be read; each chart reports its own state."
		}
		return metricsStateAvailable, "Native usage collected from metrics.k8s.io."
	}
	if nodes.state == metricsStatePermissionDenied && pods.state == metricsStatePermissionDenied {
		return metricsStatePermissionDenied, "Kubernetes RBAC denied access to node and Pod metrics."
	}
	if nodes.state == metricsStateError || pods.state == metricsStateError {
		return metricsStateError, "metrics.k8s.io responded, but usage could not be read."
	}
	if nodes.state == metricsStateUnavailable || pods.state == metricsStateUnavailable {
		return metricsStateUnavailable, "metrics.k8s.io does not expose the usage resources required by this overview."
	}
	return metricsStateEmpty, "metrics.k8s.io is available, but it returned no usage samples."
}

// metricSampleState classifies a successful metrics collection by freshness and size.
func metricSampleState(count int, updatedAt, now time.Time, resourceName string) (string, string) {
	if count == 0 {
		return metricsStateEmpty, fmt.Sprintf("No %s usage samples were returned by metrics.k8s.io.", resourceName)
	}
	if updatedAt.IsZero() {
		return metricsStateError, fmt.Sprintf("The %s metrics response did not include a sample timestamp.", resourceName)
	}
	if now.After(updatedAt.Add(metricsStaleAfter)) {
		return metricsStateStale, fmt.Sprintf("The latest %s sample is %s old; values may be stale.", resourceName, now.Sub(updatedAt).Round(time.Second))
	}
	return metricsStateAvailable, fmt.Sprintf("Current %s usage from metrics.k8s.io.", resourceName)
}

// parseNodeMetrics converts metrics.k8s.io node objects into chart-ready values.
func parseNodeMetrics(items []unstructured.Unstructured, capacities map[string]MetricUsage) ([]NodeMetric, time.Time, error) {
	metrics := make([]NodeMetric, 0, len(items))
	var updatedAt time.Time
	for index := range items {
		item := &items[index]
		if item.GetName() == "" {
			return nil, time.Time{}, fmt.Errorf("node metrics item %d has no name", index)
		}
		usage, err := parseMetricUsage(item.Object, fmt.Sprintf("node %q", item.GetName()))
		if err != nil {
			return nil, time.Time{}, err
		}
		timestamp, err := parseMetricTimestamp(item.Object, fmt.Sprintf("node %q", item.GetName()))
		if err != nil {
			return nil, time.Time{}, err
		}
		capacity := capacities[item.GetName()]
		metrics = append(metrics, NodeMetric{
			Name:          item.GetName(),
			Usage:         usage,
			CPUPercent:    metricUtilization(usage.CPUCores, capacity.CPUCores),
			MemoryPercent: metricUtilization(float64(usage.MemoryBytes), float64(capacity.MemoryBytes)),
		})
		if timestamp.After(updatedAt) {
			updatedAt = timestamp
		}
	}
	return metrics, updatedAt, nil
}

// parsePodMetrics converts Pod metrics into one aggregate usage value per Pod.
func parsePodMetrics(items []unstructured.Unstructured) ([]PodMetric, time.Time, error) {
	metrics := make([]PodMetric, 0, len(items))
	var updatedAt time.Time
	for index := range items {
		item := &items[index]
		if item.GetName() == "" {
			return nil, time.Time{}, fmt.Errorf("Pod metrics item %d has no name", index)
		}
		containers, found, err := unstructured.NestedSlice(item.Object, "containers")
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("read containers for Pod %q: %w", item.GetName(), err)
		}
		if !found {
			return nil, time.Time{}, fmt.Errorf("Pod %q metrics has no containers", item.GetName())
		}
		usage := MetricUsage{}
		for containerIndex, rawContainer := range containers {
			container, ok := rawContainer.(map[string]interface{})
			if !ok {
				return nil, time.Time{}, fmt.Errorf("Pod %q container %d is malformed", item.GetName(), containerIndex)
			}
			containerUsage, usageErr := parseMetricUsage(container, fmt.Sprintf("Pod %q container %d", item.GetName(), containerIndex))
			if usageErr != nil {
				return nil, time.Time{}, usageErr
			}
			usage.CPUCores += containerUsage.CPUCores
			usage.MemoryBytes += containerUsage.MemoryBytes
		}
		timestamp, err := parseMetricTimestamp(item.Object, fmt.Sprintf("Pod %q", item.GetName()))
		if err != nil {
			return nil, time.Time{}, err
		}
		metrics = append(metrics, PodMetric{Name: item.GetName(), Namespace: item.GetNamespace(), Usage: usage})
		if timestamp.After(updatedAt) {
			updatedAt = timestamp
		}
	}
	return metrics, updatedAt, nil
}

// parseMetricUsage reads CPU and memory quantities from a metrics object.
func parseMetricUsage(object map[string]interface{}, owner string) (MetricUsage, error) {
	usage, found, err := unstructured.NestedMap(object, "usage")
	if err != nil {
		return MetricUsage{}, fmt.Errorf("read usage for %s: %w", owner, err)
	}
	if !found {
		return MetricUsage{}, fmt.Errorf("%s has no usage values", owner)
	}
	return parseMetricUsageMap(usage, owner)
}

// parseMetricUsageMap normalizes CPU and memory resource.Quantity strings.
func parseMetricUsageMap(values map[string]interface{}, owner string) (MetricUsage, error) {
	cpu, err := parseMetricQuantity(values["cpu"], owner+" CPU")
	if err != nil {
		return MetricUsage{}, err
	}
	memory, err := parseMetricQuantity(values["memory"], owner+" memory")
	if err != nil {
		return MetricUsage{}, err
	}
	return MetricUsage{CPUCores: cpu.AsApproximateFloat64(), MemoryBytes: memory.Value()}, nil
}

// parseMetricQuantity parses one metrics.k8s.io quantity while rejecting malformed JSON values.
func parseMetricQuantity(value interface{}, field string) (resource.Quantity, error) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return resource.Quantity{}, fmt.Errorf("%s must be a non-empty resource quantity", field)
	}
	quantity, err := resource.ParseQuantity(text)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("parse %s: %w", field, err)
	}
	return quantity, nil
}

// parseMetricTimestamp reads the RFC3339 sample timestamp returned by metrics.k8s.io.
func parseMetricTimestamp(object map[string]interface{}, owner string) (time.Time, error) {
	timestamp, found, err := unstructured.NestedString(object, "timestamp")
	if err != nil {
		return time.Time{}, fmt.Errorf("read timestamp for %s: %w", owner, err)
	}
	if !found || strings.TrimSpace(timestamp) == "" {
		return time.Time{}, fmt.Errorf("%s has no sample timestamp", owner)
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp for %s: %w", owner, err)
	}
	return parsed.UTC(), nil
}

// nodeCapacities extracts CPU and memory capacity used for node utilization percentages.
func nodeCapacities(items []unstructured.Unstructured) map[string]MetricUsage {
	capacities := make(map[string]MetricUsage, len(items))
	for index := range items {
		item := &items[index]
		rawCapacity, found, err := unstructured.NestedMap(item.Object, "status", "capacity")
		if err != nil || !found {
			continue
		}
		capacity, err := parseMetricUsageMap(rawCapacity, fmt.Sprintf("node %q capacity", item.GetName()))
		if err != nil {
			continue
		}
		capacities[item.GetName()] = capacity
	}
	return capacities
}

// metricUtilization returns usage as a percentage of a positive capacity.
func metricUtilization(usage, capacity float64) float64 {
	if usage <= 0 || capacity <= 0 {
		return 0
	}
	return usage / capacity * 100
}

// topNodeMetrics sorts node metrics by CPU usage and limits dashboard payload size.
func topNodeMetrics(items []NodeMetric) []NodeMetric {
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].Usage.CPUCores == items[right].Usage.CPUCores {
			return items[left].Name < items[right].Name
		}
		return items[left].Usage.CPUCores > items[right].Usage.CPUCores
	})
	if len(items) > metricsDisplayLimit {
		return items[:metricsDisplayLimit]
	}
	return items
}

// topPodMetrics sorts Pod metrics by CPU usage and limits dashboard payload size.
func topPodMetrics(items []PodMetric) []PodMetric {
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].Usage.CPUCores == items[right].Usage.CPUCores {
			if items[left].Namespace == items[right].Namespace {
				return items[left].Name < items[right].Name
			}
			return items[left].Namespace < items[right].Namespace
		}
		return items[left].Usage.CPUCores > items[right].Usage.CPUCores
	})
	if len(items) > metricsDisplayLimit {
		return items[:metricsDisplayLimit]
	}
	return items
}

// summarizeResource extracts common fields from an unstructured resource.
func summarizeResource(item *unstructured.Unstructured) ResourceSummary {
	return ResourceSummary{
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		Kind:            item.GetKind(),
		UID:             string(item.GetUID()),
		CreatedAt:       item.GetCreationTimestamp().Time.UTC(),
		Status:          resourceStatus(item),
		Ready:           resourceReady(item),
		Labels:          item.GetLabels(),
		ResourceVersion: item.GetResourceVersion(),
	}
}

// resourceStatus returns a compact status string for common resource shapes.
func resourceStatus(item *unstructured.Unstructured) string {
	if phase, found, _ := unstructured.NestedString(item.Object, "status", "phase"); found && phase != "" {
		return phase
	}
	if conditions, found, _ := unstructured.NestedSlice(item.Object, "status", "conditions"); found {
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]interface{})
			if !ok {
				continue
			}
			conditionType, _, _ := unstructured.NestedString(condition, "type")
			conditionStatus, _, _ := unstructured.NestedString(condition, "status")
			if conditionType == "Ready" || conditionType == "Available" || conditionType == "Progressing" {
				return conditionStatus
			}
		}
	}
	return "Active"
}

// resourceReady returns a ready/desired pair for common workload resources.
func resourceReady(item *unstructured.Unstructured) string {
	ready := nestedInt64(item.Object, "status", "readyReplicas")
	if ready == 0 {
		ready = nestedInt64(item.Object, "status", "availableReplicas")
	}
	desired := nestedInt64(item.Object, "spec", "replicas")
	if desired > 0 || ready > 0 {
		return fmt.Sprintf("%d/%d", ready, desired)
	}
	return ""
}

// nestedInt64 reads an integer field from an unstructured object.
func nestedInt64(object map[string]interface{}, fields ...string) int64 {
	value, found, _ := unstructured.NestedInt64(object, fields...)
	if !found {
		return 0
	}
	return value
}

// buildNodeOverview calculates node health counters.
func buildNodeOverview(items []unstructured.Unstructured) NodeOverview {
	overview := NodeOverview{Total: len(items)}
	for index := range items {
		item := &items[index]
		unschedulable, _, _ := unstructured.NestedBool(item.Object, "spec", "unschedulable")
		if unschedulable {
			overview.Cordoned++
		}
		ready := false
		pressure := false
		conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]interface{})
			if !ok {
				continue
			}
			conditionType, _, _ := unstructured.NestedString(condition, "type")
			conditionStatus, _, _ := unstructured.NestedString(condition, "status")
			if conditionType == "Ready" && conditionStatus == "True" {
				ready = true
			}
			if strings.HasSuffix(conditionType, "Pressure") && conditionStatus == "True" {
				pressure = true
			}
		}
		if ready {
			overview.Ready++
		} else {
			overview.NotReady++
		}
		if pressure {
			overview.Pressure++
		}
	}
	return overview
}

// buildWorkloadOverview calculates workload health counters.
func buildWorkloadOverview(deployments, statefulSets, daemonSets, jobs, cronJobs []unstructured.Unstructured) WorkloadOverview {
	overview := WorkloadOverview{
		Deployments:  len(deployments),
		StatefulSets: len(statefulSets),
		DaemonSets:   len(daemonSets),
		Jobs:         len(jobs),
		CronJobs:     len(cronJobs),
	}
	for index := range deployments {
		item := &deployments[index]
		if nestedInt64(item.Object, "status", "availableReplicas") >= nestedInt64(item.Object, "spec", "replicas") {
			overview.HealthyDeployments++
		}
	}
	for index := range statefulSets {
		item := &statefulSets[index]
		if nestedInt64(item.Object, "status", "readyReplicas") >= nestedInt64(item.Object, "spec", "replicas") {
			overview.HealthyStatefulSets++
		}
	}
	for index := range daemonSets {
		item := &daemonSets[index]
		if nestedInt64(item.Object, "status", "numberReady") >= nestedInt64(item.Object, "status", "desiredNumberScheduled") {
			overview.HealthyDaemonSets++
		}
	}
	for index := range jobs {
		item := &jobs[index]
		if nestedInt64(item.Object, "status", "failed") > 0 {
			overview.FailedJobs++
		}
	}
	return overview
}

// buildPodOverview calculates Pod phase and restart counters.
func buildPodOverview(items []unstructured.Unstructured) PodOverview {
	overview := PodOverview{Total: len(items)}
	for index := range items {
		item := &items[index]
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		switch phase {
		case "Running":
			overview.Running++
		case "Pending":
			overview.Pending++
		case "Failed":
			overview.Failed++
		}
		statuses, _, _ := unstructured.NestedSlice(item.Object, "status", "containerStatuses")
		restarts := int64(0)
		for _, rawStatus := range statuses {
			status, ok := rawStatus.(map[string]interface{})
			if !ok {
				continue
			}
			restarts += nestedInt64(status, "restartCount")
		}
		if restarts > 0 {
			overview.Restarting++
		}
	}
	return overview
}

// buildStorageOverview calculates PersistentVolumeClaim phase counters.
func buildStorageOverview(items []unstructured.Unstructured) StorageOverview {
	overview := StorageOverview{Total: len(items)}
	for index := range items {
		phase, _, _ := unstructured.NestedString(items[index].Object, "status", "phase")
		switch phase {
		case "Bound":
			overview.Bound++
		case "Pending":
			overview.Pending++
		case "Lost":
			overview.Lost++
		}
	}
	return overview
}

// buildWarningEvents selects and sorts recent warning Events.
func buildWarningEvents(items []unstructured.Unstructured) []EventSummary {
	warnings := buildEventSummaries(items, true)
	if len(warnings) > 8 {
		warnings = warnings[:8]
	}
	return warnings
}

// eventOptions creates the native field selector used for resource Events.
func eventOptions(uid string) metav1.ListOptions {
	return metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", uid).String(), Limit: 100}
}

// buildEventSummaries converts unstructured Events into UI summaries.
func buildEventSummaries(items []unstructured.Unstructured, warningsOnly bool) []EventSummary {
	events := make([]EventSummary, 0, len(items))
	for index := range items {
		event := summarizeEvent(&items[index])
		if warningsOnly && event.Type != "Warning" {
			continue
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].LastSeen.After(events[j].LastSeen) })
	return events
}

// summarizeEvent extracts the display fields from one Kubernetes Event.
func summarizeEvent(item *unstructured.Unstructured) EventSummary {
	eventType, _, _ := unstructured.NestedString(item.Object, "type")
	reason, _, _ := unstructured.NestedString(item.Object, "reason")
	message, _, _ := unstructured.NestedString(item.Object, "message")
	objectName := ""
	involvedObject, _, _ := unstructured.NestedMap(item.Object, "involvedObject")
	if involvedObject != nil {
		kind, _, _ := unstructured.NestedString(involvedObject, "kind")
		name, _, _ := unstructured.NestedString(involvedObject, "name")
		objectName = kind + "/" + name
	}
	count := nestedInt64(item.Object, "count")
	if count < 1 {
		count = 1
	}
	return EventSummary{
		Type:      eventType,
		Reason:    reason,
		Message:   message,
		Name:      item.GetName(),
		Namespace: item.GetNamespace(),
		Object:    objectName,
		Count:     count,
		FirstSeen: eventTimeForFields(item, "firstTimestamp", "eventTime", "lastTimestamp"),
		LastSeen:  eventTimeForFields(item, "lastTimestamp", "eventTime", "firstTimestamp"),
	}
}

// eventTime extracts the best available timestamp from an Event.
func eventTime(item *unstructured.Unstructured) time.Time {
	return eventTimeForFields(item, "lastTimestamp", "eventTime", "firstTimestamp")
}

// eventTimeForFields returns the first parseable timestamp from supplied fields.
func eventTimeForFields(item *unstructured.Unstructured, fields ...string) time.Time {
	for _, field := range fields {
		value, found, _ := unstructured.NestedString(item.Object, field)
		if !found || value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
	}
	return item.GetCreationTimestamp().Time.UTC()
}
