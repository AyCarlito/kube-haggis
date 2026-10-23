package kube

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/client-go/kubernetes/scheme"
)

// TestValidateEphemeralContainerRequestRejectsUnsafeInputs verifies debug-container validation before client creation.
func TestValidateEphemeralContainerRequestRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name    string
		ns      string
		pod     string
		request EphemeralContainerRequest
		want    string
	}{
		{name: "missing namespace", pod: "api", request: EphemeralContainerRequest{Name: "debugger", Image: "busybox"}, want: "namespace is required"},
		{name: "missing Pod", ns: "default", request: EphemeralContainerRequest{Name: "debugger", Image: "busybox"}, want: "Pod name is required"},
		{name: "missing name", ns: "default", pod: "api", request: EphemeralContainerRequest{Image: "busybox"}, want: "debug container name is required"},
		{name: "missing image", ns: "default", pod: "api", request: EphemeralContainerRequest{Name: "debugger"}, want: "debug container image is required"},
		{name: "invalid command argument", ns: "default", pod: "api", request: EphemeralContainerRequest{Name: "debugger", Image: "busybox", Command: []string{"/bin/sh", ""}}, want: "command argument 2 is empty"},
		{name: "invalid target", ns: "default", pod: "api", request: EphemeralContainerRequest{Name: "debugger", Image: "busybox", TargetContainerName: "not valid"}, want: "target container name is invalid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateEphemeralContainerRequest(test.ns, test.pod, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// TestValidatePodCopyPathRejectsUnsafeRemotePaths verifies copy paths cannot select directories or traversal segments.
func TestValidatePodCopyPathRejectsUnsafeRemotePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "missing namespace", path: "/tmp/file.txt", want: "namespace is required"},
		{name: "relative path", path: "tmp/file.txt", want: "must be absolute"},
		{name: "parent segment", path: "/tmp/../file.txt", want: "must not contain"},
		{name: "directory path", path: "/tmp/", want: "must identify a file"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validatePodCopyPath("", "api", "", test.path)
			if test.name != "missing namespace" {
				_, err = validatePodCopyPath("default", "api", "", test.path)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}

	parsed, err := validatePodCopyPath("default", "api", "app", "/var/log/app.log")
	if err != nil {
		t.Fatalf("valid copy path rejected: %v", err)
	}
	if parsed.directory != "/var/log" || parsed.name != "app.log" {
		t.Fatalf("unexpected parsed path: %+v", parsed)
	}
}

// TestWritePodTarArchiveProducesOneRegularFile verifies the upload archive sent over pods/exec.
func TestWritePodTarArchiveProducesOneRegularFile(t *testing.T) {
	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go writePodTarArchive(writer, strings.NewReader("hello kube"), int64(len("hello kube")), "config.txt", result)

	archive := tar.NewReader(reader)
	header, err := archive.Next()
	if err != nil {
		t.Fatalf("read tar header: %v", err)
	}
	content, err := io.ReadAll(archive)
	if err != nil {
		t.Fatalf("read tar content: %v", err)
	}
	if header.Name != "config.txt" || !isRegularTarEntry(header) || string(content) != "hello kube" {
		t.Fatalf("unexpected tar entry: name=%q type=%d content=%q", header.Name, header.Typeflag, content)
	}
	if _, err := archive.Next(); err != io.EOF {
		t.Fatalf("expected one tar entry, got %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("write tar archive: %v", err)
	}
}

// TestAddEphemeralContainerUsesNativeSubresource verifies the typed client request and fields.
func TestAddEphemeralContainerUsesNativeSubresource(t *testing.T) {
	var update corev1.Pod
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/namespaces/default/pods/api" && request.Method == http.MethodGet {
			writeTestPod(t, writer, &corev1.Pod{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "7", ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "server"}}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "app:latest"}}},
			})
			return
		}
		if request.URL.Path == "/api/v1/namespaces/default/pods/api/ephemeralcontainers" && request.Method == http.MethodPut {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Logf("update read error: %v", readErr)
				http.Error(writer, readErr.Error(), http.StatusBadRequest)
				return
			}
			decoded, _, err := protobuf.NewSerializer(scheme.Scheme, scheme.Scheme).Decode(body, nil, &corev1.Pod{})
			if err != nil {
				t.Logf("update decode error: %v", err)
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			update = *decoded.(*corev1.Pod)
			writeTestPod(t, writer, &update)
			return
		}
		t.Logf("unexpected request: %s %s", request.Method, request.URL.Path)
		http.NotFound(writer, request)
	}))
	defer server.Close()

	configPath := writeTestKubeconfig(t, server.URL)
	service := NewService(configPath)
	updated, err := service.AddEphemeralContainer(context.Background(), "test", "default", "api", EphemeralContainerRequest{
		Name:                "debugger",
		Image:               "busybox:1.36",
		Command:             []string{"/bin/sh"},
		TargetContainerName: "app",
	})
	if err != nil {
		t.Fatalf("add ephemeral container: %v", err)
	}
	if len(update.Spec.EphemeralContainers) != 1 || update.Spec.EphemeralContainers[0].Name != "debugger" || update.Spec.EphemeralContainers[0].Image != "busybox:1.36" || update.Spec.EphemeralContainers[0].TargetContainerName != "app" {
		t.Fatalf("unexpected subresource payload: %+v", update.Spec.EphemeralContainers)
	}
	if len(update.ManagedFields) != 0 {
		t.Fatalf("managedFields were sent in subresource payload: %+v", update.ManagedFields)
	}
	if updated == nil || len(updated.Spec.EphemeralContainers) != 1 {
		t.Fatalf("unexpected updated Pod: %+v", updated)
	}
}

