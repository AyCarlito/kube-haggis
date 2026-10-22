package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

const (
	searchStateAvailable        = "available"
	searchStatePartial          = "partial"
	searchStateEmpty            = "empty"
	searchStatePermissionDenied = "permission-denied"
	searchStateUnavailable      = "unavailable"
	searchStateError            = "error"

	searchRequestTimeout  = 8 * time.Second
	searchSourceBudget    = 24
	searchConcurrency     = 6
	searchListPageSize    = 100
	searchPageBudget      = 24
	searchDefaultPageSize = 100
	searchFilterMax       = 256
	searchLabelMax        = 24
	searchLabelKeyMax     = 128
	searchLabelValueMax   = 256
)

// SearchRequest contains the bounded filters and page controls accepted by the global search endpoint.
type SearchRequest struct {
	Query         string
	Group         string
	Kind          string
	Resource      string
	Namespace     string
	Name          string
	LabelSelector string
	PageSize      int
	Cursor        string
}

// SearchResult contains only the GVR coordinates and safe summary fields needed for navigation.
type SearchResult struct {
	Group      string            `json:"group"`
	Version    string            `json:"version"`
	Resource   string            `json:"resource"`
	Kind       string            `json:"kind"`
	Scope      string            `json:"scope"`
	Namespaced bool              `json:"namespaced"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	Status     string            `json:"status"`
	Ready      string            `json:"ready,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// SearchSource reports the bounded outcome of listing one exact discovered GVR.
type SearchSource struct {
	Group       string `json:"group"`
	Version     string `json:"version"`
	Resource    string `json:"resource"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	Namespaced  bool   `json:"namespaced"`
	State       string `json:"state"`
	ResultCount int    `json:"resultCount"`
	HasMore     bool   `json:"hasMore,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Issue       string `json:"issue,omitempty"`
}

// SearchResponse contains one bounded cross-resource result page, cursor state, and safe catalog health.
type SearchResponse struct {
	Context    string           `json:"context"`
	Revision   uint64           `json:"revision"`
	State      string           `json:"state"`
	Items      []SearchResult   `json:"items"`
	Sources    []SearchSource   `json:"sources"`
	Issues     []DiscoveryIssue `json:"issues,omitempty"`
	Partial    bool             `json:"partial"`
	Truncated  bool             `json:"truncated"`
	CallCount  int              `json:"callCount"`
	CallBudget int              `json:"callBudget"`
	PageSize   int              `json:"pageSize"`
	PageCount  int              `json:"pageCount"`
	PageBudget int              `json:"pageBudget"`
	HasMore    bool             `json:"hasMore"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// searchSourceResult keeps source-local summaries private for bounded source fan-out tests.
type searchSourceResult struct {
	source SearchSource
	items  []SearchResult
	called bool
	pages  int
}

// Search executes one bounded page of a read-only search across the revision-scoped discovery catalog.
func (s *Service) Search(ctx context.Context, contextName string, request SearchRequest) (SearchResponse, error) {
	normalized, err := normalizeSearchRequest(request)
	if err != nil {
		return SearchResponse{}, err
	}
	searchContext, cancel := context.WithTimeout(ctx, searchRequestTimeout)
	defer cancel()

	dynamicClient, discoveryClient, _, resolvedContext, revision, err := s.clientsWithRevision(contextName)
	if err != nil {
		return SearchResponse{}, err
	}
	if normalized.Cursor != "" {
		state, found := s.loadSearchCursor(normalized.Cursor)
		if !found {
			return SearchResponse{}, fmt.Errorf("search cursor is expired or invalid")
		}
		if state.Context != resolvedContext || state.Revision != revision || !searchRequestsEqual(state.Request, normalized) {
			return SearchResponse{}, fmt.Errorf("search cursor is stale")
		}
		return s.advanceSearchCursor(searchContext, dynamicClient, state)
	}
	catalog, discoveryErr := s.cachedDiscoveryCatalog(searchContext, resolvedContext, revision, dynamicClient, discoveryClient)
	response := SearchResponse{
		Context:    resolvedContext,
		Revision:   revision,
		Issues:     cloneDiscoveryIssues(catalog.Issues),
		CallBudget: searchSourceBudget,
		PageSize:   normalized.PageSize,
		PageBudget: searchPageBudget,
		Items:      make([]SearchResult, 0),
		Sources:    make([]SearchSource, 0),
	}
	if discoveryErr != nil && (errors.Is(discoveryErr, context.Canceled) || errors.Is(discoveryErr, context.DeadlineExceeded)) {
		response.State = searchStateUnavailable
		response.Partial = true
		response.Issues = append(response.Issues, DiscoveryIssue{State: searchStateUnavailable, Message: "Kubernetes discovery did not respond before the search deadline.", Retryable: true})
		return response, nil
	}

	definitions := searchDefinitions(catalog.Items, normalized)
	definitions, definitionsTruncated := boundSearchDefinitions(definitions)
	state := newSearchCursorState(resolvedContext, revision, normalized, catalog, definitions, definitionsTruncated)
	return s.advanceSearchCursor(searchContext, dynamicClient, state)
}

// normalizeSearchRequest trims and validates the browser-controlled search filters.
func normalizeSearchRequest(request SearchRequest) (SearchRequest, error) {
	request.Query = strings.TrimSpace(request.Query)
	request.Group = strings.TrimSpace(request.Group)
	request.Kind = strings.TrimSpace(request.Kind)
	request.Resource = strings.TrimSpace(request.Resource)
	request.Namespace = strings.TrimSpace(request.Namespace)
	request.Name = strings.TrimSpace(request.Name)
	request.LabelSelector = strings.TrimSpace(request.LabelSelector)
	request.Cursor = strings.TrimSpace(request.Cursor)
	if request.PageSize == 0 {
		request.PageSize = searchDefaultPageSize
	}
	if request.Namespace == "" {
		request.Namespace = "all"
	}
	for _, value := range []string{request.Query, request.Group, request.Kind, request.Resource, request.Namespace, request.Name, request.LabelSelector, request.Cursor} {
		if len(value) > searchFilterMax || strings.ContainsAny(value, "\x00\r\n") {
			return SearchRequest{}, fmt.Errorf("search filter is invalid")
		}
	}
	if request.PageSize != 25 && request.PageSize != 50 && request.PageSize != 100 {
		return SearchRequest{}, fmt.Errorf("search filter is invalid")
	}
	if request.LabelSelector != "" {
		if _, err := labels.Parse(request.LabelSelector); err != nil {
			return SearchRequest{}, fmt.Errorf("label selector is invalid")
		}
	}
	if request.Name != "" && len(utilvalidation.IsDNS1123Subdomain(request.Name)) > 0 {
		return SearchRequest{}, fmt.Errorf("search filter is invalid")
	}
	if request.Namespace != "all" && len(utilvalidation.IsDNS1123Label(request.Namespace)) > 0 {
		return SearchRequest{}, fmt.Errorf("search filter is invalid")
	}
	return request, nil
}

// searchDefinitions filters catalog rows before any Kubernetes list call is scheduled.
func searchDefinitions(definitions []ResourceDefinition, request SearchRequest) []ResourceDefinition {
	candidates := make([]ResourceDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if !searchDefinitionMatchesFilters(definition, request) {
			continue
		}
		candidates = append(candidates, definition)
	}
	// Free text is also an object-name query, so retain every exact-filter candidate and
	// let the bounded summaries decide whether the query matches each object.
	return candidates
}

// searchDefinitionMatchesFilters matches exact catalog identity fields without resolving short names.
func searchDefinitionMatchesFilters(definition ResourceDefinition, request SearchRequest) bool {
	if request.Group != "" {
		if strings.EqualFold(request.Group, "core") {
			if definition.Group != "" {
				return false
			}
		} else if !strings.EqualFold(definition.Group, request.Group) {
			return false
		}
	}
	if request.Kind != "" && !strings.EqualFold(definition.Kind, request.Kind) {
		return false
	}
	if request.Resource != "" && !strings.EqualFold(definition.Resource, request.Resource) {
		return false
	}
	return true
}

// boundSearchDefinitions applies the fixed discovered-resource fan-out budget before scheduling calls.
func boundSearchDefinitions(definitions []ResourceDefinition) ([]ResourceDefinition, bool) {
	if len(definitions) <= searchSourceBudget {
		return definitions, false
	}
	return definitions[:searchSourceBudget], true
}

// searchDiscoveredSources lists selected GVRs with a fixed concurrency cap.
func searchDiscoveredSources(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, request SearchRequest) []searchSourceResult {
	if len(definitions) == 0 {
		return nil
	}
	semaphore := make(chan struct{}, searchConcurrency)
	results := make(chan searchSourceResult, len(definitions))
	pagesPerSource := searchPagesPerSource(len(definitions))
	var waitGroup sync.WaitGroup
	for _, definition := range definitions {
		definition := definition
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- searchSourceResult{source: SearchSource{
					Group:      definition.Group,
					Version:    definition.Version,
					Resource:   definition.Resource,
					Kind:       definition.Kind,
					Scope:      definition.Scope,
					Namespaced: definition.Namespaced,
					State:      searchStateUnavailable,
					Issue:      "The search deadline was reached before this resource could be read.",
				}}
				return
			}
			results <- searchSourceForDefinition(ctx, client, definition, request, pagesPerSource)
		}()
	}
	waitGroup.Wait()
	close(results)
	collected := make([]searchSourceResult, 0, len(definitions))
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

// searchPagesPerSource divides the fixed page budget across the selected resource sources.
func searchPagesPerSource(sourceCount int) int {
	if sourceCount <= 0 {
		return searchPageBudget
	}
	pages := searchPageBudget / sourceCount
	if pages < 1 {
		return 1
	}
	return pages
}

// searchSourceForDefinition lists one exact catalog definition across bounded pages and returns a safe source outcome.
func searchSourceForDefinition(ctx context.Context, client dynamic.Interface, definition ResourceDefinition, request SearchRequest, pageBudget int) searchSourceResult {
	source := SearchSource{
		Group:      definition.Group,
		Version:    definition.Version,
		Resource:   definition.Resource,
		Kind:       definition.Kind,
		Scope:      definition.Scope,
		Namespaced: definition.Namespaced,
		State:      searchStateEmpty,
	}
	if len(definition.Verbs) > 0 && !containsString(definition.Verbs, "list") {
		source.State = searchStateUnavailable
		source.Issue = "This resource is not advertised as listable by Kubernetes discovery."
		return searchSourceResult{source: source}
	}

	if pageBudget < 1 {
		pageBudget = 1
	}
	options := metav1.ListOptions{LabelSelector: request.LabelSelector, Limit: searchListPageSize}
	if request.Name != "" {
		options.FieldSelector = fields.OneTermEqualSelector("metadata.name", request.Name).String()
	}
	gvr := schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}
	result := searchSourceResult{source: source, called: true, items: make([]SearchResult, 0)}
	for page := 0; page < pageBudget; page++ {
		items, err := listResource(ctx, client, gvr, request.Namespace, definition.Namespaced, options)
		result.pages++
		if err != nil {
			result.source.State, result.source.Issue = classifySearchSourceError(definition, err)
			return result
		}
		for index := range items.Items {
			candidate := summarizeSearchResource(&items.Items[index], definition)
			if searchResultMatchesQuery(candidate, request.Query) {
				result.items = append(result.items, candidate)
			}
		}
		continuation := items.GetContinue()
		if continuation == "" {
			break
		}
		options.Continue = continuation
		if page == pageBudget-1 {
			result.source.Truncated = true
		}
	}
	result.source.State = searchStateEmpty
	if len(result.items) > 0 {
		result.source.State = searchStateAvailable
	}
	return result
}

// searchResultMatchesQuery filters safe summaries after the Kubernetes server-side selectors run.
func searchResultMatchesQuery(result SearchResult, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{result.Group, result.Version, result.Resource, result.Kind, result.Namespace, result.Name}, " "))
	return strings.Contains(text, needle)
}

// summarizeSearchResource creates a redacted, bounded summary without returning a Kubernetes object body.
func summarizeSearchResource(item *unstructured.Unstructured, definition ResourceDefinition) SearchResult {
	return SearchResult{
		Group:      definition.Group,
		Version:    definition.Version,
		Resource:   definition.Resource,
		Kind:       firstNonEmpty(item.GetKind(), definition.Kind),
		Scope:      definition.Scope,
		Namespaced: definition.Namespaced,
		Name:       item.GetName(),
		Namespace:  item.GetNamespace(),
		CreatedAt:  item.GetCreationTimestamp().Time.UTC(),
		Status:     resourceStatus(item),
		Ready:      resourceReady(item),
		Labels:     boundedSearchLabels(item.GetLabels()),
	}
}

// boundedSearchLabels limits label keys and values before they enter a browser response.
func boundedSearchLabels(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > searchLabelMax {
		keys = keys[:searchLabelMax]
	}
	bounded := make(map[string]string, len(keys))
	for _, originalKey := range keys {
		key := originalKey
		if len(originalKey) > searchLabelKeyMax {
			key = originalKey[:searchLabelKeyMax]
		}
		value := values[originalKey]
		if len(value) > searchLabelValueMax {
			value = value[:searchLabelValueMax]
		}
		bounded[key] = value
	}
	return bounded
}

// classifySearchSourceError converts one Kubernetes list failure into a safe operator message.
func classifySearchSourceError(definition ResourceDefinition, err error) (string, string) {
	coordinate := searchDefinitionCoordinate(definition)
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return searchStatePermissionDenied, fmt.Sprintf("Kubernetes RBAC denied list access for %s.", coordinate)
	case apierrors.IsNotFound(err):
		return searchStateUnavailable, fmt.Sprintf("The Kubernetes API for %s is unavailable or no longer serves this resource.", coordinate)
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || discoveryErrorLooksUnavailable(err):
		return searchStateUnavailable, fmt.Sprintf("The Kubernetes API for %s did not respond to the search.", coordinate)
	default:
		return searchStateError, fmt.Sprintf("Search could not read %s; retry the search.", coordinate)
	}
}

// searchDefinitionCoordinate formats one complete GVR for safe source diagnostics.
func searchDefinitionCoordinate(definition ResourceDefinition) string {
	if definition.Group == "" {
		return definition.Version + "/" + definition.Resource
	}
	return definition.Group + "/" + definition.Version + "/" + definition.Resource
}

// aggregateSearchState derives the public state without treating a partial source as a total failure.
func aggregateSearchState(sources []SearchSource, catalogState string, issues []DiscoveryIssue, truncated bool, itemCount int) string {
	hasSourceFailure := false
	hasHealthySource := false
	allPermissionDenied := len(sources) > 0
	allUnavailable := len(sources) > 0
	for _, source := range sources {
		switch source.State {
		case searchStatePermissionDenied:
			hasSourceFailure = true
			allUnavailable = false
		case searchStateUnavailable:
			hasSourceFailure = true
			allPermissionDenied = false
		case searchStateError:
			hasSourceFailure = true
			allPermissionDenied = false
			allUnavailable = false
		case searchStateAvailable, searchStateEmpty:
			hasHealthySource = true
			allPermissionDenied = false
			allUnavailable = false
		default:
			allPermissionDenied = false
			allUnavailable = false
		}
	}
	if itemCount > 0 {
		if hasSourceFailure || len(issues) > 0 || truncated {
			return searchStatePartial
		}
		return searchStateAvailable
	}
	if hasHealthySource && hasSourceFailure {
		return searchStatePartial
	}
	if truncated || (len(issues) > 0 && catalogState == searchStatePartial) {
		return searchStatePartial
	}
	if allPermissionDenied {
		return searchStatePermissionDenied
	}
	if allUnavailable {
		return searchStateUnavailable
	}
	if hasSourceFailure {
		return searchStateError
	}
	if len(sources) == 0 {
		switch catalogState {
		case searchStatePermissionDenied, searchStateUnavailable, searchStateError:
			return catalogState
		}
	}
	return searchStateEmpty
}
