package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AyCarlito/kube-haggis/internal/audit"
	"github.com/AyCarlito/kube-haggis/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestRequestMiddlewareAssignsSafeIDsAndCapturesStatus verifies generated and accepted request IDs.
func TestRequestMiddlewareAssignsSafeIDsAndCapturesStatus(t *testing.T) {
	server := &Server{}
	handler := server.withLogging(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if requestIDFor(request) == "" {
			t.Fatal("request context did not contain a correlation ID")
		}
		writer.WriteHeader(http.StatusTeapot)
	}))

	generatedRequest := httptest.NewRequest(http.MethodGet, "/api/resource?context=field&group=apps&version=v1&resource=deployments", nil)
	generatedRequest.Header.Set("X-Request-ID", "unsafe value with spaces")
	generatedResponse := httptest.NewRecorder()
	handler.ServeHTTP(generatedResponse, generatedRequest)
	generatedID := generatedResponse.Header().Get("X-Request-ID")
	if !validRequestID(generatedID) || generatedID == "unsafe value with spaces" {
		t.Fatalf("unsafe incoming request ID was not replaced: %q", generatedID)
	}

	acceptedRequest := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	acceptedRequest.Header.Set("X-Request-ID", "operator-42")
	acceptedResponse := httptest.NewRecorder()
	handler.ServeHTTP(acceptedResponse, acceptedRequest)
	if acceptedResponse.Header().Get("X-Request-ID") != "operator-42" {
		t.Fatalf("valid incoming request ID was not preserved: %q", acceptedResponse.Header().Get("X-Request-ID"))
	}
	if acceptedResponse.Code != http.StatusTeapot || server.diagnosticStore().snapshot(kube.DiagnosticsSnapshot{}, "unavailable", time.Now()).Requests.Total != 2 {
		t.Fatalf("request status or bounded counter was not captured: status=%d", acceptedResponse.Code)
	}
}

