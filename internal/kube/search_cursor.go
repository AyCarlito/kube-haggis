package kube

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	searchCursorTTL       = 10 * time.Minute
	searchCursorMax       = 64
	searchCursorTokenSize = 24
)

// searchCursorSource stores continuation state for one discovered resource source.
type searchCursorSource struct {
	definition    ResourceDefinition
	continueToken string
	exhausted     bool
	source        SearchSource
}

// searchCursorState stores one immutable boundary in a paginated global search.
type searchCursorState struct {
	Context          string
	Revision         uint64
	Request          SearchRequest
	CatalogState     string
	Issues           []DiscoveryIssue
	CatalogTruncated bool
	Sources          []searchCursorSource
	Pending          []searchCursorItem
}

// searchCursorItem associates one pending safe summary with its source ledger row.
type searchCursorItem struct {
	result      SearchResult
	sourceIndex int
}

// searchCursorEntry stores one cursor state and its bounded in-memory lifetime.
type searchCursorEntry struct {
	state     searchCursorState
	createdAt time.Time
	expiresAt time.Time
}

// searchCursorPage contains one safe page read for a cursor source.
type searchCursorPage struct {
	sourceIndex   int
	items         []SearchResult
	continueToken string
	exhausted     bool
	state         string
	issue         string
	called        bool
}

// newSearchCursorState creates a cursor boundary from one discovered catalog selection.
func newSearchCursorState(contextName string, revision uint64, request SearchRequest, catalog ResourceCatalog, definitions []ResourceDefinition, catalogTruncated bool) searchCursorState {
	request.Cursor = ""
	sources := make([]searchCursorSource, 0, len(definitions))
	for _, definition := range definitions {
		sources = append(sources, searchCursorSource{
			definition: definition,
			source: SearchSource{
				Group:      definition.Group,
				Version:    definition.Version,
				Resource:   definition.Resource,
				Kind:       definition.Kind,
				Scope:      definition.Scope,
				Namespaced: definition.Namespaced,
				State:      searchStateEmpty,
			},
		})
	}
	return searchCursorState{
		Context:          contextName,
		Revision:         revision,
		Request:          request,
		CatalogState:     catalog.State,
		Issues:           cloneDiscoveryIssues(catalog.Issues),
		CatalogTruncated: catalogTruncated,
		Sources:          sources,
		Pending:          make([]searchCursorItem, 0),
	}
}

// searchRequestsEqual compares the immutable filters and page size bound to a cursor.
func searchRequestsEqual(left, right SearchRequest) bool {
	return left.Query == right.Query &&
		left.Group == right.Group &&
		left.Kind == right.Kind &&
		left.Resource == right.Resource &&
		left.Namespace == right.Namespace &&
		left.Name == right.Name &&
		left.LabelSelector == right.LabelSelector &&
		left.PageSize == right.PageSize
}

// cloneSearchCursorState copies cursor-owned slices before a state is advanced or stored.
func cloneSearchCursorState(state searchCursorState) searchCursorState {
	clone := state
	clone.Issues = cloneDiscoveryIssues(state.Issues)
	clone.Sources = append([]searchCursorSource(nil), state.Sources...)
	clone.Pending = make([]searchCursorItem, 0, len(state.Pending))
	for _, item := range state.Pending {
		clone.Pending = append(clone.Pending, searchCursorItem{result: cloneSearchResult(item.result), sourceIndex: item.sourceIndex})
	}
	return clone
}

// cloneSearchResult copies one safe result summary without sharing label maps with cursor state.
func cloneSearchResult(result SearchResult) SearchResult {
	clone := result
	if result.Labels != nil {
		clone.Labels = make(map[string]string, len(result.Labels))
		for key, value := range result.Labels {
			clone.Labels[key] = value
		}
	}
	return clone
}

// loadSearchCursor returns a live cursor snapshot and removes expired sessions.
func (s *Service) loadSearchCursor(token string) (searchCursorState, bool) {
	now := time.Now()
	s.searchCursorMu.Lock()
	defer s.searchCursorMu.Unlock()
	for candidate, entry := range s.searchCursors {
		if !now.Before(entry.expiresAt) {
			delete(s.searchCursors, candidate)
		}
	}
	entry, found := s.searchCursors[token]
	if !found {
		return searchCursorState{}, false
	}
	return cloneSearchCursorState(entry.state), true
}

