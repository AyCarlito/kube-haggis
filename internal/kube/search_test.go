package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestSearchSourceUsesExactGVRAndServerSelectors verifies field and label filters reach one catalog GVR.
func TestSearchSourceUsesExactGVRAndServerSelectors(t *testing.T) {
	client := newResourceListDynamicClient(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "api",
			"namespace": "team-a",
			"labels":    map[string]interface{}{"app": "api"},
		},
		"spec": map[string]interface{}{"private": "private-value"},
	}})
	definition := ResourceDefinition{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"}}
	result := searchSourceForDefinition(context.Background(), client, definition, SearchRequest{
		Namespace:     "team-a",
		Name:          "api",
		LabelSelector: "app=api",
	}, 1)
	if result.source.State != searchStateAvailable || len(result.items) != 1 || result.items[0].Group != "apps" || result.items[0].Version != "v1" || result.items[0].Resource != "deployments" {
		t.Fatalf("search source lost exact GVR or result: %+v", result)
	}
	listAction, ok := client.Actions()[0].(clienttesting.ListAction)
	if !ok {
		t.Fatalf("expected list action, got %T", client.Actions()[0])
	}
	if listAction.GetResource() != (schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}) || listAction.GetNamespace() != "team-a" {
		t.Fatalf("search used the wrong GVR or namespace: resource=%s namespace=%q", listAction.GetResource(), listAction.GetNamespace())
	}
	restrictions := listAction.GetListRestrictions()
	if restrictions.Labels.String() != "app=api" || restrictions.Fields.String() != "metadata.name=api" {
		t.Fatalf("search selectors were not propagated: labels=%q fields=%q", restrictions.Labels.String(), restrictions.Fields.String())
	}
	encoded, err := json.Marshal(result.items[0])
	if err != nil {
		t.Fatalf("encode safe search result: %v", err)
	}
	if strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "spec") {
		t.Fatalf("search result exposed an object body: %s", encoded)
	}
}

// TestSearchSourcePaginatesFreeTextMatches verifies a later list page can satisfy a free-text object-name query.
func TestSearchSourcePaginatesFreeTextMatches(t *testing.T) {
	client := newResourceListDynamicClient()
	page := 0
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		page++
		if page == 1 {
			items := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata":   map[string]interface{}{"name": "controller", "namespace": "team-a"},
			}}}}
			items.SetContinue("page-2")
			return true, items, nil
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]interface{}{"name": "operator-controller", "namespace": "team-a"},
		}}}}, nil
	})

	result := searchSourceForDefinition(context.Background(), client, ResourceDefinition{
		Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"},
	}, SearchRequest{Namespace: "all", Query: "operator"}, 2)
	if page != 2 || result.source.State != searchStateAvailable || len(result.items) != 1 || result.items[0].Name != "operator-controller" {
		t.Fatalf("search did not follow a continuation page for a free-text match: pages=%d result=%+v", page, result)
	}
	if result.source.Truncated {
		t.Fatalf("completed continuation pages were incorrectly marked truncated: %+v", result.source)
	}
}

// TestSearchSourceMarksPageBudgetTruncation verifies an unfinished continuation is surfaced as partial coverage.
func TestSearchSourceMarksPageBudgetTruncation(t *testing.T) {
	client := newResourceListDynamicClient()
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		items := &unstructured.UnstructuredList{}
		items.SetContinue("more")
		return true, items, nil
	})

	result := searchSourceForDefinition(context.Background(), client, ResourceDefinition{
		Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"},
	}, SearchRequest{Namespace: "all", Query: "operator"}, 2)
	if result.pages != 2 || !result.source.Truncated {
		t.Fatalf("page budget was not surfaced: %+v", result)
	}
}

// TestAdvanceSearchCursorReturnsSubsequentPages verifies pending summaries and Kubernetes continuation state survive page boundaries.
func TestAdvanceSearchCursorReturnsSubsequentPages(t *testing.T) {
	client := newResourceListDynamicClient()
	page := 0
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		page++
		if page == 1 {
			items := make([]unstructured.Unstructured, 25)
			for index := range items {
				items[index] = unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Pod",
					"metadata":   map[string]interface{}{"name": fmt.Sprintf("operator-%02d", index), "namespace": "team-a"},
				}}
			}
			list := &unstructured.UnstructuredList{Items: items}
			list.SetContinue("page-2")
			return true, list, nil
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]interface{}{"name": "operator-25", "namespace": "team-a"},
		}}}}, nil
	})

	service := NewService("")
	state := newSearchCursorState("test", 1, SearchRequest{Namespace: "all", Query: "operator", PageSize: 25}, ResourceCatalog{State: searchStateAvailable}, []ResourceDefinition{{
		Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"},
	}}, false)
	first, err := service.advanceSearchCursor(context.Background(), client, state)
	if err != nil || len(first.Items) != 25 || len(first.Sources) != 1 || first.Sources[0].ResultCount != 25 || !first.HasMore || first.NextCursor == "" || first.PageCount != 1 {
		t.Fatalf("first cursor page was not bounded correctly: response=%+v err=%v", first, err)
	}
	nextState, found := service.loadSearchCursor(first.NextCursor)
	if !found {
		t.Fatal("first response did not retain its opaque cursor state")
	}
	second, err := service.advanceSearchCursor(context.Background(), client, nextState)
	if err != nil || len(second.Items) != 1 || second.Items[0].Name != "operator-25" || second.HasMore || second.NextCursor != "" || second.PageCount != 1 {
		t.Fatalf("second cursor page was not complete: response=%+v err=%v", second, err)
	}
}

