package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestCRDCatalogAndListExposeVersionedCapabilities verifies server JSON preserves CRD metadata and safe printer values.
func TestCRDCatalogAndListExposeVersionedCapabilities(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Errorf("encode fake CRD response: %v", err)
			}
		}
		crd := map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata":   map[string]interface{}{"name": "widgets.widgets.example.com"},
			"spec": map[string]interface{}{
				"group": "widgets.example.com",
				"names": map[string]interface{}{"plural": "widgets", "kind": "Widget"},
				"scope": "Namespaced",
				"subresources": map[string]interface{}{
					"status": map[string]interface{}{},
					"scale":  map[string]interface{}{"specReplicasPath": ".spec.replicas", "statusReplicasPath": ".status.replicas"},
				},
				"versions": []interface{}{
					map[string]interface{}{"name": "v1", "served": true, "storage": true, "schema": map[string]interface{}{"openAPIV3Schema": map[string]interface{}{"type": "object"}}},
					map[string]interface{}{"name": "v1beta1", "served": true, "storage": false, "schema": map[string]interface{}{"openAPIV3Schema": map[string]interface{}{"type": "object"}}, "additionalPrinterColumns": []interface{}{map[string]interface{}{"name": "Phase", "type": "string", "jsonPath": ".status.phase"}}},
				},
			},
		}
		switch request.URL.Path {
		case "/api":
			writeJSON(&metav1.APIVersions{Versions: []string{}})
		case "/apis":
			writeJSON(&metav1.APIGroupList{Groups: []metav1.APIGroup{{Name: "widgets.example.com", Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "widgets.example.com/v1", Version: "v1"}, {GroupVersion: "widgets.example.com/v1beta1", Version: "v1beta1"}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "widgets.example.com/v1beta1", Version: "v1beta1"}}}})
		case "/apis/widgets.example.com/v1", "/apis/widgets.example.com/v1beta1":
			version := strings.TrimPrefix(request.URL.Path, "/apis/widgets.example.com/")
			writeJSON(&metav1.APIResourceList{GroupVersion: "widgets.example.com/" + version, APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list", "watch"}}}})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeJSON(&unstructured.UnstructuredList{Object: map[string]interface{}{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "metadata": map[string]interface{}{}}, Items: []unstructured.Unstructured{{Object: crd}}})
		case "/apis/widgets.example.com/v1beta1/namespaces/team-a/widgets":
			list := &unstructured.UnstructuredList{
				Object: map[string]interface{}{"apiVersion": "widgets.example.com/v1beta1", "kind": "WidgetList", "metadata": map[string]interface{}{"resourceVersion": "31"}},
				Items: []unstructured.Unstructured{{Object: map[string]interface{}{
					"apiVersion": "widgets.example.com/v1beta1",
					"kind":       "Widget",
					"metadata":   map[string]interface{}{"name": "blue", "namespace": "team-a", "resourceVersion": "30"},
					"status":     map[string]interface{}{"phase": "Ready"},
				}}},
			}
			writeJSON(list)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer apiServer.Close()

	handler := (&Server{kube: kube.NewService(writeServerAPIKubeconfig(t, apiServer.URL))}).Handler()
	catalogResponse := httptest.NewRecorder()
	handler.ServeHTTP(catalogResponse, httptest.NewRequest(http.MethodGet, "/api/discovery?context=test", nil))
	if catalogResponse.Code != http.StatusOK {
		t.Fatalf("CRD catalog request failed: status=%d body=%s", catalogResponse.Code, catalogResponse.Body.String())
	}
	var catalog kube.ResourceCatalog
	if err := json.NewDecoder(catalogResponse.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode CRD catalog: %v", err)
	}
	var beta kube.ResourceDefinition
	for _, definition := range catalog.Items {
		if definition.Group == "widgets.example.com" && definition.Version == "v1beta1" && definition.Resource == "widgets" {
			beta = definition
		}
	}
	capability := beta.CustomResourceCapabilities
	if capability == nil || !beta.CustomResource || capability.PreferredVersion != "v1beta1" || capability.StorageVersion != "v1" || len(capability.ServedVersions) != 2 || !capability.StatusSubresource || !capability.ScaleSubresource || len(capability.PrinterColumns) != 1 {
		t.Fatalf("versioned CRD capabilities were not exposed: %+v", beta)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/resources?context=test&group=widgets.example.com&version=v1beta1&resource=widgets&namespace=team-a&namespaced=true&limit=25", nil))
	var list kube.ResourceList
	if err := json.NewDecoder(listResponse.Body).Decode(&list); err != nil {
		t.Fatalf("decode CRD list: %v", err)
	}
	if listResponse.Code != http.StatusOK || len(list.Items) != 1 || list.Items[0].PrinterColumns["Phase"] != "Ready" || list.Definition.Version != "v1beta1" {
		t.Fatalf("CRD list lost version identity or printer value: status=%d list=%+v", listResponse.Code, list)
	}
}

// TestCRDSchemaHandlerExposesFallbackAndReadOnlyMetadata verifies the server does not hide partial form semantics.
func TestCRDSchemaHandlerExposesFallbackAndReadOnlyMetadata(t *testing.T) {
	server := &Server{resourceSchema: func(context.Context, string, string, string, string) (kube.ResourceSchema, error) {
		return kube.ResourceSchema{
			State:             "available",
			Group:             "widgets.example.com",
			Version:           "v1",
			Resource:          "widgets",
			Kind:              "Widget",
			StatusSubresource: true,
			Fallbacks:         []kube.SchemaFallback{{Path: "$.spec.template", Reason: "unsupported schema construct \"oneOf\""}},
			Schema:            &kube.SchemaField{Type: "object"},
		}, nil
	}}
	response := httptest.NewRecorder()
	server.handleSchema(response, httptest.NewRequest(http.MethodGet, "/api/schema?context=test&group=widgets.example.com&version=v1&resource=widgets", nil))
	var payload kube.ResourceSchema
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode schema response: %v", err)
	}
	if response.Code != http.StatusOK || !payload.StatusSubresource || len(payload.Fallbacks) != 1 || payload.Fallbacks[0].Path != "$.spec.template" {
		t.Fatalf("schema fallback/read-only metadata was lost: status=%d payload=%+v", response.Code, payload)
	}
}
