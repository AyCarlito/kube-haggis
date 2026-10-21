package kube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	discoveryfake "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

// blockingPreferredResourcesDiscovery holds one preferred-resource call until a test releases it.
type blockingPreferredResourcesDiscovery struct {
	*preferredResourcesDiscovery
	started chan struct{}
	release chan struct{}
}

// ServerPreferredResources blocks deterministically so caller cancellation can be observed.
func (d *blockingPreferredResourcesDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	close(d.started)
	<-d.release
	return nil, nil
}

// TestServiceCachesDiscoverySingleFlightAndInvalidatesOnReload verifies revision-scoped discovery behavior.
func TestServiceCachesDiscoverySingleFlightAndInvalidatesOnReload(t *testing.T) {
	var discoveryRequests atomic.Int32
	firstDiscoveryStarted := make(chan struct{})
	releaseFirstDiscovery := make(chan struct{})
	var firstDiscoveryOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeDiscoveryJSON := func(value interface{}) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(value); err != nil {
				t.Errorf("encode discovery response: %v", err)
			}
		}
		switch request.URL.Path {
		case "/api":
			discoveryRequests.Add(1)
			firstDiscoveryOnce.Do(func() {
				close(firstDiscoveryStarted)
				<-releaseFirstDiscovery
			})
			writeDiscoveryJSON(&metav1.APIVersions{Versions: []string{"v1"}})
		case "/api/v1":
			discoveryRequests.Add(1)
			writeDiscoveryJSON(&metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}}}})
		case "/apis":
			discoveryRequests.Add(1)
			writeDiscoveryJSON(&metav1.APIGroupList{})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeDiscoveryJSON(map[string]interface{}{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "items": []interface{}{}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewService(writeTestKubeconfig(t, server.URL))
	results := make(chan error, 2)
	go func() {
		_, err := service.Discovery(context.Background(), "test")
		results <- err
	}()
	<-firstDiscoveryStarted
	go func() {
		_, err := service.Discovery(context.Background(), "test")
		results <- err
	}()
	close(releaseFirstDiscovery)
	for index := 0; index < 2; index++ {
		err := <-results
		if err != nil {
			t.Fatalf("concurrent discovery failed: %v", err)
		}
	}

	firstLoadRequests := discoveryRequests.Load()
	if firstLoadRequests == 0 {
		t.Fatal("discovery server observed no discovery requests")
	}
	if _, err := service.Discovery(context.Background(), "test"); err != nil {
		t.Fatalf("cached discovery failed: %v", err)
	}
	if discoveryRequests.Load() != firstLoadRequests {
		t.Fatalf("cached discovery performed new API discovery requests: before=%d after=%d", firstLoadRequests, discoveryRequests.Load())
	}

	if _, err := service.ReloadContexts(); err != nil {
		t.Fatalf("forced kubeconfig reload failed: %v", err)
	}
	if _, err := service.Discovery(context.Background(), "test"); err != nil {
		t.Fatalf("discovery after reload failed: %v", err)
	}
	if discoveryRequests.Load() <= firstLoadRequests {
		t.Fatalf("kubeconfig reload did not invalidate discovery cache: before=%d after=%d", firstLoadRequests, discoveryRequests.Load())
	}
}

// TestCachedDiscoveryHonorsCallerCancellationAndCleansFlight verifies canceled callers do not strand a flight.
func TestCachedDiscoveryHonorsCallerCancellationAndCleansFlight(t *testing.T) {
	discoveryClient := &blockingPreferredResourcesDiscovery{
		preferredResourcesDiscovery: &preferredResourcesDiscovery{
			FakeDiscovery: &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := NewService("")
	callerContext, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.cachedDiscovery(callerContext, "test", 0, nil, discoveryClient)
		result <- err
	}()
	<-discoveryClient.started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery caller returned the wrong error: %v", err)
	}

	close(discoveryClient.release)
	if _, err := service.cachedDiscovery(context.Background(), "test", 0, nil, discoveryClient); err != nil {
		t.Fatalf("completed cache flight returned an error: %v", err)
	}
	service.mu.Lock()
	_, stillLoading := service.discoveryFlights[discoveryCacheKey{contextName: "test", revision: 0}]
	service.mu.Unlock()
	if stillLoading {
		t.Fatal("completed discovery flight was not cleaned up")
	}
}