// TestSearchSourcesClassifyPartialRBACAndRedactErrors verifies one denied source does not erase usable results.
func TestSearchSourcesClassifyPartialRBACAndRedactErrors(t *testing.T) {
	client := newResourceListDynamicClient(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]interface{}{"name": "api", "namespace": "team-a"},
	}})
	client.PrependReactor("list", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "deployments", nil)
	})
	definitions := []ResourceDefinition{
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"}},
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"}},
	}
	results := searchDiscoveredSources(context.Background(), client, definitions, SearchRequest{Namespace: "team-a"})
	if len(results) != 2 {
		t.Fatalf("expected both sources, got %d", len(results))
	}
	var denied searchSourceResult
	var available searchSourceResult
	for _, result := range results {
		if result.source.Resource == "deployments" {
			denied = result
		} else {
			available = result
		}
	}
	if denied.source.State != searchStatePermissionDenied || !strings.Contains(denied.source.Issue, "RBAC") || len(available.items) != 1 {
		t.Fatalf("partial source state was not explicit: denied=%+v available=%+v", denied, available)
	}
	if strings.Contains(denied.source.Issue, "forbidden") && strings.Contains(denied.source.Issue, "private") {
		t.Fatalf("denied source leaked backend error text: %q", denied.source.Issue)
	}
}

// TestSearchDefinitionsKeepsObjectNameSourcesWhenCatalogMatchesQuery verifies free text does not discard object-name sources.
func TestSearchDefinitionsKeepsObjectNameSourcesWhenCatalogMatchesQuery(t *testing.T) {
	definitions := []ResourceDefinition{
		{Group: "example.com", Version: "v1", Resource: "apis", Kind: "API", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"}},
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Scope: "Namespaced", Verbs: []string{"list"}},
	}
	request, err := normalizeSearchRequest(SearchRequest{Query: "api", Namespace: "team-a"})
	if err != nil {
		t.Fatalf("normalize object-name search: %v", err)
	}
	candidates := searchDefinitions(definitions, request)
	if len(candidates) != len(definitions) {
		t.Fatalf("free-text query narrowed catalog sources before object matching: got %d candidates", len(candidates))
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "example.com", Version: "v1", Resource: "apis"}: "APIList",
		{Version: "v1", Resource: "pods"}:                       "PodList",
	}, &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]interface{}{"name": "api", "namespace": "team-a"},
	}})
	results := searchDiscoveredSources(context.Background(), client, candidates, request)
	for _, result := range results {
		for _, item := range result.items {
			if item.Resource == "pods" && item.Name == "api" {
				return
			}
		}
	}
	t.Fatalf("object-name match was not returned from the retained candidate sources: %+v", results)
}

// TestSearchSourcesHonorsConcurrencyCap verifies the fixed six-source concurrency ceiling.
func TestSearchSourcesHonorsConcurrencyCap(t *testing.T) {
	listKinds := make(map[schema.GroupVersionResource]string)
	for index := 0; index < 12; index++ {
		listKinds[schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets-" + string(rune('a'+index))}] = "WidgetList"
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	var mutex sync.Mutex
	active := 0
	maximum := 0
	client.PrependReactor("list", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
		mutex.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mutex.Unlock()
		time.Sleep(5 * time.Millisecond)
		mutex.Lock()
		active--
		mutex.Unlock()
		return true, &unstructured.UnstructuredList{Object: map[string]interface{}{"apiVersion": "v1", "kind": "ResourceList"}}, nil
	})
	definitions := make([]ResourceDefinition, 12)
	for index := range definitions {
		definitions[index] = ResourceDefinition{Group: "example.com", Version: "v1", Resource: "widgets-" + string(rune('a'+index)), Kind: "Widget", Namespaced: false, Scope: "Cluster", Verbs: []string{"list"}}
	}
	results := searchDiscoveredSources(context.Background(), client, definitions, SearchRequest{Namespace: "all"})
	if len(results) != len(definitions) || maximum > searchConcurrency {
		t.Fatalf("search exceeded concurrency budget: sources=%d maximum=%d budget=%d", len(results), maximum, searchConcurrency)
	}
}

// TestBoundSearchDefinitionsHonorsSourceBudget verifies discovery fan-out never exceeds the fixed source cap.
func TestBoundSearchDefinitionsHonorsSourceBudget(t *testing.T) {
	definitions := make([]ResourceDefinition, searchSourceBudget+3)
	bounded, truncated := boundSearchDefinitions(definitions)
	if !truncated || len(bounded) != searchSourceBudget {
		t.Fatalf("source budget was not enforced: truncated=%t count=%d budget=%d", truncated, len(bounded), searchSourceBudget)
	}
}

// TestNormalizeSearchRequestRejectsUnsafeFilters verifies invalid label selectors do not reach Kubernetes.
func TestNormalizeSearchRequestRejectsUnsafeFilters(t *testing.T) {
	if _, err := normalizeSearchRequest(SearchRequest{LabelSelector: "app in ("}); err == nil || !strings.Contains(err.Error(), "label selector") {
		t.Fatalf("invalid label selector was accepted: %v", err)
	}
	if _, err := normalizeSearchRequest(SearchRequest{Query: strings.Repeat("x", searchFilterMax+1)}); err == nil {
		t.Fatal("oversized search filter was accepted")
	}
	if _, err := normalizeSearchRequest(SearchRequest{Name: "unsafe/name"}); err == nil {
		t.Fatal("unsafe metadata.name field selector was accepted")
	}
	if _, err := normalizeSearchRequest(SearchRequest{PageSize: 30}); err == nil {
		t.Fatal("unsupported search page size was accepted")
	}
}
