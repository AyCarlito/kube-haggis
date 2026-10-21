package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestWatchHandlersSignalClosureAndSafeFailure verifies generic and Event SSE lifecycle signals.
func TestWatchHandlersSignalClosureAndSafeFailure(t *testing.T) {
	t.Run("generic closure", func(t *testing.T) {
		handler, closeServer := newWatchHandlerTestHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
		})
		defer closeServer()

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/watch?context=test&group=&version=v1&resource=pods&namespace=default&namespaced=true&resourceVersion=12", nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"type":"CLOSED"`) || !strings.Contains(response.Body.String(), `"reason":"watch-closed"`) {
			t.Fatalf("generic watch closure was not signaled: status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("event closure", func(t *testing.T) {
		handler, closeServer := newWatchHandlerTestHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
		})
		defer closeServer()

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/events/watch?context=test&namespace=default&uid=pod-uid&resourceVersion=12", nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"type":"CLOSED"`) {
			t.Fatalf("Event watch closure was not signaled: status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("resource version error", func(t *testing.T) {
		handler, closeServer := newWatchHandlerTestHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			payload := map[string]interface{}{
				"type": "ERROR",
				"object": &metav1.Status{
					TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
					Status:   metav1.StatusFailure,
					Reason:   metav1.StatusReasonExpired,
					Message:  "secret watch detail must not reach the browser",
					Code:     http.StatusGone,
				},
			}
			if err := json.NewEncoder(writer).Encode(payload); err != nil {
				t.Errorf("encode fake watch error: %v", err)
			}
		})
		defer closeServer()

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/watch?context=test&group=&version=v1&resource=pods&namespace=default&namespaced=true&resourceVersion=12", nil))
		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, `"type":"ERROR"`) || !strings.Contains(body, `"reason":"resource-version-expired"`) || !strings.Contains(body, `"relist":true`) || strings.Contains(body, "secret watch detail") {
			t.Fatalf("resource-version watch error was not classified safely: status=%d body=%s", response.Code, body)
		}
	})
}

// TestWatchOpenErrorUsesSafeGoneResponse verifies an HTTP 410 watch-open failure is classifiable without raw Kubernetes text.
func TestWatchOpenErrorUsesSafeGoneResponse(t *testing.T) {
	handler, closeServer := newWatchHandlerTestHandler(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusGone)
		_ = json.NewEncoder(writer).Encode(&metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusFailure,
			Reason:   metav1.StatusReasonExpired,
			Message:  "raw expired resource version should stay server-side",
			Code:     http.StatusGone,
		})
	})
	defer closeServer()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/watch?context=test&group=&version=v1&resource=pods&namespace=default&namespaced=true&resourceVersion=expired", nil))
	body := response.Body.String()
	if response.Code != http.StatusGone || !strings.Contains(body, `"reason":"resource-version-expired"`) || !strings.Contains(body, `"relist":true`) || strings.Contains(body, "raw expired resource version") {
		t.Fatalf("watch-open 410 was not safely classified: status=%d body=%s", response.Code, body)
	}
}

// TestWatchOpenPermissionUsesSafeSSESignal verifies native EventSource can classify initial RBAC denial.
func TestWatchOpenPermissionUsesSafeSSESignal(t *testing.T) {
	handler, closeServer := newWatchHandlerTestHandler(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(writer).Encode(&metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusFailure,
			Reason:   metav1.StatusReasonForbidden,
			Message:  "raw permission detail should stay server-side",
			Code:     http.StatusForbidden,
		})
	})
	defer closeServer()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/watch?context=test&group=&version=v1&resource=pods&namespace=default&namespaced=true&resourceVersion=12", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(body, `"type":"ERROR"`) || !strings.Contains(body, `"reason":"permission-denied"`) || strings.Contains(body, "raw permission detail") {
		t.Fatalf("permission watch-open error was not sent as a safe SSE signal: status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), body)
	}
	if strings.Contains(body, `"relist":true`) {
		t.Fatalf("permission watch-open error incorrectly requested relist retries: body=%s", body)
	}
}

// newWatchHandlerTestHandler creates a local Kubernetes API and KubeHaggis handler for watch tests.
func newWatchHandlerTestHandler(t *testing.T, watchResponse func(http.ResponseWriter, *http.Request)) (http.Handler, func()) {
	t.Helper()
	apiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("watch") == "true" {
			watchResponse(writer, request)
			return
		}
		writeWatchDiscoveryResponse(writer, request)
	}))
	service := kube.NewService(writeServerAPIKubeconfig(t, apiServer.URL))
	return (&Server{kube: service}).Handler(), apiServer.Close
}

// writeWatchDiscoveryResponse serves the minimum safe discovery surface used by generic watch tests.
func writeWatchDiscoveryResponse(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	switch request.URL.Path {
	case "/api":
		_ = json.NewEncoder(writer).Encode(&metav1.APIVersions{Versions: []string{"v1"}})
	case "/api/v1":
		_ = json.NewEncoder(writer).Encode(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}}}})
	case "/apis":
		_ = json.NewEncoder(writer).Encode(&metav1.APIGroupList{})
	default:
		http.NotFound(writer, request)
	}
}
