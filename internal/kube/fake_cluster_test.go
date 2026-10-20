package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// TestServiceUsesFakeClusterForDiscoveryWatchListAndApply verifies public service paths against a deterministic API server.
func TestServiceUsesFakeClusterForDiscoveryWatchListAndApply(t *testing.T) {
	var appliedObject map[string]interface{}
	var applyQuery url.Values
	group := func(name string) metav1.APIGroup {
		return metav1.APIGroup{
			TypeMeta: metav1.TypeMeta{Kind: "APIGroup", APIVersion: "v1"},
			Name:     name,
			Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: name + "/v1", Version: "v1"}},
			PreferredVersion: metav1.GroupVersionForDiscovery{
				GroupVersion: name + "/v1",
				Version:      "v1",
			},
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Errorf("encode fake Kubernetes response: %v", err)
			}
		}

		if request.URL.Path == "/api/v1/namespaces/default/pods" && request.URL.Query().Get("watch") == "true" {
			if request.URL.Query().Get("resourceVersion") != "12" || request.URL.Query().Get("allowWatchBookmarks") != "true" {
				t.Errorf("watch query lost safety options: %s", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"type":"ADDED","object":{"apiVersion":"v1","kind":"Pod","metadata":{"name":"api","namespace":"default","resourceVersion":"13"}}}` + "\n"))
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}

		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/namespaces/default/configmaps" {
			if err := json.NewDecoder(request.Body).Decode(&appliedObject); err != nil {
				t.Errorf("decode fake apply request: %v", err)
			}
			applyQuery = request.URL.Query()
			writeJSON(map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":            "settings",
					"namespace":       "default",
					"resourceVersion": "21",
				},
			})
			return
		}

		switch request.URL.Path {
		case "/api":
			writeJSON(metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{"v1"}})
		case "/api/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: []string{"get", "list", "watch", "create", "update"}},
			}})
		case "/apis":
			writeJSON(&metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}, Groups: []metav1.APIGroup{group("apps"), group("apiextensions.k8s.io")}})
		case "/apis/apps/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "watch", "update"}},
			}})
		case "/apis/apiextensions.k8s.io/v1":
			writeJSON(&metav1.APIResourceList{GroupVersion: "apiextensions.k8s.io/v1", APIResources: []metav1.APIResource{
				{Name: "customresourcedefinitions", Kind: "CustomResourceDefinition", Namespaced: false, Verbs: []string{"get", "list"}},
			}})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeJSON(&unstructured.UnstructuredList{Object: map[string]interface{}{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinitionList",
				"metadata":   map[string]interface{}{"resourceVersion": "19"},
			}, Items: []unstructured.Unstructured{}})
		case "/apis/apps/v1/namespaces/default/deployments":
			writeJSON(&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "DeploymentList",
					"metadata":   map[string]interface{}{"resourceVersion": "18", "continue": "next-page"},
				},
				Items: []unstructured.Unstructured{{Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name":              "api",
						"namespace":         "default",
						"uid":               "deployment-uid",
						"resourceVersion":   "17",
						"creationTimestamp": "2026-10-20T10:00:00Z",
						"labels":            map[string]interface{}{"app": "api"},
					},
					"spec":   map[string]interface{}{"replicas": int64(2)},
					"status": map[string]interface{}{"availableReplicas": int64(2)},
				}}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewService(writeTestKubeconfig(t, server.URL))
	definitions, err := service.Discovery(context.Background(), "test")
	if err != nil {
		t.Fatalf("fake-cluster discovery failed: %v", err)
	}
	var deploymentDefinition ResourceDefinition
	for _, definition := range definitions {
		if definition.Group == "apps" && definition.Version == "v1" && definition.Resource == "deployments" {
			deploymentDefinition = definition
		}
	}
	if deploymentDefinition.Kind != "Deployment" || !deploymentDefinition.Namespaced || len(deploymentDefinition.Verbs) == 0 {
		t.Fatalf("deployment discovery metadata was not returned: %+v", deploymentDefinition)
	}

	resources, err := service.ListResources(context.Background(), "test", "apps", "v1", "deployments", "default", true, 3, "app=api")
	if err != nil {
		t.Fatalf("fake-cluster list failed: %v", err)
	}
	if resources.Continue != "next-page" || resources.ResourceVersion != "18" || len(resources.Items) != 1 || resources.Items[0].Name != "api" || resources.Definition.Kind != "Deployment" {
		t.Fatalf("unexpected fake-cluster list: %+v", resources)
	}

	watcher, err := service.WatchResources(context.Background(), "test", "", "v1", "pods", "default", true, "12")
	if err != nil {
		t.Fatalf("fake-cluster watch failed: %v", err)
	}
	defer watcher.Stop()
	select {
	case event := <-watcher.ResultChan():
		if event.Type != watch.Added || event.Object.(*unstructured.Unstructured).GetName() != "api" {
			t.Fatalf("unexpected fake-cluster watch event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("fake-cluster watch did not return an event")
	}

	item, err := service.ApplyResource(context.Background(), "test", "", "v1", "configmaps", "default", true, true, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  namespace: default\n  managedFields:\n  - manager: server\ndata:\n  key: value\n"))
	if err != nil {
		t.Fatalf("fake-cluster apply failed: %v", err)
	}
	if item.GetName() != "settings" || item.GetResourceVersion() != "21" || applyQuery.Get("dryRun") != "All" || applyQuery.Get("fieldManager") != "kube-haggis" {
		t.Fatalf("apply options or response were incorrect: item=%+v query=%v", item, applyQuery)
	}
	metadata, found, metadataErr := unstructured.NestedMap(appliedObject, "metadata")
	if metadataErr != nil || !found {
		t.Fatalf("apply payload metadata was malformed: found=%t err=%v payload=%v", found, metadataErr, appliedObject)
	}
	if _, found := metadata["managedFields"]; found {
		t.Fatal("generic apply sent server-owned managedFields")
	}
}

// TestServiceApplyResourcePropagatesRBACAndConflict verifies public mutation errors remain classifiable.
func TestServiceApplyResourcePropagatesRBACAndConflict(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		reason     metav1.StatusReason
		want       func(error) bool
	}{
		{name: "forbidden", statusCode: http.StatusForbidden, reason: metav1.StatusReasonForbidden, want: apierrors.IsForbidden},
		{name: "conflict", statusCode: http.StatusConflict, reason: metav1.StatusReasonConflict, want: apierrors.IsConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPut {
					t.Errorf("expected resource update, got %s", request.Method)
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

			service := NewService(writeTestKubeconfig(t, server.URL))
			_, err := service.ApplyResource(context.Background(), "test", "apps", "v1", "deployments", "default", true, false, []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: default\n  resourceVersion: \"7\"\n"))
			if err == nil || !test.want(err) {
				t.Fatalf("expected %s error, got %v", test.name, err)
			}
		})
	}
}

// TestServiceApplyResourceRejectsMalformedDocumentBeforeMutation verifies malformed generic documents never reach the API.
func TestServiceApplyResourceRejectsMalformedDocumentBeforeMutation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		http.Error(writer, "unexpected mutation", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := NewService(writeTestKubeconfig(t, server.URL))
	_, err := service.ApplyResource(context.Background(), "test", "", "v1", "configmaps", "default", true, false, []byte("metadata: ["))
	if err == nil || !strings.Contains(err.Error(), "decode resource document") {
		t.Fatalf("expected malformed resource error, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("malformed resource reached the fake API: %d requests", requests)
	}
}
