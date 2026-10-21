package kube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	discoveryfake "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

// completeCatalogDiscovery provides deterministic all-version discovery for catalog tests.
type completeCatalogDiscovery struct {
	*discoveryfake.FakeDiscovery
	resources []*metav1.APIResourceList
	err       error
}

// ServerGroupsAndResources returns the configured complete discovery response.
func (d *completeCatalogDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, d.resources, d.err
}

// TestNormalizeResourceListsBuildsCompleteIdentityAndCapabilities verifies GVR, scope, metadata, and subresource folding.
func TestNormalizeResourceListsBuildsCompleteIdentityAndCapabilities(t *testing.T) {
	definitions := normalizeResourceLists([]*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"watch", "get", "list"}, ShortNames: []string{"po"}, Categories: []string{"all"}},
				{Name: "nodes", Kind: "Node", Namespaced: false, Verbs: []string{"list"}},
			},
		},
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "watch", "update"}},
				{Name: "deployments/status", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "update"}},
				{Name: "deployments/scale", Kind: "Scale", Namespaced: true, Verbs: []string{"get", "update"}},
			},
		},
		{
			GroupVersion: "apps/v1beta1",
			APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list"}}},
		},
		{
			GroupVersion: "widgets.example.com/v1",
			APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: false, Verbs: []string{"get", "list", "watch"}}},
		},
	})

	if len(definitions) != 5 {
		t.Fatalf("expected one row per top-level GVR, got %d: %+v", len(definitions), definitions)
	}
	var pods ResourceDefinition
	for _, definition := range definitions {
		if definition.Group == "" && definition.Version == "v1" && definition.Resource == "pods" {
			pods = definition
		}
	}
	if pods.Resource != "pods" || pods.Scope != "Namespaced" || pods.Verbs[0] != "get" {
		t.Fatalf("core resource identity or metadata was not normalized: %+v", pods)
	}
	var deployment, widget ResourceDefinition
	for _, definition := range definitions {
		if definition.Group == "apps" && definition.Version == "v1" {
			deployment = definition
		}
		if definition.Group == "widgets.example.com" {
			widget = definition
		}
		if strings.Contains(definition.Resource, "/") {
			t.Fatalf("subresource became a listable catalog row: %+v", definition)
		}
	}
	if deployment.Kind != "Deployment" || deployment.Scope != "Namespaced" || len(deployment.Subresources) != 2 {
		t.Fatalf("deployment capabilities were not attached: %+v", deployment)
	}
	if deployment.Subresources[0].Name != "scale" || deployment.Subresources[1].Name != "status" {
		t.Fatalf("subresources were not sorted or preserved: %+v", deployment.Subresources)
	}
	if widget.Resource != "widgets" || widget.Scope != "Cluster" || widget.Kind != "Widget" {
		t.Fatalf("custom resource identity was incomplete: %+v", widget)
	}
}

// TestDiscoverCompleteDefinitionsRetainsAllVersionsAndPartialIssues verifies partial discovery is explicit and usable.
func TestDiscoverCompleteDefinitionsRetainsAllVersionsAndPartialIssues(t *testing.T) {
	client := &completeCatalogDiscovery{
		FakeDiscovery: &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}},
		resources: []*metav1.APIResourceList{
			{GroupVersion: "example.com/v1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}}},
			{GroupVersion: "example.com/v1beta1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}}},
		},
		err: &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{
			{Group: "blocked.example.com", Version: "v1"}: apierrors.NewForbidden(schema.GroupResource{Group: "blocked.example.com", Resource: "widgets"}, "widgets", nil),
		}},
	}
	definitions, issues, err := discoverCompleteDefinitions(client)
	if err == nil || len(definitions) != 2 || len(issues) != 1 {
		t.Fatalf("partial complete discovery was not retained: definitions=%+v issues=%+v err=%v", definitions, issues, err)
	}
	if issues[0].State != resourceCatalogStatePermissionDenied || issues[0].Group != "blocked.example.com" || !issues[0].Retryable {
		t.Fatalf("RBAC discovery issue was not classified: %+v", issues[0])
	}
}

// TestNormalizeResourceListsDoesNotTruncateLargeCatalog verifies every discovered CRD-sized catalog row is retained.
func TestNormalizeResourceListsDoesNotTruncateLargeCatalog(t *testing.T) {
	const resourceCount = 2048
	resources := make([]metav1.APIResource, 0, resourceCount)
	for index := 0; index < resourceCount; index++ {
		resources = append(resources, metav1.APIResource{Name: "widgets-" + string(rune('a'+index%26)) + "-" + strconv.Itoa(index), Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}})
	}
	definitions := normalizeResourceLists([]*metav1.APIResourceList{{GroupVersion: "example.com/v1", APIResources: resources}})
	if len(definitions) != resourceCount {
		t.Fatalf("large discovery catalog was truncated: got %d, want %d", len(definitions), resourceCount)
	}
}

// TestListResourcesRejectsCallerScopeContradictions verifies namespaced requests use discovery scope instead of query trust.
func TestListResourcesRejectsCallerScopeContradictions(t *testing.T) {
	listRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeCatalogTestJSON := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Fatalf("encode catalog test response: %v", err)
			}
		}
		switch request.URL.Path {
		case "/api":
			writeCatalogTestJSON(&metav1.APIVersions{Versions: []string{"v1"}})
		case "/api/v1":
			writeCatalogTestJSON(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}}}})
		case "/apis":
			writeCatalogTestJSON(&metav1.APIGroupList{})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeCatalogTestJSON(map[string]interface{}{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "items": []interface{}{}})
		case "/api/v1/namespaces/default/pods":
			listRequests++
			writeCatalogTestJSON(map[string]interface{}{"apiVersion": "v1", "kind": "PodList", "items": []interface{}{}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewService(writeTestKubeconfig(t, server.URL))
	_, err := service.ListResources(context.Background(), "test", "", "v1", "pods", "default", false, 25, "", "")
	if err == nil || !strings.Contains(err.Error(), "scope contradicts discovery") || listRequests != 0 {
		t.Fatalf("contradictory scope reached the Kubernetes list path: err=%v listRequests=%d", err, listRequests)
	}
}

// TestDiscoveryIssueForErrorClassifiesUnavailableAPIs verifies retryable unavailable discovery state.
func TestDiscoveryIssueForErrorClassifiesUnavailableAPIs(t *testing.T) {
	issue := discoveryIssueForError("metrics.k8s.io", "v1beta1", errors.New("connection refused"))
	if issue.State != resourceCatalogStateUnavailable || !issue.Retryable || !strings.Contains(issue.Message, "metrics.k8s.io/v1beta1") {
		t.Fatalf("connection discovery issue was not unavailable: %+v", issue)
	}
	issue = discoveryIssueForError("metrics.k8s.io", "v1beta1", apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "v1beta1"}, "v1beta1"))
	if issue.State != resourceCatalogStateUnavailable {
		t.Fatalf("not-found discovery issue was not unavailable: %+v", issue)
	}
}
