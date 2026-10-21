package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kube-haggis/kube-haggis/internal/kube"
)

// TestHandleEphemeralContainerRequiresWriteMode verifies the backend write guard for debug containers.
func TestHandleEphemeralContainerRequiresWriteMode(t *testing.T) {
	server := &Server{kube: kube.NewService("/path/that/does/not/exist")}
	request := httptest.NewRequest(http.MethodPost, "/api/pod/debug?namespace=default&pod=api", strings.NewReader(`{"name":"debugger","image":"busybox"}`))
	response := httptest.NewRecorder()

	server.handleEphemeralContainer(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "write mode") {
		t.Fatalf("expected write-mode rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandleEphemeralContainerReturnsInputValidation verifies malformed coordinates are reported as client errors.
func TestHandleEphemeralContainerReturnsInputValidation(t *testing.T) {
	server := &Server{kube: kube.NewService("/path/that/does/not/exist")}
	request := httptest.NewRequest(http.MethodPost, "/api/pod/debug?namespace=all&pod=api", strings.NewReader(`{"name":"debugger","image":"busybox"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleEphemeralContainer(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "namespace is required") {
		t.Fatalf("expected namespace validation, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandleEphemeralContainerRejectsMalformedJSON verifies malformed operation bodies are client errors.
func TestHandleEphemeralContainerRejectsMalformedJSON(t *testing.T) {
	server := &Server{kube: kube.NewService("/path/that/does/not/exist")}
	request := httptest.NewRequest(http.MethodPost, "/api/pod/debug?namespace=default&pod=api", strings.NewReader(`{"name":`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleEphemeralContainer(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "decode ephemeral") {
		t.Fatalf("expected malformed JSON error, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandlePodCopyDownloadValidatesPath verifies read-only downloads still validate Pod coordinates locally.
func TestHandlePodCopyDownloadValidatesPath(t *testing.T) {
	server := &Server{kube: kube.NewService("/path/that/does/not/exist")}
	request := httptest.NewRequest(http.MethodGet, "/api/pod/copy?namespace=default&pod=api&path=relative.txt", nil)
	response := httptest.NewRecorder()

	server.handlePodCopy(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "must be absolute") {
		t.Fatalf("expected path validation, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandlePodCopyUploadRequiresWriteMode verifies uploads cannot bypass the application write switch.
func TestHandlePodCopyUploadRequiresWriteMode(t *testing.T) {
	server := &Server{kube: kube.NewService("/path/that/does/not/exist")}
	request := httptest.NewRequest(http.MethodPost, "/api/pod/copy?namespace=default&pod=api", bytes.NewReader(nil))
	response := httptest.NewRecorder()

	server.handlePodCopy(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "write mode") {
		t.Fatalf("expected write-mode rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestWriteErrorClassifiesUnsupportedSubresource verifies unsupported Kubernetes features use a clear HTTP status.
func TestWriteErrorClassifiesUnsupportedSubresource(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, errString("native stream failed"))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("unexpected baseline error status: %d", response.Code)
	}

	response = httptest.NewRecorder()
	writeError(response, errString("ephemeral debug containers are not supported by this Kubernetes API server"))
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported subresource status: %d", response.Code)
	}
}

// TestHandleOverviewReturnsMetricsPayload verifies metrics remain on the read-only overview API.
func TestHandleOverviewReturnsMetricsPayload(t *testing.T) {
	server := &Server{
		overview: func(context.Context, string) (kube.ClusterOverview, error) {
			return kube.ClusterOverview{
				MetricsAvailable: true,
				Metrics: kube.MetricsOverview{
					State:     "available",
					NodeState: "available",
					PodState:  "available",
					Nodes:     []kube.NodeMetric{{Name: "worker-a", Usage: kube.MetricUsage{CPUCores: 0.5, MemoryBytes: 1024}}},
					Pods:      []kube.PodMetric{{Name: "api", Namespace: "default", Usage: kube.MetricUsage{CPUCores: 0.25, MemoryBytes: 512}}},
				},
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/overview?context=field", nil)
	response := httptest.NewRecorder()

	server.handleOverview(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("overview request failed: %d %s", response.Code, response.Body.String())
	}
	var payload kube.ClusterOverview
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode overview response: %v", err)
	}
	if !payload.MetricsAvailable || payload.Metrics.State != "available" || len(payload.Metrics.Nodes) != 1 || len(payload.Metrics.Pods) != 1 {
		t.Fatalf("metrics payload was not returned: %+v", payload)
	}
}

// TestHandleOverviewReturnsBackendError verifies overview collection errors use the standard API response.
func TestHandleOverviewReturnsBackendError(t *testing.T) {
	server := &Server{overview: func(context.Context, string) (kube.ClusterOverview, error) {
		return kube.ClusterOverview{}, errString("metrics collection transport failed")
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	response := httptest.NewRecorder()

	server.handleOverview(response, request)

	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "metrics collection transport failed") {
		t.Fatalf("expected backend error, got %d %s", response.Code, response.Body.String())
	}
}

// errString adapts a literal into the error interface for HTTP classification tests.
type errString string

// Error returns the literal test error.
func (e errString) Error() string { return string(e) }