// TestRequestResponseWriterPreservesStreaming verifies SSE writes and flushes remain available.
func TestRequestResponseWriterPreservesStreaming(t *testing.T) {
	server := &Server{}
	handler := server.withLogging(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Fatal("request writer lost http.Flusher")
		}
		if _, err := writer.Write([]byte("data: connected\n\n")); err != nil {
			t.Fatalf("stream write failed: %v", err)
		}
		flusher.Flush()
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/watch?context=field&group=&version=v1&resource=pods", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || response.Body.String() != "data: connected\n\n" {
		t.Fatalf("streaming response was altered: status=%d content-type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

// diagnosticsHijackWriter is a minimal test writer implementing the WebSocket upgrade seam.
type diagnosticsHijackWriter struct {
	header     http.Header
	connection net.Conn
	buffered   *bufio.ReadWriter
	status     int
}

// Header returns the test writer's mutable response headers.
func (writer *diagnosticsHijackWriter) Header() http.Header {
	return writer.header
}

// Write records a successful test response body write.
func (writer *diagnosticsHijackWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

// WriteHeader records a test response status.
func (writer *diagnosticsHijackWriter) WriteHeader(status int) {
	writer.status = status
}

// Hijack returns the test connection and buffered writer used by an upgrade.
func (writer *diagnosticsHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.connection, writer.buffered, nil
}

// TestRequestResponseWriterPreservesHijack verifies WebSocket upgrades still reach the underlying writer.
func TestRequestResponseWriterPreservesHijack(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	underlying := &diagnosticsHijackWriter{header: make(http.Header), connection: left, buffered: bufio.NewReadWriter(bufio.NewReader(right), bufio.NewWriter(right))}
	wrapped := newRequestResponseWriter(underlying)
	connection, _, err := wrapped.Hijack()
	if err != nil || connection != left || wrapped.Status() != http.StatusSwitchingProtocols {
		t.Fatalf("hijack was not preserved: connection=%v status=%d err=%v", connection, wrapped.Status(), err)
	}
}

// TestDiagnosticsRetainsOnlyBoundedSafeEvents verifies concurrent-safe aggregates do not retain arbitrary text.
func TestDiagnosticsRetainsOnlyBoundedSafeEvents(t *testing.T) {
	store := newDiagnosticsStore()
	for index := 0; index < maxDiagnosticErrors+8; index++ {
		store.recordSafeError("private-secret-value", "resource-read", http.StatusBadGateway)
	}
	response := store.snapshot(kube.DiagnosticsSnapshot{}, "unavailable", time.Now())
	if len(response.RecentErrors) != maxDiagnosticErrors || response.Requests.ErrorCategories["request-failed"] != maxDiagnosticErrors+8 {
		t.Fatalf("diagnostics retention or category bound failed: %+v", response.Requests)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("encode diagnostics snapshot: %v", err)
	}
	if strings.Contains(string(encoded), "private-secret-value") {
		t.Fatalf("diagnostics retained arbitrary sensitive category text: %s", encoded)
	}

	session := store.startSession("terminal", "field", "core/v1/pods/exec")
	if store.snapshot(kube.DiagnosticsSnapshot{}, "unavailable", time.Now()).Sessions.Terminal != 1 {
		t.Fatal("active terminal session was not counted")
	}
	store.finishSession(session, "", "terminal")
	if store.snapshot(kube.DiagnosticsSnapshot{}, "unavailable", time.Now()).Sessions.Terminal != 0 {
		t.Fatal("finished terminal session remained active")
	}
}

// TestDiagnosticsEmptyCollectionsEncodeAsArrays verifies clean snapshots never encode rendered collections as null.
func TestDiagnosticsEmptyCollectionsEncodeAsArrays(t *testing.T) {
	handler := (&Server{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil))
	var payload struct {
		RecentErrors []json.RawMessage `json:"recentErrors"`
		Watches      struct {
			Current []json.RawMessage `json:"current"`
			Recent  []json.RawMessage `json:"recent"`
		} `json:"watches"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode empty diagnostics response: %v", err)
	}
	if response.Code != http.StatusOK || payload.RecentErrors == nil || len(payload.RecentErrors) != 0 || payload.Watches.Current == nil || len(payload.Watches.Current) != 0 || payload.Watches.Recent == nil || len(payload.Watches.Recent) != 0 {
		t.Fatalf("empty diagnostics collections were not encoded as arrays: status=%d payload=%+v body=%s", response.Code, payload, response.Body.String())
	}
}

// TestHealthAndReadinessSeparateLiveness verifies liveness stays healthy when kubeconfig dependencies are absent.
func TestHealthAndReadinessSeparateLiveness(t *testing.T) {
	server := (&Server{kube: kube.NewService(filepath.Join(t.TempDir(), "missing-config"))}).Handler()
	health := httptest.NewRecorder()
	server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK || health.Header().Get("X-Request-ID") == "" {
		t.Fatalf("liveness was not local-process compatible: status=%d request-id=%q", health.Code, health.Header().Get("X-Request-ID"))
	}
	readiness := httptest.NewRecorder()
	server.ServeHTTP(readiness, httptest.NewRequest(http.MethodGet, "/api/readiness", nil))
	if readiness.Code != http.StatusServiceUnavailable || strings.Contains(readiness.Body.String(), "missing-config") {
		t.Fatalf("missing kubeconfig was not safely classified: status=%d body=%s", readiness.Code, readiness.Body.String())
	}
}

// TestReadinessReportsMissingContext verifies an unknown selected context is not treated as ready.
func TestReadinessReportsMissingContext(t *testing.T) {
	cluster := newDiagnosticsDiscoveryServer(t, false)
	defer cluster.Close()
	server := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, cluster.URL))}).Handler()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/readiness?context=missing", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"selected":"missing"`) || !strings.Contains(response.Body.String(), `"state":"unavailable"`) {
		t.Fatalf("missing selected context was not classified: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestReadinessReportsAuditUnavailable verifies an unavailable local audit dependency is explicit.
func TestReadinessReportsAuditUnavailable(t *testing.T) {
	cluster := newDiagnosticsDiscoveryServer(t, false)
	defer cluster.Close()
	service := kube.NewService(writeServerAPIKubeconfig(t, cluster.URL))
	server := (&Server{kube: service}).Handler()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/readiness?context=test", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"audit":{"state":"unavailable"}`) {
		t.Fatalf("audit unavailability was not reported: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestReadinessReportsHealthyDependencies verifies the ready state after a successful discovery load and audit check.
func TestReadinessReportsHealthyDependencies(t *testing.T) {
	cluster := newDiagnosticsDiscoveryServer(t, false)
	defer cluster.Close()
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open test audit store: %v", err)
	}
	defer store.Close()
	server := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, cluster.URL)), audit: store}).Handler()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/readiness?context=test", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ready"`) || !strings.Contains(response.Body.String(), `"state":"available"`) {
		t.Fatalf("healthy dependencies were not ready: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestReadinessReportsPartialDiscovery verifies usable but incomplete discovery is reported as degraded.
func TestReadinessReportsPartialDiscovery(t *testing.T) {
	cluster := newDiagnosticsDiscoveryServer(t, true)
	defer cluster.Close()
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open test audit store: %v", err)
	}
	defer store.Close()
	server := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, cluster.URL)), audit: store}).Handler()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/readiness?context=test", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"status":"degraded"`) || !strings.Contains(response.Body.String(), `"state":"partial"`) {
		t.Fatalf("partial discovery was not reported safely: status=%d body=%s", response.Code, response.Body.String())
	}
}

// newDiagnosticsDiscoveryServer creates a small fake API server for readiness dependency tests.
func newDiagnosticsDiscoveryServer(t *testing.T, partial bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeDiscoveryPayload := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(value)
		}
		switch request.URL.Path {
		case "/api":
			writeDiscoveryPayload(&metav1.APIVersions{Versions: []string{"v1"}})
		case "/api/v1":
			writeDiscoveryPayload(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}}}})
		case "/apis":
			writeDiscoveryPayload(&metav1.APIGroupList{Groups: []metav1.APIGroup{{Name: "apps", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "apps/v1", Version: "v1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "apps/v1", Version: "v1"}}}})
		case "/apis/apps/v1":
			if partial {
				writer.WriteHeader(http.StatusForbidden)
				writeDiscoveryPayload(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Code: http.StatusForbidden})
				return
			}
			writeDiscoveryPayload(&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list"}}}})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeDiscoveryPayload(map[string]interface{}{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "items": []interface{}{}})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestDiagnosticResponseWriterReadFrom verifies response byte capture does not alter io.Copy behavior.
func TestDiagnosticResponseWriterReadFrom(t *testing.T) {
	response := httptest.NewRecorder()
	writer := newRequestResponseWriter(response)
	count, err := io.Copy(writer, strings.NewReader("diagnostic stream"))
	if err != nil || count != int64(len("diagnostic stream")) || writer.BytesWritten() != count || response.Body.String() != "diagnostic stream" {
		t.Fatalf("ReadFrom changed stream behavior: count=%d captured=%d body=%q err=%v", count, writer.BytesWritten(), response.Body.String(), err)
	}
}
