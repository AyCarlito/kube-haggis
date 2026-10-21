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

// TestDiscoveryHandlerReturnsCompleteCatalog verifies the HTTP catalog includes all versions and subresource capabilities.
func TestDiscoveryHandlerReturnsCompleteCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeCatalogResponse := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Fatalf("encode catalog response: %v", err)
			}
		}
		switch request.URL.Path {
		case "/api":
			writeCatalogResponse(&metav1.APIVersions{Versions: []string{"v1"}})
		case "/api/v1":
			writeCatalogResponse(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
				{Name: "pods/log", Kind: "Pod", Namespaced: true, Verbs: []string{"get"}},
			}})
		case "/apis":
			writeCatalogResponse(&metav1.APIGroupList{Groups: []metav1.APIGroup{{
				Name:             "apps",
				Versions:         []metav1.GroupVersionForDiscovery{{GroupVersion: "apps/v1", Version: "v1"}, {GroupVersion: "apps/v1beta1", Version: "v1beta1"}},
				PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "apps/v1", Version: "v1"},
			}}})
		case "/apis/apps/v1", "/apis/apps/v1beta1":
			version := strings.TrimPrefix(request.URL.Path, "/apis/apps/")
			writeCatalogResponse(&metav1.APIResourceList{GroupVersion: "apps/" + version, APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "watch", "update"}},
				{Name: "deployments/status", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "update"}},
			}})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeCatalogResponse(map[string]interface{}{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "items": []interface{}{}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	handler := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, server.URL))}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/discovery?context=test", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog handler failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var catalog kube.ResourceCatalog
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if catalog.State != "available" || len(catalog.Items) != 3 {
		t.Fatalf("catalog was incomplete or unhealthy: %+v", catalog)
	}
	for _, definition := range catalog.Items {
		if strings.Contains(definition.Resource, "/") {
			t.Fatalf("subresource was exposed as a top-level row: %+v", definition)
		}
		if definition.Group == "apps" && definition.Version == "v1" {
			if definition.Scope != "Namespaced" || len(definition.Subresources) != 1 || definition.Subresources[0].Name != "status" {
				t.Fatalf("deployment catalog capabilities were incomplete: %+v", definition)
			}
		}
	}
}

// TestDiscoveryHandlerSurfacesPartialRBACDiscovery verifies the catalog response keeps usable rows and denied API state.
func TestDiscoveryHandlerSurfacesPartialRBACDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeCatalogResponse := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Fatalf("encode partial catalog response: %v", err)
			}
		}
		switch request.URL.Path {
		case "/api":
			writeCatalogResponse(&metav1.APIVersions{Versions: []string{"v1"}})
		case "/api/v1":
			writeCatalogResponse(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true}}})
		case "/apis":
			writeCatalogResponse(&metav1.APIGroupList{Groups: []metav1.APIGroup{
				{Name: "apps", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "apps/v1", Version: "v1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "apps/v1", Version: "v1"}},
				{Name: "blocked.example.com", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "blocked.example.com/v1", Version: "v1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "blocked.example.com/v1", Version: "v1"}},
			}})
		case "/apis/apps/v1":
			writeCatalogResponse(&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true}}})
		case "/apis/blocked.example.com/v1":
			writer.WriteHeader(http.StatusForbidden)
			writeCatalogResponse(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Code: http.StatusForbidden})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeCatalogResponse(map[string]interface{}{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "items": []interface{}{}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	handler := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, server.URL))}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/discovery?context=test", nil))
	var catalog kube.ResourceCatalog
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode partial catalog response: %v", err)
	}
	if response.Code != http.StatusOK || catalog.State != "partial" || len(catalog.Items) != 2 || len(catalog.Issues) != 1 || catalog.Issues[0].State != "permission-denied" || !catalog.Issues[0].Retryable {
		t.Fatalf("partial RBAC discovery was not explicit: status=%d catalog=%+v", response.Code, catalog)
	}
}
