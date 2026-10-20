package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kube-haggis/kube-haggis/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// writeServerAPIKubeconfig writes a test-only kubeconfig pointing at one local fake API server.
func writeServerAPIKubeconfig(t *testing.T, serverURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := clientcmdapi.NewConfig()
	config.CurrentContext = "test"
	config.Clusters["test"] = &clientcmdapi.Cluster{Server: serverURL}
	config.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "test"}
	config.AuthInfos["test"] = &clientcmdapi.AuthInfo{}
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatalf("write fake API kubeconfig: %v", err)
	}
	return path
}

// TestGenericDiscoveryAndWatchHandlersUseFakeCluster verifies isolated HTTP handlers against a local Kubernetes API.
func TestGenericDiscoveryAndWatchHandlersUseFakeCluster(t *testing.T) {
	listQuery := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Errorf("encode fake Kubernetes response: %v", err)
			}
		}
		if request.URL.Path == "/api/v1/namespaces/default/pods" && request.URL.Query().Get("watch") == "true" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"type":"ADDED","object":{"apiVersion":"v1","kind":"Pod","metadata":{"name":"api","namespace":"default"}}}` + "\n"))
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}
		if request.URL.Path == "/api/v1/namespaces/default/pods" {
			listQuery = request.URL.RawQuery
			writeJSON(&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "PodList",
					"metadata":   map[string]interface{}{"resourceVersion": "12", "continue": "next-page"},
				},
				Items: []unstructured.Unstructured{{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Pod",
					"metadata": map[string]interface{}{
						"name":              "api",
						"namespace":         "default",
						"uid":               "pod-uid",
						"resourceVersion":   "11",
						"creationTimestamp": "2026-10-20T10:00:00Z",
					},
					"status": map[string]interface{}{"phase": "Running"},
				}}},
			})
			return
		}
		switch request.URL.Path {
		case "/api":
			writeJSON(metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{"v1"}})
		case "/api/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}}}})
		case "/apis":
			writeJSON(&metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}, Groups: []metav1.APIGroup{{
				TypeMeta:         metav1.TypeMeta{Kind: "APIGroup", APIVersion: "v1"},
				Name:             "apps",
				Versions:         []metav1.GroupVersionForDiscovery{{GroupVersion: "apps/v1", Version: "v1"}},
				PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "apps/v1", Version: "v1"},
			}}})
		case "/apis/apps/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "watch"}}}})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeJSON(&unstructured.UnstructuredList{Object: map[string]interface{}{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinitionList",
				"metadata":   map[string]interface{}{},
			}, Items: []unstructured.Unstructured{}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := kube.NewService(writeServerAPIKubeconfig(t, server.URL))
	handler := (&Server{kube: service}).Handler()

	discoveryResponse := httptest.NewRecorder()
	handler.ServeHTTP(discoveryResponse, httptest.NewRequest(http.MethodGet, "/api/discovery?context=test", nil))
	if discoveryResponse.Code != http.StatusOK {
		t.Fatalf("discovery handler failed: status=%d body=%s", discoveryResponse.Code, discoveryResponse.Body.String())
	}
	var discoveryPayload struct {
		Items []kube.ResourceDefinition `json:"items"`
	}
	if err := json.NewDecoder(discoveryResponse.Body).Decode(&discoveryPayload); err != nil {
		t.Fatalf("decode discovery handler response: %v", err)
	}
	foundPod := false
	for _, item := range discoveryPayload.Items {
		if item.Resource == "pods" && item.Kind == "Pod" && item.Namespaced {
			foundPod = true
		}
	}
	if !foundPod {
		t.Fatalf("discovery handler omitted fake Pod definition: %+v", discoveryPayload.Items)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/resources?context=test&group=&version=v1&resource=pods&namespace=default&namespaced=true&limit=25&continue=opaque-token&labelSelector=app%3Dapi", nil))
	var listPayload kube.ResourceList
	if err := json.NewDecoder(listResponse.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode resource list handler response: %v", err)
	}
	if listResponse.Code != http.StatusOK || listPayload.Continue != "next-page" || listPayload.ResourceVersion != "12" || len(listPayload.Items) != 1 || listPayload.Items[0].Status != "Running" {
		t.Fatalf("resource list handler returned unexpected page: status=%d payload=%+v", listResponse.Code, listPayload)
	}
	if !strings.Contains(listQuery, "continue=opaque-token") || !strings.Contains(listQuery, "limit=25") || !strings.Contains(listQuery, "labelSelector=app%3Dapi") {
		t.Fatalf("resource list handler did not preserve query controls: %s", listQuery)
	}

	watchResponse := httptest.NewRecorder()
	handler.ServeHTTP(watchResponse, httptest.NewRequest(http.MethodGet, "/api/watch?context=test&group=&version=v1&resource=pods&namespace=default&namespaced=true&resourceVersion=12", nil))
	if watchResponse.Code != http.StatusOK || watchResponse.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(watchResponse.Body.String(), "KubeHaggis watch connected") || !strings.Contains(watchResponse.Body.String(), `"type":"ADDED"`) {
		t.Fatalf("watch handler did not stream the fake event: status=%d content-type=%q body=%s", watchResponse.Code, watchResponse.Header().Get("Content-Type"), watchResponse.Body.String())
	}
}

// TestGenericResourceHandlerRedactsRBACAndConflictErrors verifies generic mutation HTTP status and response safety.
func TestGenericResourceHandlerRedactsRBACAndConflictErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		reason     metav1.StatusReason
		wantStatus int
	}{
		{name: "forbidden", statusCode: http.StatusForbidden, reason: metav1.StatusReasonForbidden, wantStatus: http.StatusForbidden},
		{name: "conflict", statusCode: http.StatusConflict, reason: metav1.StatusReasonConflict, wantStatus: http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if test.name == "conflict" && request.Method == http.MethodGet {
					writer.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(writer).Encode(&unstructured.Unstructured{Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name":            "api",
							"namespace":       "default",
							"resourceVersion": "8",
						},
						"spec": map[string]interface{}{"replicas": int64(3)},
					}})
					return
				}
				if request.Method != http.MethodPut || request.URL.Query().Get("dryRun") != "All" {
					t.Errorf("generic mutation lost update or dry-run options: method=%s query=%s", request.Method, request.URL.RawQuery)
				}
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(test.statusCode)
				_ = json.NewEncoder(writer).Encode(&metav1.Status{
					TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
					Status:   metav1.StatusFailure,
					Reason:   test.reason,
					Message:  "Secret value private-value was rejected by the fake Kubernetes API",
					Code:     int32(test.statusCode),
				})
			}))
			defer server.Close()

			handler := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, server.URL))}).Handler()
			request := httptest.NewRequest(http.MethodPatch, "/api/resource?context=test&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&dryRun=true", strings.NewReader("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: default\n  resourceVersion: \"7\"\n"))
			request.Header.Set("X-KubeHaggis-Write", "enabled")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus || strings.Contains(response.Body.String(), "private-value") {
				t.Fatalf("generic mutation error was misclassified or unsafe: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
