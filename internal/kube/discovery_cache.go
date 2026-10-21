package kube

import (
	"context"
	"errors"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

const (
	discoveryCacheLoadTimeout    = 5 * time.Second
	discoveryCacheRequestTimeout = 5 * time.Second
)

// discoveryCacheKey identifies one context and one loaded kubeconfig revision.
type discoveryCacheKey struct {
	contextName string
	revision    uint64
}

// discoveryCacheEntry stores normalized discovery and its partial-read error.
type discoveryCacheEntry struct {
	definitions []ResourceDefinition
	err         error
}

// discoveryFlight represents one in-progress discovery load shared by callers.
type discoveryFlight struct {
	done   chan struct{}
	result discoveryCacheEntry
}

// cloneResourceDefinitions returns an isolated copy of discovery metadata.
func cloneResourceDefinitions(definitions []ResourceDefinition) []ResourceDefinition {
	cloned := make([]ResourceDefinition, len(definitions))
	for index, definition := range definitions {
		cloned[index] = definition
		cloned[index].Verbs = append([]string(nil), definition.Verbs...)
		cloned[index].ShortNames = append([]string(nil), definition.ShortNames...)
		cloned[index].Categories = append([]string(nil), definition.Categories...)
	}
	return cloned
}

// cachedDiscovery loads one context's discovery once for the active kubeconfig revision.
func (s *Service) cachedDiscovery(ctx context.Context, contextName string, revision uint64, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface) ([]ResourceDefinition, error) {
	key := discoveryCacheKey{contextName: contextName, revision: revision}
	s.mu.Lock()
	if s.discoveryCache == nil {
		s.discoveryCache = make(map[discoveryCacheKey]discoveryCacheEntry)
	}
	if s.discoveryFlights == nil {
		s.discoveryFlights = make(map[discoveryCacheKey]*discoveryFlight)
	}
	if entry, found := s.discoveryCache[key]; found {
		definitions := cloneResourceDefinitions(entry.definitions)
		s.mu.Unlock()
		return definitions, entry.err
	}
	flight, found := s.discoveryFlights[key]
	if !found {
		flight = &discoveryFlight{done: make(chan struct{})}
		s.discoveryFlights[key] = flight
	}
	s.mu.Unlock()

	if !found {
		go s.loadDiscovery(key, flight, dynamicClient, discoveryClient, context.WithoutCancel(ctx))
	}
	select {
	case <-flight.done:
		return cloneResourceDefinitions(flight.result.definitions), flight.result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// loadDiscovery performs one bounded revision-scoped discovery load and completes its flight.
func (s *Service) loadDiscovery(key discoveryCacheKey, flight *discoveryFlight, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, ctx context.Context) {
	loadContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), discoveryCacheLoadTimeout)
	defer cancel()
	resultChannel := make(chan discoveryCacheEntry, 1)
	go func() {
		definitions, discoveryErr := discoverDefinitions(discoveryClient)
		if loadContext.Err() != nil {
			resultChannel <- discoveryCacheEntry{err: loadContext.Err()}
			return
		}
		markCustomResourceDefinitions(loadContext, dynamicClient, definitions)
		if loadContext.Err() != nil {
			resultChannel <- discoveryCacheEntry{err: loadContext.Err()}
			return
		}
		resultChannel <- discoveryCacheEntry{definitions: cloneResourceDefinitions(definitions), err: discoveryErr}
	}()

	var entry discoveryCacheEntry
	select {
	case entry = <-resultChannel:
	case <-loadContext.Done():
		entry = discoveryCacheEntry{err: loadContext.Err()}
	}

	s.mu.Lock()
	flight.result = entry
	if s.revision == key.revision && !errors.Is(entry.err, context.DeadlineExceeded) && !errors.Is(entry.err, context.Canceled) {
		s.discoveryCache[key] = entry
	}
	if current, stillLoading := s.discoveryFlights[key]; stillLoading && current == flight {
		delete(s.discoveryFlights, key)
	}
	close(flight.done)
	s.mu.Unlock()
}