// storeSearchCursor stores one immutable cursor snapshot with bounded retention.
func (s *Service) storeSearchCursor(state searchCursorState) (string, error) {
	token, err := newSearchCursorToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.searchCursorMu.Lock()
	defer s.searchCursorMu.Unlock()
	if s.searchCursors == nil {
		s.searchCursors = make(map[string]searchCursorEntry)
	}
	for candidate, entry := range s.searchCursors {
		if !now.Before(entry.expiresAt) {
			delete(s.searchCursors, candidate)
		}
	}
	for len(s.searchCursors) >= searchCursorMax {
		oldestToken := ""
		var oldest time.Time
		for candidate, entry := range s.searchCursors {
			if oldestToken == "" || entry.createdAt.Before(oldest) {
				oldestToken = candidate
				oldest = entry.createdAt
			}
		}
		if oldestToken == "" {
			break
		}
		delete(s.searchCursors, oldestToken)
	}
	s.searchCursors[token] = searchCursorEntry{
		state:     cloneSearchCursorState(state),
		createdAt: now,
		expiresAt: now.Add(searchCursorTTL),
	}
	return token, nil
}

// newSearchCursorToken creates a URL-safe random token without embedding search data.
func newSearchCursorToken() (string, error) {
	value := make([]byte, searchCursorTokenSize)
	if _, err := cryptorand.Read(value); err != nil {
		return "", fmt.Errorf("create search cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// advanceSearchCursor fills one user-visible page while respecting source and request budgets.
func (s *Service) advanceSearchCursor(ctx context.Context, client dynamic.Interface, state searchCursorState) (SearchResponse, error) {
	state = cloneSearchCursorState(state)
	items := make([]SearchResult, 0, state.Request.PageSize)
	sourcePageCounts := make([]int, len(state.Sources))
	pageCount := 0
	calledSources := make(map[int]struct{})
	for len(items) < state.Request.PageSize {
		items = appendPendingSearchResults(items, &state.Pending, sourcePageCounts, state.Request.PageSize)
		if len(items) >= state.Request.PageSize || pageCount >= searchPageBudget {
			break
		}
		indexes := activeSearchCursorSources(state.Sources)
		if len(indexes) == 0 {
			break
		}
		if len(indexes) > searchConcurrency {
			indexes = indexes[:searchConcurrency]
		}
		remainingBudget := searchPageBudget - pageCount
		if len(indexes) > remainingBudget {
			indexes = indexes[:remainingBudget]
		}
		pages := make(chan searchCursorPage, len(indexes))
		var waitGroup sync.WaitGroup
		for _, index := range indexes {
			index := index
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				page := listSearchCursorPage(ctx, client, state.Sources[index], state.Request)
				page.sourceIndex = index
				pages <- page
			}()
		}
		waitGroup.Wait()
		close(pages)
		outcomes := make(map[int]searchCursorPage, len(indexes))
		for page := range pages {
			outcomes[page.sourceIndex] = page
		}
		for _, index := range indexes {
			outcome := outcomes[index]
			if outcome.called {
				pageCount++
				calledSources[index] = struct{}{}
			}
			source := &state.Sources[index]
			if outcome.issue != "" {
				source.source.State = outcome.state
				source.source.Issue = outcome.issue
				source.exhausted = true
				source.source.HasMore = false
				continue
			}
			if len(outcome.items) > 0 {
				source.source.State = searchStateAvailable
				for _, item := range outcome.items {
					state.Pending = append(state.Pending, searchCursorItem{result: item, sourceIndex: index})
				}
			} else if source.source.State == "" {
				source.source.State = searchStateEmpty
			}
			source.continueToken = outcome.continueToken
			source.exhausted = outcome.exhausted
			source.source.HasMore = !outcome.exhausted
		}
	}
	items = appendPendingSearchResults(items, &state.Pending, sourcePageCounts, state.Request.PageSize)
	active := activeSearchCursorSources(state.Sources)
	pageBudgetHit := pageCount >= searchPageBudget && len(active) > 0
	response := SearchResponse{
		Context:    state.Context,
		Revision:   state.Revision,
		Items:      items,
		Issues:     cloneDiscoveryIssues(state.Issues),
		CallCount:  len(calledSources),
		CallBudget: searchSourceBudget,
		PageSize:   state.Request.PageSize,
		PageCount:  pageCount,
		PageBudget: searchPageBudget,
		Sources:    make([]SearchSource, 0, len(state.Sources)),
		Truncated:  state.CatalogTruncated || pageBudgetHit,
	}
	for index, source := range state.Sources {
		visible := source.source
		visible.ResultCount = sourcePageCounts[index]
		visible.HasMore = !source.exhausted
		visible.Truncated = pageBudgetHit && !source.exhausted
		response.Sources = append(response.Sources, visible)
	}
	response.HasMore = len(state.Pending) > 0 || len(active) > 0
	response.State = aggregateSearchState(response.Sources, state.CatalogState, response.Issues, response.Truncated, len(response.Items))
	response.Partial = response.State == searchStatePartial
	if response.HasMore {
		nextCursor, err := s.storeSearchCursor(state)
		if err != nil {
			return SearchResponse{}, err
		}
		response.NextCursor = nextCursor
	}
	return response, nil
}

// appendPendingSearchResults moves at most the remaining page capacity out of cursor state.
func appendPendingSearchResults(items []SearchResult, pending *[]searchCursorItem, sourceCounts []int, pageSize int) []SearchResult {
	remaining := pageSize - len(items)
	if remaining <= 0 || len(*pending) == 0 {
		return items
	}
	if remaining > len(*pending) {
		remaining = len(*pending)
	}
	for _, pendingItem := range (*pending)[:remaining] {
		items = append(items, pendingItem.result)
		if pendingItem.sourceIndex >= 0 && pendingItem.sourceIndex < len(sourceCounts) {
			sourceCounts[pendingItem.sourceIndex]++
		}
	}
	*pending = (*pending)[remaining:]
	return items
}

// activeSearchCursorSources returns source indexes that can still produce another page.
func activeSearchCursorSources(sources []searchCursorSource) []int {
	indexes := make([]int, 0, len(sources))
	for index, source := range sources {
		if !source.exhausted {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

// listSearchCursorPage reads one continuation page and filters it into safe summaries.
func listSearchCursorPage(ctx context.Context, client dynamic.Interface, source searchCursorSource, request SearchRequest) searchCursorPage {
	if len(source.definition.Verbs) > 0 && !containsString(source.definition.Verbs, "list") {
		return searchCursorPage{exhausted: true, state: searchStateUnavailable, issue: "This resource is not advertised as listable by Kubernetes."}
	}
	options := metav1.ListOptions{LabelSelector: request.LabelSelector, Continue: source.continueToken, Limit: searchListPageSize}
	if request.Name != "" {
		options.FieldSelector = fields.OneTermEqualSelector("metadata.name", request.Name).String()
	}
	gvr := schema.GroupVersionResource{Group: source.definition.Group, Version: source.definition.Version, Resource: source.definition.Resource}
	items, err := listResource(ctx, client, gvr, request.Namespace, source.definition.Namespaced, options)
	if err != nil {
		state, issue := classifySearchSourceError(source.definition, err)
		return searchCursorPage{called: true, exhausted: true, state: state, issue: issue}
	}
	page := searchCursorPage{called: true, continueToken: items.GetContinue(), exhausted: items.GetContinue() == "", state: searchStateEmpty, items: make([]SearchResult, 0, len(items.Items))}
	for index := range items.Items {
		candidate := summarizeSearchResource(&items.Items[index], source.definition)
		if searchResultMatchesQuery(candidate, request.Query) {
			page.items = append(page.items, candidate)
		}
	}
	if len(page.items) > 0 {
		page.state = searchStateAvailable
	}
	return page
}
