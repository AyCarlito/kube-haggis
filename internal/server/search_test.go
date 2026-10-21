package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestSearchHandlerReturnsBoundedPartialGVRResults verifies selectors, exact result identity, partial sources, and redaction.
func TestSearchHandlerReturnsBoundedPartialGVRResults(t *testing.T) {
	listQuery := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Errorf("encode fake search response: %v", err)
			}
		}
		switch request.URL.Path {
		case "/api":
			writeJSON(&metav1.APIVersions{Versions: []string{"v1"}})
		case "/api/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}}}})
		case "/apis":
			writeJSON(&metav1.APIGroupList{Groups: []metav1.APIGroup{
				{Name: "apps", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "apps/v1", Version: "v1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "apps/v1", Version: "v1"}},
				{Name: "blocked.example.com", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "blocked.example.com/v1", Version: "v1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "blocked.example.com/v1", Version: "v1"}},
			}})
		case "/apis/apps/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list"}}}})
		case "/apis/blocked.example.com/v1":
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusForbidden)
			writeJSON(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Message: "private-value should never reach the browser", Code: http.StatusForbidden})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeJSON(&unstructured.UnstructuredList{Object: map[string]interface{}{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinitionList",
				"metadata":   map[string]interface{}{},
			}, Items: []unstructured.Unstructured{}})
		case "/api/v1/namespaces/team-a/pods":
			listQuery = request.URL.RawQuery
			writeJSON(&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "PodList",
					"metadata":   map[string]interface{}{"resourceVersion": "17"},
				},
				Items: []unstructured.Unstructured{
					{Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "api",
							"namespace": "team-a",
							"labels":    map[string]interface{}{"app": "api"},
						},
						"spec": map[string]interface{}{"private": "private-value"},
					}},
				},
			})
		case "/apis/apps/v1/namespaces/team-a/deployments":
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusForbidden)
			writeJSON(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Message: "private-value deployment denial", Code: http.StatusForbidden})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	handler := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, server.URL))}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/search?context=test&namespace=team-a&query=api&name=api&labelSelector=app%3Dapi", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("search handler failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var payload kube.SearchResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if payload.State != "partial" || !payload.Partial || len(payload.Items) != 1 || payload.CallCount != 2 || payload.CallBudget != 24 {
		t.Fatalf("search did not expose bounded partial state: %+v", payload)
	}
	item := payload.Items[0]
	if item.Group != "" || item.Version != "v1" || item.Resource != "pods" || item.Kind != "Pod" || item.Namespace != "team-a" || item.Name != "api" {
		t.Fatalf("search result lost complete core GVR identity: %+v", item)
	}
	encoded := response.Body.String()
	if strings.Contains(encoded, "private-value") || strings.Contains(encoded, "\"spec\"") {
		t.Fatalf("search response leaked Kubernetes error or object data: %s", encoded)
	}
	if !strings.Contains(listQuery, "fieldSelector=metadata.name%3Dapi") || !strings.Contains(listQuery, "labelSelector=app%3Dapi") {
		t.Fatalf("search request did not propagate server selectors: %s", listQuery)
	}
	denied := false
	for _, source := range payload.Sources {
		if source.Resource == "deployments" {
			denied = source.State == "permission-denied" && strings.Contains(source.Issue, "RBAC")
		}
	}
	if !denied {
		t.Fatalf("RBAC-limited source was not explicit: %+v", payload.Sources)
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/search?context=test&labelSelector=app+in+%28", nil))
	if invalid.Code != http.StatusBadRequest || strings.Contains(invalid.Body.String(), "private-value") || !strings.Contains(invalid.Body.String(), "Search filters are invalid") {
		t.Fatalf("invalid search filter was not safely rejected: status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

// TestWriteSearchErrorAddsSafeCorrelationMetadata verifies fixed search failures carry request diagnostics.
func TestWriteSearchErrorAddsSafeCorrelationMetadata(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		status    int
		category  string
		retry     string
		requestID string
	}{
		{name: "backend", err: errors.New("search provider private-search-token failed"), status: http.StatusBadGateway, category: "dependency-unavailable", retry: "retry", requestID: "search-backend-7"},
		{name: "validation", err: errors.New("search filter private-search-token is invalid"), status: http.StatusBadRequest, category: "validation", retry: "check-and-retry", requestID: "search-filter-8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := (&Server{}).withLogging(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writeSearchError(writer, test.err)
			}))
			request := httptest.NewRequest(http.MethodGet, "/api/search", nil)
			request.Header.Set("X-Request-ID", test.requestID)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			body := response.Body.String()
			if response.Code != test.status || strings.Contains(body, "private-search-token") || !strings.Contains(body, `"errorCategory":"`+test.category+`"`) || !strings.Contains(body, `"retry":"`+test.retry+`"`) || !strings.Contains(body, `"requestId":"`+test.requestID+`"`) {
				t.Fatalf("search error metadata was unsafe or incomplete: status=%d body=%s", response.Code, body)
			}
		})
	}
}
