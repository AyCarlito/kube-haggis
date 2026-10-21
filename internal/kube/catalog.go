package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

const (
	resourceCatalogStateAvailable        = "available"
	resourceCatalogStatePartial          = "partial"
	resourceCatalogStateUnavailable      = "unavailable"
	resourceCatalogStatePermissionDenied = "permission-denied"
	resourceCatalogStateError            = "error"
)

// ResourceSubresource describes one discovered capability below a top-level resource.
type ResourceSubresource struct {
	Name  string   `json:"name"`
	Verbs []string `json:"verbs"`
}

// DiscoveryIssue describes an API group or version that could not be completely discovered.
type DiscoveryIssue struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	State     string `json:"state"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// ResourceCatalog contains every normalized top-level resource and explicit discovery health.
type ResourceCatalog struct {
	Items  []ResourceDefinition `json:"items"`
	State  string               `json:"state"`
	Issues []DiscoveryIssue     `json:"issues,omitempty"`
}

// discoverCompleteDefinitions loads all served API versions and normalizes subresources.
func discoverCompleteDefinitions(discoveryClient discovery.DiscoveryInterface) ([]ResourceDefinition, []DiscoveryIssue, error) {
	definitions, issues, _, discoveryErr := discoverCompleteDefinitionsWithPreferences(discoveryClient)
	return definitions, issues, discoveryErr
}

// discoverCompleteDefinitionsWithPreferences loads normalized discovery and the API group's preferred versions.
func discoverCompleteDefinitionsWithPreferences(discoveryClient discovery.DiscoveryInterface) ([]ResourceDefinition, []DiscoveryIssue, map[string]string, error) {
	groups, resourceLists, discoveryErr := discoveryClient.ServerGroupsAndResources()
	if len(resourceLists) == 0 && discoveryErr == nil {
		// Some test doubles and older discovery implementations only implement the preferred call.
		resourceLists, discoveryErr = discoveryClient.ServerPreferredResources()
		groups = nil
	}
	definitions := normalizeResourceLists(resourceLists)
	issues := discoveryIssues(discoveryErr)
	issues = append(issues, missingDiscoveryIssues(groups, resourceLists)...)
	return definitions, deduplicateDiscoveryIssues(issues), preferredDiscoveryVersions(groups), discoveryErr
}

// missingDiscoveryIssues identifies group versions advertised by the server without a resource list.
func missingDiscoveryIssues(groups []*metav1.APIGroup, resourceLists []*metav1.APIResourceList) []DiscoveryIssue {
	if len(groups) == 0 {
		return nil
	}
	resourceVersions := make(map[string]struct{}, len(resourceLists))
	for _, resourceList := range resourceLists {
		if resourceList != nil {
			resourceVersions[resourceList.GroupVersion] = struct{}{}
		}
	}
	issues := make([]DiscoveryIssue, 0)
	for _, group := range groups {
		if group == nil {
			continue
		}
		for _, version := range group.Versions {
			if _, found := resourceVersions[version.GroupVersion]; found {
				continue
			}
			issues = append(issues, discoveryIssueForError(group.Name, version.Version, errors.New("resource list was not returned")))
		}
	}
	return issues
}

// deduplicateDiscoveryIssues keeps partial discovery state concise when one API failure has multiple signals.
func deduplicateDiscoveryIssues(issues []DiscoveryIssue) []DiscoveryIssue {
	if len(issues) < 2 {
		return issues
	}
	seen := make(map[string]struct{}, len(issues))
	result := make([]DiscoveryIssue, 0, len(issues))
	for _, issue := range issues {
		key := issue.Group + "\x00" + issue.Version
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, issue)
	}
	return result
}

// normalizeResourceLists converts discovery rows into unique top-level GVR definitions.
func normalizeResourceLists(resourceLists []*metav1.APIResourceList) []ResourceDefinition {
	definitionsByKey := make(map[string]ResourceDefinition)
	subresourcesByKey := make(map[string]map[string]ResourceSubresource)
	for _, resourceList := range resourceLists {
		if resourceList == nil {
			continue
		}
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range resourceList.APIResources {
			name := strings.TrimSpace(resource.Name)
			if name == "" {
				continue
			}
			resourceName := name
			subresourceName := ""
			if separator := strings.IndexByte(name, '/'); separator >= 0 {
				resourceName = name[:separator]
				subresourceName = name[separator+1:]
			}
			key := definitionKey(ResourceDefinition{Group: groupVersion.Group, Version: groupVersion.Version, Resource: resourceName})
			if subresourceName != "" {
				if subresourcesByKey[key] == nil {
					subresourcesByKey[key] = make(map[string]ResourceSubresource)
				}
				subresourcesByKey[key][subresourceName] = ResourceSubresource{Name: subresourceName, Verbs: sortedUniqueStrings(resource.Verbs)}
				continue
			}

			definition := ResourceDefinition{
				Group:      groupVersion.Group,
				Version:    groupVersion.Version,
				Resource:   resourceName,
				Kind:       resource.Kind,
				Namespaced: resource.Namespaced,
				Scope:      resourceScope(resource.Namespaced),
				Verbs:      sortedUniqueStrings(resource.Verbs),
				ShortNames: sortedUniqueStrings(resource.ShortNames),
				Categories: sortedUniqueStrings(resource.Categories),
			}
			if existing, found := definitionsByKey[key]; found {
				definition = mergeResourceDefinitions(existing, definition)
			}
			definitionsByKey[key] = definition
		}
	}

	definitions := make([]ResourceDefinition, 0, len(definitionsByKey))
	for key, definition := range definitionsByKey {
		if subresources := subresourcesByKey[key]; len(subresources) > 0 {
			definition.Subresources = make([]ResourceSubresource, 0, len(subresources))
			for _, subresource := range subresources {
				definition.Subresources = append(definition.Subresources, subresource)
			}
			sort.Slice(definition.Subresources, func(left, right int) bool {
				return definition.Subresources[left].Name < definition.Subresources[right].Name
			})
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitionKey(definitions[left]) < definitionKey(definitions[right])
	})
	return definitions
}

// mergeResourceDefinitions combines duplicate discovery rows without losing metadata.
func mergeResourceDefinitions(left, right ResourceDefinition) ResourceDefinition {
	merged := left
	if merged.Kind == "" {
		merged.Kind = right.Kind
	}
	if merged.Scope == "" {
		merged.Scope = right.Scope
	}
	merged.Namespaced = right.Namespaced
	merged.CustomResource = left.CustomResource || right.CustomResource
	if merged.CustomResourceCapabilities == nil {
		merged.CustomResourceCapabilities = cloneCustomResourceCapabilities(right.CustomResourceCapabilities)
	}
	merged.Verbs = sortedUniqueStrings(append(append([]string(nil), left.Verbs...), right.Verbs...))
	merged.ShortNames = sortedUniqueStrings(append(append([]string(nil), left.ShortNames...), right.ShortNames...))
	merged.Categories = sortedUniqueStrings(append(append([]string(nil), left.Categories...), right.Categories...))
	return merged
}

// resourceScope converts Kubernetes discovery's namespaced flag into a stable catalog label.
func resourceScope(namespaced bool) string {
	if namespaced {
		return "Namespaced"
	}
	return "Cluster"
}

// sortedUniqueStrings returns deterministic, duplicate-free discovery metadata.
func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// discoveryIssues converts a partial discovery error into stable, actionable issue records.
func discoveryIssues(err error) []DiscoveryIssue {
	if err == nil {
		return nil
	}
	var groupError *discovery.ErrGroupDiscoveryFailed
	if errors.As(err, &groupError) && len(groupError.Groups) > 0 {
		groupVersions := make([]schema.GroupVersion, 0, len(groupError.Groups))
		for groupVersion := range groupError.Groups {
			groupVersions = append(groupVersions, groupVersion)
		}
		sort.Slice(groupVersions, func(left, right int) bool {
			return groupVersions[left].String() < groupVersions[right].String()
		})
		issues := make([]DiscoveryIssue, 0, len(groupVersions))
		for _, groupVersion := range groupVersions {
			issues = append(issues, discoveryIssueForError(groupVersion.Group, groupVersion.Version, groupError.Groups[groupVersion]))
		}
		return issues
	}
	return []DiscoveryIssue{discoveryIssueForError("", "", err)}
}

// discoveryIssueForError classifies one discovery failure without exposing server error text.
func discoveryIssueForError(group, version string, err error) DiscoveryIssue {
	state := resourceCatalogStateError
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		state = resourceCatalogStatePermissionDenied
	} else if apierrors.IsNotFound(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || discoveryErrorLooksUnavailable(err) {
		state = resourceCatalogStateUnavailable
	}
	coordinate := discoveryCoordinate(group, version)
	message := fmt.Sprintf("Discovery for %s failed; refresh the catalog and retry.", coordinate)
	switch state {
	case resourceCatalogStatePermissionDenied:
		message = fmt.Sprintf("Kubernetes RBAC denied discovery for %s; refresh after access changes.", coordinate)
	case resourceCatalogStateUnavailable:
		message = fmt.Sprintf("The Kubernetes API for %s is unavailable; retry discovery.", coordinate)
	}
	return DiscoveryIssue{Group: group, Version: version, State: state, Message: message, Retryable: true}
}

// discoveryErrorLooksUnavailable identifies transport failures that do not carry Kubernetes status metadata.
func discoveryErrorLooksUnavailable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection refused") || strings.Contains(message, "connection reset") || strings.Contains(message, "timeout") || strings.Contains(message, "temporarily unavailable") || strings.Contains(message, "service unavailable") || strings.Contains(message, "not returned")
}

// discoveryCoordinate formats a group-version for issue and validation messages.
func discoveryCoordinate(group, version string) string {
	if group == "" && version == "" {
		return "the Kubernetes API"
	}
	if group == "" {
		return version
	}
	return group + "/" + version
}

// resourceCatalogState derives the aggregate catalog state from definitions and issues.
func resourceCatalogState(definitions []ResourceDefinition, issues []DiscoveryIssue) string {
	if len(issues) == 0 {
		return resourceCatalogStateAvailable
	}
	if len(definitions) > 0 {
		return resourceCatalogStatePartial
	}
	allPermissionDenied := true
	allUnavailable := true
	for _, issue := range issues {
		allPermissionDenied = allPermissionDenied && issue.State == resourceCatalogStatePermissionDenied
		allUnavailable = allUnavailable && issue.State == resourceCatalogStateUnavailable
	}
	if allPermissionDenied {
		return resourceCatalogStatePermissionDenied
	}
	if allUnavailable {
		return resourceCatalogStateUnavailable
	}
	return resourceCatalogStateError
}

// resourceCatalogFromEntry returns an isolated public catalog from one cached discovery entry.
func resourceCatalogFromEntry(entry discoveryCacheEntry) ResourceCatalog {
	issues := cloneDiscoveryIssues(entry.issues)
	if len(issues) == 0 && entry.err != nil {
		issues = discoveryIssues(entry.err)
	}
	definitions := cloneResourceDefinitions(entry.definitions)
	return ResourceCatalog{Items: definitions, State: resourceCatalogState(definitions, issues), Issues: issues}
}

// cloneDiscoveryIssues returns an isolated copy of discovery issue metadata.
func cloneDiscoveryIssues(issues []DiscoveryIssue) []DiscoveryIssue {
	if len(issues) == 0 {
		return nil
	}
	return append([]DiscoveryIssue(nil), issues...)
}

// ResourceCatalog returns the complete cached catalog and its explicit health state.
func (s *Service) ResourceCatalog(ctx context.Context, contextName string) (ResourceCatalog, error) {
	dynamicClient, discoveryClient, _, resolvedContext, revision, err := s.clientsWithRevision(contextName)
	if err != nil {
		return ResourceCatalog{}, err
	}
	catalog, discoveryErr := s.cachedDiscoveryCatalog(ctx, resolvedContext, revision, dynamicClient, discoveryClient)
	if discoveryErr != nil && (errors.Is(discoveryErr, context.Canceled) || errors.Is(discoveryErr, context.DeadlineExceeded)) {
		return catalog, discoveryErr
	}
	return catalog, nil
}

// InvalidateDiscovery drops one context's cached catalog and retires its active flight.
func (s *Service) InvalidateDiscovery(contextName string) error {
	_, _, _, resolvedContext, revision, err := s.clientsWithRevision(contextName)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.discoveryGenerations == nil {
		s.discoveryGenerations = make(map[discoveryCacheKey]uint64)
	}
	key := discoveryCacheKey{contextName: resolvedContext, revision: revision}
	delete(s.discoveryCache, key)
	s.discoveryGenerations[key]++
	delete(s.discoveryFlights, key)
	s.resourceSchemaCache = make(map[resourceSchemaCacheKey]ResourceSchema)
	s.resourceSchemaCacheOrder = nil
	s.mu.Unlock()
	return nil
}

// resolveResourceDefinition validates a generic request against discovery when available.
func (s *Service) resolveResourceDefinition(ctx context.Context, contextName, group, version, resource string, namespaced bool) (ResourceDefinition, error) {
	catalog, err := s.ResourceCatalog(ctx, contextName)
	if err != nil {
		return ResourceDefinition{}, err
	}
	for _, definition := range catalog.Items {
		if definition.Group != group || definition.Version != version || definition.Resource != resource {
			continue
		}
		if definition.Namespaced != namespaced {
			return ResourceDefinition{}, fmt.Errorf("resource %s scope contradicts discovery: requested %s, discovered %s", definitionKey(definition), resourceScope(namespaced), definition.Scope)
		}
		return definition, nil
	}
	if len(catalog.Items) > 0 || catalog.State == resourceCatalogStateAvailable {
		return ResourceDefinition{}, fmt.Errorf("resource %s is not present in the discovery catalog; refresh before retrying", definitionKey(ResourceDefinition{Group: group, Version: version, Resource: resource}))
	}
	// A completely unavailable discovery endpoint cannot prove a contradiction. Preserve the
	// generic path while the catalog endpoint reports the unavailable state to the operator.
	return ResourceDefinition{Group: group, Version: version, Resource: resource, Namespaced: namespaced, Scope: resourceScope(namespaced)}, nil
}

// validateResourceNamespace rejects namespace coordinates that contradict a discovered scope.
func validateResourceNamespace(definition ResourceDefinition, namespace string, requireNamespace bool) error {
	namespace = strings.TrimSpace(namespace)
	if !definition.Namespaced {
		if namespace != "" && namespace != "all" {
			return fmt.Errorf("cluster-scoped resource %s cannot use namespace %q", definitionKey(definition), namespace)
		}
		return nil
	}
	if requireNamespace && (namespace == "" || namespace == "all") {
		return fmt.Errorf("namespace is required for namespaced resource %s", definitionKey(definition))
	}
	return nil
}