// TestEnsureNetworkDebugContainerAddsReusableContainer verifies the stable capture container contract.
func TestEnsureNetworkDebugContainerAddsReusableContainer(t *testing.T) {
	var update corev1.Pod
	dryRunSeen := false
	updateCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/namespaces/default/pods/api" && request.Method == http.MethodGet {
			pod := &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "7"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "app:latest"}}}}
			if len(update.Spec.EphemeralContainers) > 0 {
				pod.Spec.EphemeralContainers = update.Spec.EphemeralContainers
			}
			writeTestPod(t, writer, pod)
			return
		}
		if request.URL.Path == "/api/v1/namespaces/default/pods/api/ephemeralcontainers" && request.Method == http.MethodPut {
			updateCount++
			if request.URL.Query().Get("dryRun") == "All" {
				dryRunSeen = true
			}
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read update: %v", readErr)
			}
			decoded, _, decodeErr := protobuf.NewSerializer(scheme.Scheme, scheme.Scheme).Decode(body, nil, &corev1.Pod{})
			if decodeErr != nil {
				t.Fatalf("decode update: %v", decodeErr)
			}
			update = *decoded.(*corev1.Pod)
			writeTestPod(t, writer, &update)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	service := NewService(writeTestKubeconfig(t, server.URL))
	result, err := service.EnsureNetworkDebugContainer(context.Background(), "test", "default", "api", DefaultNetworkDebugImage)
	if err != nil {
		t.Fatalf("ensure network debug container: %v", err)
	}
	if result == nil || !result.Created || result.Name != NetworkDebugContainerName || result.Image != DefaultNetworkDebugImage {
		t.Fatalf("unexpected network container result: %+v", result)
	}
	if !dryRunSeen || updateCount != 2 {
		t.Fatalf("network capability dry-run was not completed before the real update: dryRun=%v updates=%d", dryRunSeen, updateCount)
	}
	if len(update.Spec.EphemeralContainers) != 1 || update.Spec.EphemeralContainers[0].Name != NetworkDebugContainerName || update.Spec.EphemeralContainers[0].SecurityContext == nil || update.Spec.EphemeralContainers[0].SecurityContext.Capabilities == nil || len(update.Spec.EphemeralContainers[0].SecurityContext.Capabilities.Add) != 1 || update.Spec.EphemeralContainers[0].SecurityContext.Capabilities.Add[0] != "NET_RAW" {
		t.Fatalf("unexpected network container security payload: %+v", update.Spec.EphemeralContainers)
	}
}

