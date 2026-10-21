package kube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// newResourceListDynamicClient creates a dynamic fake with list kinds for generic resource tests.
func newResourceListDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "pods"}:                       "PodList",
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	}, objects...)
}

// preferredResourcesDiscovery overrides the incomplete client-go preferred-resource fake for deterministic discovery tests.
type preferredResourcesDiscovery struct {
	*discoveryfake.FakeDiscovery
	resources []*metav1.APIResourceList
	err       error
}

// ServerPreferredResources returns the configured fake discovery response.
func (d *preferredResourcesDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return d.resources, d.err
}

// TestDiscoverDefinitionsSortsAndCopiesMetadata verifies generic discovery normalization.
func TestDiscoverDefinitionsSortsAndCopiesMetadata(t *testing.T) {
	resources := []*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{{
				Name:       "deployments",
				Kind:       "Deployment",
				Namespaced: true,
				Verbs:      []string{"get", "list", "watch"},
				ShortNames: []string{"deploy"},
				Categories: []string{"all"},
			}},
		},
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{{
				Name:       "pods",
				Kind:       "Pod",
				Namespaced: true,
				Verbs:      []string{"get", "list", "watch"},
			}},
		},
	}
	client := &preferredResourcesDiscovery{
		FakeDiscovery: &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}},
		resources:     resources,
	}

	definitions, err := discoverDefinitions(client)
	if err != nil {
		t.Fatalf("discover definitions: %v", err)
	}
	if len(definitions) != 2 || definitions[0].Resource != "pods" || definitions[1].Resource != "deployments" {
		t.Fatalf("definitions were not stably sorted: %+v", definitions)
	}
	if definitions[1].Kind != "Deployment" || !definitions[1].Namespaced || len(definitions[1].ShortNames) != 1 || definitions[1].ShortNames[0] != "deploy" {
		t.Fatalf("discovery metadata was not preserved: %+v", definitions[1])
	}

	definitions[1].Verbs[0] = "changed"
	if resources[0].APIResources[0].Verbs[0] == "changed" {
		t.Fatal("discovery verbs shared backing storage with the fake response")
	}
}

// TestDiscoverDefinitionsReturnsPartialResultsWithError verifies callers can retain usable discovery after one failure.
func TestDiscoverDefinitionsReturnsPartialResultsWithError(t *testing.T) {
	client := &preferredResourcesDiscovery{
		FakeDiscovery: &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}},
		resources: []*metav1.APIResourceList{{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true}},
		}},
		err: errors.New("apps discovery was denied"),
	}

	definitions, err := discoverDefinitions(client)
	if err == nil || !strings.Contains(err.Error(), "discovery was denied") || len(definitions) != 1 || definitions[0].Resource != "pods" {
		t.Fatalf("partial discovery was not preserved: definitions=%+v err=%v", definitions, err)
	}
}

// TestListResourceUsesNamespaceAndListRestrictions verifies namespaced list routing and query propagation.
func TestListResourceUsesNamespaceAndListRestrictions(t *testing.T) {
	client := newResourceListDynamicClient(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]interface{}{"name": "api", "namespace": "default", "labels": map[string]interface{}{"app": "api"}},
	}})

	items, err := listResource(context.Background(), client, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", true, metav1.ListOptions{Limit: 4, Continue: "next-page", LabelSelector: "app=api"})
	if err != nil {
		t.Fatalf("list namespaced resource: %v", err)
	}
	if len(items.Items) != 1 || items.Items[0].GetName() != "api" {
		t.Fatalf("unexpected namespaced list: %+v", items.Items)
	}
	listAction, ok := client.Actions()[0].(clienttesting.ListAction)
	if !ok {
		t.Fatalf("expected list action, got %T", client.Actions()[0])
	}
	if listAction.GetNamespace() != "default" {
		t.Fatalf("list used the wrong namespace: %q", listAction.GetNamespace())
	}
	restrictions := listAction.GetListRestrictions()
	if restrictions.Labels.String() != "app=api" {
		t.Fatalf("list restrictions were not preserved: %+v", restrictions)
	}

	clusterClient := newResourceListDynamicClient()
	if _, err := listResource(context.Background(), clusterClient, schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "all", true, metav1.ListOptions{}); err != nil {
		t.Fatalf("list all namespaces: %v", err)
	}
	clusterAction, ok := clusterClient.Actions()[0].(clienttesting.ListAction)
	if !ok || clusterAction.GetNamespace() != "" {
		t.Fatalf("all-namespace list was not cluster-scoped: %T namespace=%q", clusterClient.Actions()[0], clusterAction.GetNamespace())
	}
}

// TestListResourcePropagatesRBACDenial verifies generic list failures remain classifiable as permission errors.
func TestListResourcePropagatesRBACDenial(t *testing.T) {
	client := newResourceListDynamicClient()
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pods", nil)
	})

	_, err := listResource(context.Background(), client, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", true, metav1.ListOptions{})
	if err == nil || !apierrors.IsForbidden(err) {
		t.Fatalf("expected RBAC list denial, got %v", err)
	}
}

// TestWatchResourceStreamsNamespacedEventsAndResourceVersion verifies watch routing and concurrency coordinates.
func TestWatchResourceStreamsNamespacedEventsAndResourceVersion(t *testing.T) {
	client := newResourceListDynamicClient()
	fakeWatcher := watch.NewRaceFreeFake()
	client.PrependWatchReactor("pods", func(action clienttesting.Action) (bool, watch.Interface, error) {
		watchAction, ok := action.(clienttesting.WatchAction)
		if !ok {
			t.Fatalf("expected watch action, got %T", action)
		}
		if watchAction.GetNamespace() != "default" {
			t.Fatalf("watch used the wrong namespace: %q", watchAction.GetNamespace())
		}
		if watchAction.GetWatchRestrictions().ResourceVersion != "12" {
			t.Fatalf("watch lost resource version: %+v", watchAction.GetWatchRestrictions())
		}
		return true, fakeWatcher, nil
	})

	result, err := watchResource(context.Background(), client, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", true, metav1.ListOptions{ResourceVersion: "12", AllowWatchBookmarks: true})
	if err != nil {
		t.Fatalf("watch resource: %v", err)
	}
	fakeWatcher.Add(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]interface{}{"name": "api", "namespace": "default"},
	}})
	select {
	case event := <-result.ResultChan():
		if event.Type != watch.Added || event.Object.(*unstructured.Unstructured).GetName() != "api" {
			t.Fatalf("unexpected watch event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("watch event was not delivered")
	}
	fakeWatcher.Stop()
}

// TestWatchResourcePropagatesRBACDenial verifies watch authorization failures reach the caller unchanged.
func TestWatchResourcePropagatesRBACDenial(t *testing.T) {
	client := newResourceListDynamicClient()
	client.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pods", nil)
	})

	_, err := watchResource(context.Background(), client, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", true, metav1.ListOptions{})
	if err == nil || !apierrors.IsForbidden(err) || !strings.Contains(err.Error(), "pods") {
		t.Fatalf("expected RBAC watch denial, got %v", err)
	}
}