// TestEnsureNetworkDebugContainerRejectsRunAsNonRootBeforeMutation verifies incompatible default images are blocked preflight.
func TestEnsureNetworkDebugContainerRejectsRunAsNonRootBeforeMutation(t *testing.T) {
	updated := false
	runAsNonRoot := true
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/namespaces/default/pods/api" && request.Method == http.MethodGet {
			writeTestPod(t, writer, &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "7"}, Spec: corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot}, Containers: []corev1.Container{{Name: "app", Image: "app:latest"}}}})
			return
		}
		if request.URL.Path == "/api/v1/namespaces/default/pods/api/ephemeralcontainers" {
			updated = true
			t.Fatalf("ephemeral-container update was attempted after failed preflight")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	service := NewService(writeTestKubeconfig(t, server.URL))
	_, err := service.EnsureNetworkDebugContainer(context.Background(), "test", "default", "api", DefaultNetworkDebugImage)
	if err == nil || !strings.Contains(err.Error(), "non-root-compatible network debug image") {
		t.Fatalf("expected runAsNonRoot preflight failure, got %v", err)
	}
	if updated {
		t.Fatal("preflight failure mutated the Pod")
	}
}

// TestDescribeEphemeralContainerUpdateErrorExplainsUnsupportedAndForbidden verifies operator-facing API errors.
func TestDescribeEphemeralContainerUpdateErrorExplainsUnsupportedAndForbidden(t *testing.T) {
	resource := schema.GroupResource{Resource: "pods"}
	unsupported := describeEphemeralContainerUpdateError(apierrors.NewMethodNotSupported(resource, "update"))
	if !strings.Contains(unsupported.Error(), "not supported") || !strings.Contains(unsupported.Error(), "ephemeralcontainers") {
		t.Fatalf("unsupported error was unclear: %v", unsupported)
	}
	forbidden := describeEphemeralContainerUpdateError(apierrors.NewForbidden(resource, "api", nil))
	if !strings.Contains(forbidden.Error(), "RBAC") {
		t.Fatalf("forbidden error was unclear: %v", forbidden)
	}
}

// writeTestKubeconfig creates a minimal context pointing at a test Kubernetes API server.
func writeTestKubeconfig(t *testing.T, serverURL string) string {
	t.Helper()
	path := t.TempDir() + "/config"
	contents := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, serverURL)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test kubeconfig: %v", err)
	}
	return path
}

// writeTestPod writes a typed Pod response for the native client test server.
func writeTestPod(t *testing.T, writer http.ResponseWriter, pod *corev1.Pod) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(pod); err != nil {
		t.Fatalf("write test Pod: %v", err)
	}
}

// TestSafeTarEntryNameRejectsArchiveTraversal verifies downloaded archives cannot escape their file scope.
func TestSafeTarEntryNameRejectsArchiveTraversal(t *testing.T) {
	for _, name := range []string{"/etc/passwd", "../passwd", "dir/../../passwd"} {
		if safeTarEntryName(name) {
			t.Fatalf("unsafe tar entry accepted: %q", name)
		}
	}
	if !safeTarEntryName("./passwd") {
		t.Fatal("regular tar entry rejected")
	}
}

// TestCopyFileMethodsValidateBeforeLoadingKubeconfig verifies malformed requests fail locally.
func TestCopyFileMethodsValidateBeforeLoadingKubeconfig(t *testing.T) {
	service := NewService("/path/that/does/not/exist")
	if _, err := service.CopyFileFromPod(context.Background(), "", "default", "api", "", "relative.txt"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("expected download path validation, got %v", err)
	}
	if err := service.CopyFileToPod(context.Background(), "", "default", "api", "", "/tmp/file", bytes.NewReader(nil), maxPodCopySize+1); err == nil || !strings.Contains(err.Error(), "no larger") {
		t.Fatalf("expected upload size validation, got %v", err)
	}
}
