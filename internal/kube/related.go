package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

const (
	relatedResultLimit           = 100
	relatedListLimit             = 100
	relatedNoticeLimit           = 16
	relatedStateAvailable        = "available"
	relatedStateEmpty            = "empty"
	relatedStatePartial          = "partial"
	relatedStatePermissionDenied = "permission-denied"
	relatedStateError            = "error"
	relatedStateUnavailable      = "unavailable"
	relatedItemResolved          = "resolved"
	relatedItemUnresolved        = "unresolved"
	relatedItemPermissionDenied  = "permission-denied"
	relatedItemError             = "error"
	relatedItemUnavailable       = "unavailable"
	relatedTypeOwner             = "owner"
	relatedTypeOwnedDescendant   = "owned-descendant"
	relatedTypeSelectorMatch     = "selector-match"
	relatedTypeSelectedBy        = "selected-by"
	relatedTypeBoundVolume       = "bound-volume"
	relatedTypeBoundClaim        = "bound-claim"
	relatedTypeReference         = "reference"
	relatedTypeInvolvedObject    = "involved-object"
	relatedSourceOwnerReferences = "metadata.ownerReferences"
	relatedSourceSelector        = "spec.selector"
	relatedSourceVolumeName      = "spec.volumeName"
	relatedSourceClaimRef        = "spec.claimRef"
	relatedSourceInvolvedObject  = "involvedObject"
)

// RelatedResourceList is the bounded, sanitized relationship response for one Kubernetes object.
type RelatedResourceList struct {
	State       string                      `json:"state"`
	Message     string                      `json:"message,omitempty"`
	Items       []RelatedResource           `json:"items"`
	Notices     []RelatedRelationshipNotice `json:"notices,omitempty"`
	Truncated   bool                        `json:"truncated,omitempty"`
	Diagnostics RelatedDiagnostics          `json:"diagnostics"`
}

// RelatedResource describes one resolved or explicitly unresolved relationship target.
type RelatedResource struct {
	Type               string              `json:"type"`
	Source             string              `json:"source"`
	State              string              `json:"state"`
	Definition         *ResourceDefinition `json:"definition,omitempty"`
	APIVersion         string              `json:"apiVersion,omitempty"`
	Kind               string              `json:"kind,omitempty"`
	Name               string              `json:"name,omitempty"`
	Namespace          string              `json:"namespace,omitempty"`
	UID                string              `json:"uid,omitempty"`
	Message            string              `json:"message,omitempty"`
	Controller         bool                `json:"controller,omitempty"`
	BlockOwnerDeletion bool                `json:"blockOwnerDeletion,omitempty"`
}

// RelatedRelationshipNotice describes a relationship source that could not be fully read.
type RelatedRelationshipNotice struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	State    string `json:"state"`
	Message  string `json:"message"`
	Deferred bool   `json:"deferred,omitempty"`
}

// relatedCollector accumulates safe relationship results while preserving partial failures.
type relatedCollector struct {
	ctx               context.Context
	client            dynamic.Interface
	budget            *relatedCallBudget
	definitions       []ResourceDefinition
	object            *unstructured.Unstructured
	currentDefinition ResourceDefinition
	currentGroup      string
	currentVersion    string
	currentResource   string
	currentNamespaced bool
	items             []RelatedResource
	notices           []RelatedRelationshipNotice
	itemKeys          map[string]struct{}
	noticeKeys        map[string]struct{}
	targetMu          sync.Mutex
	targetCache       map[string]relatedTargetResult
	truncated         bool
}

// relatedTargetResult stores one memoized target lookup and its safe error.
type relatedTargetResult struct {
	object *unstructured.Unstructured
	err    error
}

// relatedDescendantRule identifies one supported native controller-to-child relationship.
type relatedDescendantRule struct {
	parentGroup    string
	parentVersion  string
	parentResource string
	parentKind     string
	childGroup     string
	childVersion   string
	childResource  string
	childKind      string
}

// relatedDescendantQuery contains one native child query and its discovery and namespace availability.
type relatedDescendantQuery struct {
	definition  ResourceDefinition
	namespace   string
	namespaceOK bool
	discovered  bool
	listAllowed bool
	source      string
}

// nativeRelatedDescendantRules is the complete Phase 1 native descendant relationship set.
var nativeRelatedDescendantRules = []relatedDescendantRule{
	{parentGroup: "apps", parentVersion: "v1", parentResource: "deployments", parentKind: "Deployment", childGroup: "apps", childVersion: "v1", childResource: "replicasets", childKind: "ReplicaSet"},
	{parentGroup: "apps", parentVersion: "v1", parentResource: "replicasets", parentKind: "ReplicaSet", childGroup: "", childVersion: "v1", childResource: "pods", childKind: "Pod"},
	{parentGroup: "apps", parentVersion: "v1", parentResource: "statefulsets", parentKind: "StatefulSet", childGroup: "", childVersion: "v1", childResource: "pods", childKind: "Pod"},
	{parentGroup: "apps", parentVersion: "v1", parentResource: "daemonsets", parentKind: "DaemonSet", childGroup: "", childVersion: "v1", childResource: "pods", childKind: "Pod"},
	{parentGroup: "batch", parentVersion: "v1", parentResource: "jobs", parentKind: "Job", childGroup: "", childVersion: "v1", childResource: "pods", childKind: "Pod"},
	{parentGroup: "batch", parentVersion: "v1", parentResource: "cronjobs", parentKind: "CronJob", childGroup: "batch", childVersion: "v1", childResource: "jobs", childKind: "Job"},
}

// discoverDefinitions converts preferred discovery output into generic resource definitions.
func discoverDefinitions(discoveryClient discovery.DiscoveryInterface) ([]ResourceDefinition, error) {
	resourceLists, discoveryErr := discoveryClient.ServerPreferredResources()
	return normalizeResourceLists(resourceLists), discoveryErr
}

// RelatedResources returns read-only relationships for one discovered resource.
func (s *Service) RelatedResources(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (RelatedResourceList, error) {
	return s.relatedResources(ctx, contextName, group, version, resource, namespace, name, namespaced, true, false)
}

// RelatedReverseResources runs the deferred targeted native descendant relationship operation.
func (s *Service) RelatedReverseResources(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (RelatedResourceList, error) {
	return s.relatedResources(ctx, contextName, group, version, resource, namespace, name, namespaced, false, true)
}

// relatedResources runs one direct or deferred relationship phase under safe request limits.
func (s *Service) relatedResources(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced, includeDirect, includeReverse bool) (RelatedResourceList, error) {
	started := time.Now()
	requestContext, cancel := context.WithTimeout(ctx, relatedRequestDeadline)
	defer cancel()

	dynamicClient, discoveryClient, _, resolvedContext, revision, err := s.clientsWithRevision(contextName)
	if err != nil {
		return RelatedResourceList{}, err
	}
	definitions, discoveryErr := s.cachedDiscovery(requestContext, resolvedContext, revision, dynamicClient, discoveryClient)
	definition := relatedDefinition(definitions, group, version, resource)
	if definition.Kind != "" {
		if definition.Namespaced != namespaced {
			return RelatedResourceList{}, fmt.Errorf("resource %s scope contradicts discovery: requested %s, discovered %s", definitionKey(definition), resourceScope(namespaced), definition.Scope)
		}
		if err := validateResourceNamespace(definition, namespace, true); err != nil {
			return RelatedResourceList{}, err
		}
	}
	resourceClient, err := resourceInterface(dynamicClient, schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, namespace, namespaced, false)
	if err != nil {
		return RelatedResourceList{}, err
	}
	budget := newRelatedCallBudget(requestContext)
	var object *unstructured.Unstructured
	err = budget.call(func() error {
		var getErr error
		object, getErr = resourceClient.Get(requestContext, name, metav1.GetOptions{})
		return getErr
	})
	if err != nil {
		result := relatedRequestFailure(err, budget, started, relatedRequestDeadline)
		if discoveryErr != nil {
			result.Notices = append(result.Notices, RelatedRelationshipNotice{Type: "discovery", Source: "API discovery", State: relatedNoticeState(discoveryErr), Message: "Some API resources could not be discovered; relationship coverage may be incomplete."})
			result.State = relatedStatePartial
		}
		return result, nil
	}
	result := collectRelatedResourcesWithBudget(requestContext, dynamicClient, definitions, discoveryErr, group, version, resource, namespace, namespaced, object, budget, includeDirect, includeReverse)
	result.Diagnostics = relatedDiagnostics(budget, started, relatedRequestDeadline)
	return result, nil
}

// collectRelatedResources aggregates native metadata and spec relationships using a dynamic client.
func collectRelatedResources(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, discoveryErr error, group, version, resource, namespace string, namespaced bool, object *unstructured.Unstructured) RelatedResourceList {
	return collectRelatedResourcesWithBudget(ctx, client, definitions, discoveryErr, group, version, resource, namespace, namespaced, object, newRelatedCallBudget(ctx), true, true)
}

// collectRelatedResourcesWithBudget aggregates one relationship phase with explicit request controls.
func collectRelatedResourcesWithBudget(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, discoveryErr error, group, version, resource, namespace string, namespaced bool, object *unstructured.Unstructured, budget *relatedCallBudget, includeDirect, includeReverse bool) RelatedResourceList {
	started := time.Now()
	collector := &relatedCollector{
		ctx:               ctx,
		client:            client,
		budget:            budget,
		definitions:       append([]ResourceDefinition(nil), definitions...),
		object:            object,
		currentDefinition: relatedDefinition(definitions, group, version, resource),
		currentGroup:      group,
		currentVersion:    version,
		currentResource:   resource,
		currentNamespaced: namespaced,
		items:             make([]RelatedResource, 0),
		notices:           make([]RelatedRelationshipNotice, 0),
		itemKeys:          make(map[string]struct{}),
		noticeKeys:        make(map[string]struct{}),
		targetCache:       make(map[string]relatedTargetResult),
	}
	if collector.currentDefinition.Kind == "" {
		collector.currentDefinition.Kind = object.GetKind()
	}
	if discoveryErr != nil {
		collector.addNotice("discovery", "API discovery", relatedNoticeState(discoveryErr), "Some API resources could not be discovered; relationship coverage may be incomplete.")
	}
	if includeDirect {
		collector.addOwnerRelationships()
		if collector.isEventResource() {
			collector.addInvolvedObjectRelationship()
		} else {
			collector.addSelectorRelationships()
			collector.addStorageRelationships()
			collector.addNativeReferences()
		}
	}
	if includeReverse {
		if !collector.isEventResource() {
			collector.addOwnedDescendants()
		}
	} else if includeDirect && object.GetUID() != "" {
		collector.addDeferredOwnedDescendantNotices()
	}
	result := collector.result()
	result.Diagnostics = relatedDiagnostics(budget, started, 0)
	return result
}

// definitionKey returns a stable sort and lookup key for a resource definition.
func definitionKey(definition ResourceDefinition) string {
	return definition.Group + "/" + definition.Version + "/" + definition.Resource
}

// relatedDefinition finds one exact discovered group-version-resource definition.
func relatedDefinition(definitions []ResourceDefinition, group, version, resource string) ResourceDefinition {
	for _, definition := range definitions {
		if definition.Group == group && definition.Version == version && definition.Resource == resource {
			return definition
		}
	}
	return ResourceDefinition{Group: group, Version: version, Resource: resource}
}

// relationshipDefinition returns a discovered definition or a scope-safe native fallback.
func relationshipDefinition(definitions []ResourceDefinition, group, version, resource string) (ResourceDefinition, bool) {
	definition := relatedDefinition(definitions, group, version, resource)
	if definition.Kind != "" {
		return definition, true
	}
	fallback, found := nativeRelationshipDefinition(group, version, resource)
	if !found {
		return definition, false
	}
	return fallback, false
}

// nativeRelationshipDefinition describes the built-in resources used by safe relationship rules.
func nativeRelationshipDefinition(group, version, resource string) (ResourceDefinition, bool) {
	kind := ""
	namespaced := false
	switch {
	case group == "" && version == "v1":
		switch resource {
		case "pods":
			kind, namespaced = "Pod", true
		case "services":
			kind, namespaced = "Service", true
		case "configmaps":
			kind, namespaced = "ConfigMap", true
		case "secrets":
			kind, namespaced = "Secret", true
		case "serviceaccounts":
			kind, namespaced = "ServiceAccount", true
		case "persistentvolumeclaims":
			kind, namespaced = "PersistentVolumeClaim", true
		case "persistentvolumes":
			kind = "PersistentVolume"
		}
	case group == "apps" && version == "v1":
		switch resource {
		case "deployments":
			kind, namespaced = "Deployment", true
		case "replicasets":
			kind, namespaced = "ReplicaSet", true
		case "statefulsets":
			kind, namespaced = "StatefulSet", true
		case "daemonsets":
			kind, namespaced = "DaemonSet", true
		}
	case group == "batch" && version == "v1":
		switch resource {
		case "jobs":
			kind, namespaced = "Job", true
		case "cronjobs":
			kind, namespaced = "CronJob", true
		}
	}
	if kind == "" {
		return ResourceDefinition{}, false
	}
	return ResourceDefinition{Group: group, Version: version, Resource: resource, Kind: kind, Namespaced: namespaced}, true
}

// relatedDefinitionsForKind finds all discovered definitions for one exact API version and kind.
func relatedDefinitionsForKind(definitions []ResourceDefinition, apiVersion, kind string) []ResourceDefinition {
	group, version := splitAPIVersion(apiVersion)
	matches := make([]ResourceDefinition, 0, 1)
	for _, definition := range definitions {
		if definition.Group == group && definition.Version == version && definition.Kind == kind {
			matches = append(matches, definition)
		}
	}
	return matches
}

// relatedDefinitionForKind finds one unambiguous discovered definition from an owner API version and kind.
func relatedDefinitionForKind(definitions []ResourceDefinition, apiVersion, kind string) (ResourceDefinition, bool) {
	matches := relatedDefinitionsForKind(definitions, apiVersion, kind)
	if len(matches) != 1 {
		return ResourceDefinition{}, false
	}
	return matches[0], true
}

// splitAPIVersion separates a core or grouped Kubernetes API version.
func splitAPIVersion(apiVersion string) (string, string) {
	separator := strings.IndexByte(apiVersion, '/')
	if separator < 0 {
		return "", apiVersion
	}
	return apiVersion[:separator], apiVersion[separator+1:]
}

// addOwnerRelationships adds verified direct metadata.ownerReferences targets.
func (collector *relatedCollector) addOwnerRelationships() {
	for _, owner := range collector.object.GetOwnerReferences() {
		definition, found := relatedDefinitionForKind(collector.definitions, owner.APIVersion, owner.Kind)
		item := RelatedResource{
			Type:               relatedTypeOwner,
			Source:             relatedSourceOwnerReferences,
			State:              relatedItemUnresolved,
			APIVersion:         owner.APIVersion,
			Kind:               owner.Kind,
			Name:               owner.Name,
			UID:                string(owner.UID),
			Controller:         owner.Controller != nil && *owner.Controller,
			BlockOwnerDeletion: owner.BlockOwnerDeletion != nil && *owner.BlockOwnerDeletion,
		}
		if found {
			item.Definition = &definition
			item.Namespace = collector.targetNamespace(definition.Namespaced)
			if item.Namespace == "" && definition.Namespaced {
				item.Message = "The owner is namespaced, but this object has no namespace to resolve it safely."
				collector.add(item)
				continue
			}
			target, err := collector.getTarget(definition, item.Namespace, owner.Name)
			if err == nil {
				if owner.UID != "" && target.GetUID() != owner.UID {
					item.Message = "The referenced owner name exists, but its UID does not match metadata.ownerReferences."
					collector.add(item)
					continue
				}
				item.State = relatedItemResolved
				item.UID = string(target.GetUID())
				item.Kind = target.GetKind()
				if item.Kind == "" {
					item.Kind = definition.Kind
				}
				collector.add(item)
				continue
			}
			item.State, item.Message = relatedItemErrorState(err)
			collector.add(item)
			continue
		}
		item.Message = "The owner API version and kind were not present in discovery, so navigation is unavailable."
		collector.add(item)
	}
}

// addInvolvedObjectRelationship resolves the exact target named by a core Event's involvedObject.
func (collector *relatedCollector) addInvolvedObjectRelationship() {
	item := RelatedResource{
		Type:   relatedTypeInvolvedObject,
		Source: relatedSourceInvolvedObject,
		State:  relatedItemUnresolved,
	}
	involvedObject, found, err := unstructured.NestedMap(collector.object.Object, "involvedObject")
	if err != nil {
		item.Message = "Event involvedObject is malformed, so its target cannot be resolved safely."
		collector.add(item)
		return
	}
	if !found {
		return
	}

	item.APIVersion, _, err = relatedReferenceString(involvedObject, "apiVersion")
	if err != nil {
		item.Message = "Event involvedObject.apiVersion is malformed, so its target cannot be resolved safely."
		collector.add(item)
		return
	}
	item.Kind, _, err = relatedReferenceString(involvedObject, "kind")
	if err != nil {
		item.Message = "Event involvedObject.kind is malformed, so its target cannot be resolved safely."
		collector.add(item)
		return
	}
	item.Name, _, err = relatedReferenceString(involvedObject, "name")
	if err != nil {
		item.Message = "Event involvedObject.name is malformed, so its target cannot be resolved safely."
		collector.add(item)
		return
	}
	item.Namespace, _, err = relatedReferenceString(involvedObject, "namespace")
	if err != nil {
		item.Message = "Event involvedObject.namespace is malformed, so its target cannot be resolved safely."
		collector.add(item)
		return
	}
	item.UID, _, err = relatedReferenceString(involvedObject, "uid")
	if err != nil {
		item.Message = "Event involvedObject.uid is malformed, so its target cannot be resolved safely."
		collector.add(item)
		return
	}
	if item.APIVersion == "" || item.Kind == "" || item.Name == "" {
		item.Message = "Event involvedObject must include apiVersion, kind, and name before a safe target lookup."
		collector.add(item)
		return
	}

	definitions := relatedDefinitionsForKind(collector.definitions, item.APIVersion, item.Kind)
	if len(definitions) == 0 {
		item.Message = "The Event target API version and kind were not present in discovery, so navigation is unavailable."
		collector.add(item)
		return
	}
	if len(definitions) > 1 {
		item.Message = "The Event target API version and kind matched multiple discovered resources, so navigation is unavailable."
		collector.add(item)
		return
	}
	definition := definitions[0]
	item.Definition = relatedDefinitionPointer(definition)
	targetNamespace := item.Namespace
	if definition.Namespaced {
		if targetNamespace == "" {
			item.Message = "The Event target is namespaced, but involvedObject.namespace is missing."
			collector.add(item)
			return
		}
	} else if targetNamespace != "" {
		item.Message = "The Event target is cluster-scoped, but involvedObject.namespace is not empty."
		collector.add(item)
		return
	}
	target, err := collector.getTarget(definition, targetNamespace, item.Name)
	if err != nil {
		item.State, item.Message = relatedItemErrorState(err)
		collector.add(item)
		return
	}
	if target.GetName() != item.Name {
		item.Message = "The Event target response name did not match involvedObject.name."
		collector.add(item)
		return
	}
	if targetKind := target.GetKind(); targetKind != "" && targetKind != item.Kind {
		item.Message = "The Event target response kind did not match involvedObject.kind."
		collector.add(item)
		return
	}
	if targetAPIVersion := target.GetAPIVersion(); targetAPIVersion != "" && targetAPIVersion != item.APIVersion {
		item.Message = "The Event target response API version did not match involvedObject.apiVersion."
		collector.add(item)
		return
	}
	if item.UID != "" && target.GetUID() != types.UID(item.UID) {
		item.Message = "The Event target name exists, but its UID does not match involvedObject.uid."
		collector.add(item)
		return
	}
	if definition.Namespaced && target.GetNamespace() != "" && target.GetNamespace() != targetNamespace {
		item.Message = "The Event target response namespace did not match involvedObject.namespace."
		collector.add(item)
		return
	}
	item.State = relatedItemResolved
	item.UID = string(target.GetUID())
	item.Kind = relatedObjectKind(target, item.Kind)
	collector.add(item)
}

// relatedReferenceString reads one optional string identity field from an Event ObjectReference.
func relatedReferenceString(object map[string]interface{}, field string) (string, bool, error) {
	value, found, err := unstructured.NestedString(object, field)
	return strings.TrimSpace(value), found, err
}

// isEventResource identifies the exact core/v1 Event resource handled by the involvedObject rule.
func (collector *relatedCollector) isEventResource() bool {
	return collector.currentGroup == "" && collector.currentVersion == "v1" && collector.currentResource == "events" && collector.currentKind() == "Event"
}

// ownedDescendantQueryPlan builds only the explicit native child queries supported for the current resource.
func (collector *relatedCollector) ownedDescendantQueryPlan() []relatedDescendantQuery {
	uid := strings.TrimSpace(string(collector.object.GetUID()))
	if uid == "" {
		return nil
	}
	queries := make([]relatedDescendantQuery, 0, 1)
	for _, rule := range nativeRelatedDescendantRules {
		if collector.currentGroup != rule.parentGroup || collector.currentVersion != rule.parentVersion || collector.currentResource != rule.parentResource || collector.currentKind() != rule.parentKind {
			continue
		}
		definition := relatedDefinition(collector.definitions, rule.childGroup, rule.childVersion, rule.childResource)
		discovered := definition.Kind == rule.childKind
		if !discovered {
			fallback, fallbackFound := nativeRelationshipDefinition(rule.childGroup, rule.childVersion, rule.childResource)
			if !fallbackFound {
				continue
			}
			definition = fallback
		}
		namespace, namespaceOK := collector.descendantNamespace(definition.Namespaced)
		queries = append(queries, relatedDescendantQuery{
			definition:  definition,
			namespace:   namespace,
			namespaceOK: namespaceOK,
			discovered:  discovered,
			listAllowed: discovered && definitionAllowsList(definition),
			source:      relatedDescendantSource(definition),
		})
	}
	return queries
}

// relatedDescendantSource describes one exact child GVR in relationship notices.
func relatedDescendantSource(definition ResourceDefinition) string {
	return relatedSourceOwnerReferences + " -> " + apiVersionForDefinition(definition) + "/" + definition.Resource
}

// descendantNamespace determines whether a child list can be safely scoped to the current object's namespace.
func (collector *relatedCollector) descendantNamespace(namespaced bool) (string, bool) {
	if !namespaced {
		return "", false
	}
	if !collector.currentNamespaced {
		return "", false
	}
	namespace := strings.TrimSpace(collector.object.GetNamespace())
	return namespace, namespace != ""
}

// addDeferredOwnedDescendantNotices advertises only supported native child queries for the optional phase.
func (collector *relatedCollector) addDeferredOwnedDescendantNotices() {
	for _, query := range collector.ownedDescendantQueryPlan() {
		if !query.discovered {
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedItemUnavailable, fmt.Sprintf("The supported %s child API was not present in discovery.", query.definition.Kind))
			continue
		}
		if !query.listAllowed {
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedItemUnavailable, fmt.Sprintf("Discovery did not advertise list access for supported %s descendants.", query.definition.Kind))
			continue
		}
		if !query.namespaceOK {
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedItemUnavailable, fmt.Sprintf("The current namespace is not safe for targeted %s descendants.", query.definition.Kind))
			continue
		}
		collector.truncated = true
		collector.addDeferredNotice(relatedTypeOwnedDescendant, query.source, relatedStatePartial, fmt.Sprintf("Targeted %s descendants are available from the optional relationship operation.", query.definition.Kind))
	}
}

// addOwnedDescendants lists only supported native children and filters each returned object by the exact owner UID.
func (collector *relatedCollector) addOwnedDescendants() {
	queries := collector.ownedDescendantQueryPlan()
	uid := types.UID(strings.TrimSpace(string(collector.object.GetUID())))
	for _, query := range queries {
		if !query.discovered {
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedItemUnavailable, fmt.Sprintf("The supported %s child API was not present in discovery.", query.definition.Kind))
			continue
		}
		if !query.listAllowed {
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedItemUnavailable, fmt.Sprintf("Discovery did not advertise list access for supported %s descendants.", query.definition.Kind))
			continue
		}
		if !query.namespaceOK {
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedItemUnavailable, fmt.Sprintf("The current object has no safe namespace for targeted %s descendants.", query.definition.Kind))
			continue
		}
		items, err := collector.list(query.definition, query.namespace, metav1.ListOptions{Limit: relatedListLimit})
		if err != nil {
			collector.addRelationshipListNotice(relatedTypeOwnedDescendant, query.source, err, fmt.Sprintf("Could not read targeted %s descendants.", query.definition.Kind))
			continue
		}
		if items == nil {
			continue
		}
		for index := range items.Items {
			item := &items.Items[index]
			for _, owner := range item.GetOwnerReferences() {
				if owner.UID != uid {
					continue
				}
				namespace := item.GetNamespace()
				if query.definition.Namespaced && namespace == "" {
					namespace = query.namespace
				}
				apiVersion := item.GetAPIVersion()
				if apiVersion == "" {
					apiVersion = apiVersionForDefinition(query.definition)
				}
				collector.add(RelatedResource{
					Type:               relatedTypeOwnedDescendant,
					Source:             relatedSourceOwnerReferences,
					State:              relatedItemResolved,
					Definition:         relatedDefinitionPointer(query.definition),
					APIVersion:         apiVersion,
					Kind:               relatedObjectKind(item, query.definition.Kind),
					Name:               item.GetName(),
					Namespace:          namespace,
					UID:                string(item.GetUID()),
					Controller:         owner.Controller != nil && *owner.Controller,
					BlockOwnerDeletion: owner.BlockOwnerDeletion != nil && *owner.BlockOwnerDeletion,
				})
				break
			}
		}
		if items.GetContinue() != "" {
			collector.truncated = true
			collector.addNotice(relatedTypeOwnedDescendant, query.source, relatedStatePartial, fmt.Sprintf("Targeted %s descendants are capped by the Kubernetes list limit.", query.definition.Kind))
		}
	}
}

// addRelationshipListNotice classifies bounded list failures without exposing backend error text.
func (collector *relatedCollector) addRelationshipListNotice(relationshipType, source string, err error, fallback string) {
	switch {
	case errors.Is(err, errRelatedCallBudget):
		collector.markTruncated("The relationship query reached its Kubernetes-call budget; remaining sources were not queried.")
	case errors.Is(err, context.DeadlineExceeded):
		collector.markTruncated("The relationship query deadline expired; remaining sources were not queried.")
	case errors.Is(err, context.Canceled):
		collector.markTruncated("The relationship query was canceled before all sources could be read.")
	default:
		collector.addNotice(relationshipType, source, relatedNoticeState(err), fallback)
	}
}

// definitionAllowsList uses advertised discovery verbs when they are available.
func definitionAllowsList(definition ResourceDefinition) bool {
	if len(definition.Verbs) == 0 {
		return true
	}
	for _, verb := range definition.Verbs {
		if verb == "list" {
			return true
		}
	}
	return false
}

// addSelectorRelationships handles Service and workload selectors in both directions.
func (collector *relatedCollector) addSelectorRelationships() {
	kind := collector.currentKind()
	switch {
	case collector.currentGroup == "" && collector.currentVersion == "v1" && kind == "Service":
		collector.addServicePods()
	case collector.currentGroup == "" && collector.currentVersion == "v1" && kind == "Pod":
		collector.addPodServices()
	case isSelectorWorkload(collector.currentGroup, collector.currentVersion, kind):
		collector.addWorkloadPods()
	}
}

// addServicePods lists Pods selected by a Service's exact spec.selector.
func (collector *relatedCollector) addServicePods() {
	selector, found, err := nestedServiceSelector(collector.object.Object)
	if err != nil {
		collector.addNotice(relatedTypeSelectorMatch, relatedSourceSelector, relatedItemError, "Service spec.selector is malformed and cannot be evaluated safely.")
		return
	}
	if !found || len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
		collector.addNotice(relatedTypeSelectorMatch, relatedSourceSelector, relatedItemUnavailable, "This Service has no non-empty spec.selector; its endpoints cannot be inferred from labels.")
		return
	}
	collector.addPodsForSelector(relatedTypeSelectorMatch, relatedSourceSelector, selector)
}

// addWorkloadPods lists Pods matching an explicit common workload selector.
func (collector *relatedCollector) addWorkloadPods() {
	selector, found, err := nestedLabelSelector(collector.object.Object, "spec", "selector")
	if err != nil {
		collector.addNotice(relatedTypeSelectorMatch, relatedSourceSelector, relatedItemError, "Workload spec.selector is malformed and cannot be evaluated safely.")
		return
	}
	if !found || len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
		return
	}
	collector.addPodsForSelector(relatedTypeSelectorMatch, relatedSourceSelector, selector)
}

// addPodsForSelector lists namespaced Pods for one native LabelSelector.
func (collector *relatedCollector) addPodsForSelector(relationshipType, source string, selector metav1.LabelSelector) {
	if !collector.currentNamespaced || collector.object.GetNamespace() == "" {
		collector.addNotice(relationshipType, source, relatedItemUnavailable, "Pod selector matching requires the current resource namespace.")
		return
	}
	parsed, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		collector.addNotice(relationshipType, source, relatedItemError, "The native label selector is invalid and cannot be evaluated safely.")
		return
	}
	podDefinition, found := relationshipDefinition(collector.definitions, "", "v1", "pods")
	items, err := collector.list(podDefinition, collector.object.GetNamespace(), metav1.ListOptions{Limit: relatedListLimit, LabelSelector: parsed.String()})
	if err != nil {
		collector.addRelationshipListNotice(relationshipType, source, err, "Kubernetes denied or failed the matching Pod list.")
		return
	}
	for index := range items.Items {
		item := &items.Items[index]
		relation := RelatedResource{
			Type:       relationshipType,
			Source:     source,
			State:      relatedItemResolved,
			Definition: relatedDefinitionPointer(podDefinition),
			APIVersion: item.GetAPIVersion(),
			Kind:       relatedObjectKind(item, "Pod"),
			Name:       item.GetName(),
			Namespace:  item.GetNamespace(),
			UID:        string(item.GetUID()),
		}
		if !found {
			relation.State = relatedItemUnresolved
			relation.Message = "Pods were returned, but the Pod resource was not present in discovery for navigation."
		}
		collector.add(relation)
	}
	if items.GetContinue() != "" {
		collector.markTruncated("Pod selector results are capped by the Kubernetes list limit.")
	}
}

// addPodServices lists Services whose explicit selectors match the current Pod labels.
func (collector *relatedCollector) addPodServices() {
	if !collector.currentNamespaced || collector.object.GetNamespace() == "" {
		collector.addNotice(relatedTypeSelectedBy, relatedSourceSelector, relatedItemUnavailable, "Service selector matching requires the Pod namespace.")
		return
	}
	podLabels := labels.Set(collector.object.GetLabels())
	serviceDefinition, serviceFound := relationshipDefinition(collector.definitions, "", "v1", "services")
	items, err := collector.list(serviceDefinition, collector.object.GetNamespace(), metav1.ListOptions{Limit: relatedListLimit})
	if err != nil {
		collector.addRelationshipListNotice(relatedTypeSelectedBy, relatedSourceSelector, err, "Kubernetes denied or failed the matching Service list.")
		return
	}
	for index := range items.Items {
		item := &items.Items[index]
		selector, found, selectorErr := nestedServiceSelector(item.Object)
		if selectorErr != nil {
			collector.addNotice(relatedTypeSelectedBy, relatedSourceSelector, relatedItemError, "A Service selector was malformed and was skipped safely.")
			continue
		}
		if !found {
			continue
		}
		parsed, selectorErr := metav1.LabelSelectorAsSelector(&selector)
		if selectorErr != nil {
			collector.addNotice(relatedTypeSelectedBy, relatedSourceSelector, relatedItemError, "A Service selector was invalid and was skipped safely.")
			continue
		}
		if !parsed.Matches(podLabels) {
			continue
		}
		relation := RelatedResource{
			Type:       relatedTypeSelectedBy,
			Source:     relatedSourceSelector,
			State:      relatedItemResolved,
			Definition: relatedDefinitionPointer(serviceDefinition),
			APIVersion: item.GetAPIVersion(),
			Kind:       relatedObjectKind(item, "Service"),
			Name:       item.GetName(),
			Namespace:  item.GetNamespace(),
			UID:        string(item.GetUID()),
		}
		if !serviceFound {
			relation.State = relatedItemUnresolved
			relation.Message = "Services were returned, but the Service resource was not present in discovery for navigation."
		}
		collector.add(relation)
	}
	if items.GetContinue() != "" {
		collector.markTruncated("Service selector results are capped by the Kubernetes list limit.")
	}
}

// isSelectorWorkload identifies common Kubernetes workload kinds with native selectors.
func isSelectorWorkload(group, version, kind string) bool {
	if version != "v1" {
		return false
	}
	if group != "apps" && group != "batch" {
		return false
	}
	switch kind {
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job":
		return true
	default:
		return false
	}
}

// addStorageRelationships resolves native PersistentVolume and PersistentVolumeClaim bindings.
func (collector *relatedCollector) addStorageRelationships() {
	kind := collector.currentKind()
	if collector.currentGroup != "" || collector.currentVersion != "v1" {
		return
	}
	switch kind {
	case "PersistentVolumeClaim":
		collector.addClaimVolume()
	case "PersistentVolume":
		collector.addVolumeClaim()
	}
}

// addClaimVolume resolves a PVC spec.volumeName to its bound PersistentVolume.
func (collector *relatedCollector) addClaimVolume() {
	volumeName, found, err := unstructured.NestedString(collector.object.Object, "spec", "volumeName")
	if err != nil {
		collector.addNotice(relatedTypeBoundVolume, relatedSourceVolumeName, relatedItemError, "PersistentVolumeClaim spec.volumeName is malformed.")
		return
	}
	if !found || volumeName == "" {
		return
	}
	definition, _ := relationshipDefinition(collector.definitions, "", "v1", "persistentvolumes")
	collector.addNamedTarget(relatedTypeBoundVolume, relatedSourceVolumeName, definition, "", volumeName, "PersistentVolume")
}

// addVolumeClaim resolves a PV spec.claimRef to its bound PersistentVolumeClaim.
func (collector *relatedCollector) addVolumeClaim() {
	claim, found, err := unstructured.NestedMap(collector.object.Object, "spec", "claimRef")
	if err != nil {
		collector.addNotice(relatedTypeBoundClaim, relatedSourceClaimRef, relatedItemError, "PersistentVolume spec.claimRef is malformed.")
		return
	}
	if !found {
		return
	}
	name, _, nameErr := unstructured.NestedString(claim, "name")
	namespace, _, namespaceErr := unstructured.NestedString(claim, "namespace")
	uid, _, uidErr := unstructured.NestedString(claim, "uid")
	if nameErr != nil || namespaceErr != nil || uidErr != nil || name == "" || namespace == "" {
		collector.addNotice(relatedTypeBoundClaim, relatedSourceClaimRef, relatedItemUnresolved, "PersistentVolume claimRef does not contain a complete claim name and namespace.")
		return
	}
	definition, _ := relationshipDefinition(collector.definitions, "", "v1", "persistentvolumeclaims")
	collector.addNamedTargetWithUID(relatedTypeBoundClaim, relatedSourceClaimRef, definition, namespace, name, uid, "PersistentVolumeClaim")
}

// addNamedTarget resolves one safe native name reference and records access failures explicitly.
func (collector *relatedCollector) addNamedTarget(relationshipType, source string, definition ResourceDefinition, namespace, name, kind string) {
	collector.addNamedTargetWithUID(relationshipType, source, definition, namespace, name, "", kind)
}

// addNamedTargetWithUID resolves one native name reference and optionally verifies its UID.
func (collector *relatedCollector) addNamedTargetWithUID(relationshipType, source string, definition ResourceDefinition, namespace, name, expectedUID, kind string) {
	item := RelatedResource{
		Type:       relationshipType,
		Source:     source,
		State:      relatedItemUnresolved,
		Definition: relatedDefinitionPointer(definition),
		APIVersion: apiVersionForDefinition(definition),
		Kind:       kind,
		Name:       name,
		Namespace:  namespace,
	}
	if definition.Resource == "" || definition.Kind == "" {
		item.Message = "The referenced resource kind was not present in discovery, so navigation is unavailable."
		collector.add(item)
		return
	}
	if discovered := relatedDefinition(collector.definitions, definition.Group, definition.Version, definition.Resource); discovered.Kind == "" {
		item.Message = "The referenced resource kind was not present in discovery, so navigation is unavailable."
		collector.add(item)
		return
	}
	target, err := collector.getTarget(definition, namespace, name)
	if err != nil {
		item.State, item.Message = relatedItemErrorState(err)
		collector.add(item)
		return
	}
	if expectedUID != "" && target.GetUID() != types.UID(expectedUID) {
		item.Message = "The referenced object exists, but its UID does not match the native binding reference."
		collector.add(item)
		return
	}
	item.State = relatedItemResolved
	item.UID = string(target.GetUID())
	item.Kind = relatedObjectKind(target, kind)
	collector.add(item)
}

// addNativeReferences resolves explicit PodSpec references without reading referenced object bodies.
func (collector *relatedCollector) addNativeReferences() {
	for _, root := range collector.podSpecRoots() {
		if serviceAccount, found, err := unstructured.NestedString(root.spec, "serviceAccountName"); err != nil {
			collector.addNotice(relatedTypeReference, root.source+".serviceAccountName", relatedItemError, "The native serviceAccountName field is malformed.")
		} else if found && serviceAccount != "" {
			definition, _ := relationshipDefinition(collector.definitions, "", "v1", "serviceaccounts")
			collector.addNamedTarget(relatedTypeReference, root.source+".serviceAccountName", definition, collector.object.GetNamespace(), serviceAccount, "ServiceAccount")
		}

		collector.addPodSpecNamedReferences(root)
	}
}

// relatedPodSpec identifies one native PodSpec and its source path.
type relatedPodSpec struct {
	spec   map[string]interface{}
	source string
}

// podSpecRoots returns only well-known native PodSpec locations for common resource kinds.
func (collector *relatedCollector) podSpecRoots() []relatedPodSpec {
	var roots []relatedPodSpec
	add := func(path string, fields ...string) {
		spec, found, err := unstructured.NestedMap(collector.object.Object, fields...)
		if err == nil && found {
			roots = append(roots, relatedPodSpec{spec: spec, source: path})
		}
	}
	switch {
	case collector.currentGroup == "" && collector.currentVersion == "v1" && collector.currentKind() == "Pod":
		add("spec", "spec")
	case collector.currentGroup == "apps" && collector.currentVersion == "v1" && isNativeWorkloadKind(collector.currentKind()):
		add("spec.template.spec", "spec", "template", "spec")
	case collector.currentGroup == "batch" && collector.currentVersion == "v1" && collector.currentKind() == "Job":
		add("spec.template.spec", "spec", "template", "spec")
	case collector.currentGroup == "batch" && collector.currentVersion == "v1" && collector.currentKind() == "CronJob":
		add("spec.jobTemplate.spec.template.spec", "spec", "jobTemplate", "spec", "template", "spec")
	}
	return roots
}

// isNativeWorkloadKind identifies apps/v1 resources with a native PodTemplateSpec.
func isNativeWorkloadKind(kind string) bool {
	switch kind {
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet":
		return true
	default:
		return false
	}
}

// addPodSpecNamedReferences extracts exact ConfigMap, Secret, PVC, and image-pull references.
func (collector *relatedCollector) addPodSpecNamedReferences(root relatedPodSpec) {
	definitionFor := func(resource, kind string) ResourceDefinition {
		definition, _ := relationshipDefinition(collector.definitions, "", "v1", resource)
		return definition
	}
	if imagePullSecrets, found, err := unstructured.NestedSlice(root.spec, "imagePullSecrets"); err == nil && found {
		for _, raw := range imagePullSecrets {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(entry, "name")
			if name != "" {
				collector.addNamedTarget(relatedTypeReference, root.source+".imagePullSecrets[].name", definitionFor("secrets", "Secret"), collector.object.GetNamespace(), name, "Secret")
			}
		}
	}
	if volumes, found, err := unstructured.NestedSlice(root.spec, "volumes"); err == nil && found {
		for _, raw := range volumes {
			volume, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if claim, claimFound, _ := unstructured.NestedMap(volume, "persistentVolumeClaim"); claimFound {
				name, _, _ := unstructured.NestedString(claim, "claimName")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, root.source+".volumes[].persistentVolumeClaim.claimName", definitionFor("persistentvolumeclaims", "PersistentVolumeClaim"), collector.object.GetNamespace(), name, "PersistentVolumeClaim")
				}
			}
			if configMap, configMapFound, _ := unstructured.NestedMap(volume, "configMap"); configMapFound {
				name, _, _ := unstructured.NestedString(configMap, "name")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, root.source+".volumes[].configMap.name", definitionFor("configmaps", "ConfigMap"), collector.object.GetNamespace(), name, "ConfigMap")
				}
			}
			if secret, secretFound, _ := unstructured.NestedMap(volume, "secret"); secretFound {
				name, _, _ := unstructured.NestedString(secret, "secretName")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, root.source+".volumes[].secret.secretName", definitionFor("secrets", "Secret"), collector.object.GetNamespace(), name, "Secret")
				}
			}
		}
	}
	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		entries, found, err := unstructured.NestedSlice(root.spec, field)
		if err != nil || !found {
			continue
		}
		for _, raw := range entries {
			container, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			collector.addContainerReferences(root.source+"."+field+"[]", container, definitionFor)
		}
	}
}

// addContainerReferences extracts native env and envFrom ConfigMap and Secret references.
func (collector *relatedCollector) addContainerReferences(source string, container map[string]interface{}, definitionFor func(string, string) ResourceDefinition) {
	if envFrom, found, err := unstructured.NestedSlice(container, "envFrom"); err == nil && found {
		for _, raw := range envFrom {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if ref, refFound, _ := unstructured.NestedMap(entry, "configMapRef"); refFound {
				name, _, _ := unstructured.NestedString(ref, "name")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, source+".envFrom[].configMapRef.name", definitionFor("configmaps", "ConfigMap"), collector.object.GetNamespace(), name, "ConfigMap")
				}
			}
			if ref, refFound, _ := unstructured.NestedMap(entry, "secretRef"); refFound {
				name, _, _ := unstructured.NestedString(ref, "name")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, source+".envFrom[].secretRef.name", definitionFor("secrets", "Secret"), collector.object.GetNamespace(), name, "Secret")
				}
			}
		}
	}
	if env, found, err := unstructured.NestedSlice(container, "env"); err == nil && found {
		for _, raw := range env {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			valueFrom, valueFound, _ := unstructured.NestedMap(entry, "valueFrom")
			if !valueFound {
				continue
			}
			if ref, refFound, _ := unstructured.NestedMap(valueFrom, "configMapKeyRef"); refFound {
				name, _, _ := unstructured.NestedString(ref, "name")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, source+".env[].valueFrom.configMapKeyRef.name", definitionFor("configmaps", "ConfigMap"), collector.object.GetNamespace(), name, "ConfigMap")
				}
			}
			if ref, refFound, _ := unstructured.NestedMap(valueFrom, "secretKeyRef"); refFound {
				name, _, _ := unstructured.NestedString(ref, "name")
				if name != "" {
					collector.addNamedTarget(relatedTypeReference, source+".env[].valueFrom.secretKeyRef.name", definitionFor("secrets", "Secret"), collector.object.GetNamespace(), name, "Secret")
				}
			}
		}
	}
}

// nestedLabelSelector parses a native LabelSelector from unstructured data.
func nestedLabelSelector(object map[string]interface{}, fields ...string) (metav1.LabelSelector, bool, error) {
	value, found, err := unstructured.NestedMap(object, fields...)
	if err != nil || !found {
		return metav1.LabelSelector{}, found, err
	}
	selector := metav1.LabelSelector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(value, &selector); err != nil {
		return metav1.LabelSelector{}, true, err
	}
	return selector, true, nil
}

// nestedServiceSelector parses the map[string]string selector used by native Services.
func nestedServiceSelector(object map[string]interface{}) (metav1.LabelSelector, bool, error) {
	values, found, err := unstructured.NestedStringMap(object, "spec", "selector")
	if err != nil || !found {
		return metav1.LabelSelector{}, found, err
	}
	return metav1.LabelSelector{MatchLabels: values}, true, nil
}

// currentKind returns the object's kind with the requested discovery kind as a fallback.
func (collector *relatedCollector) currentKind() string {
	if kind := collector.object.GetKind(); kind != "" {
		return kind
	}
	return collector.currentDefinition.Kind
}

// targetNamespace returns the current namespace only for namespaced target resources.
func (collector *relatedCollector) targetNamespace(namespaced bool) string {
	if !namespaced {
		return ""
	}
	return collector.object.GetNamespace()
}

// getTarget reads one target object without exposing its body to the relationship response.
func (collector *relatedCollector) getTarget(definition ResourceDefinition, namespace, name string) (*unstructured.Unstructured, error) {
	key := strings.Join([]string{definitionKey(definition), namespace, name}, "\x00")
	collector.targetMu.Lock()
	if result, found := collector.targetCache[key]; found {
		collector.targetMu.Unlock()
		return result.object, result.err
	}
	collector.targetMu.Unlock()

	resourceClient, err := resourceInterface(collector.client, schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}, namespace, definition.Namespaced, false)
	if err != nil {
		return nil, err
	}
	var object *unstructured.Unstructured
	err = collector.budget.call(func() error {
		var getErr error
		object, getErr = resourceClient.Get(collector.ctx, name, metav1.GetOptions{})
		return getErr
	})
	collector.targetMu.Lock()
	collector.targetCache[key] = relatedTargetResult{object: object, err: err}
	collector.targetMu.Unlock()
	return object, err
}

// list lists one bounded namespaced or cluster-scoped relationship collection.
func (collector *relatedCollector) list(definition ResourceDefinition, namespace string, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	var items *unstructured.UnstructuredList
	err := collector.budget.call(func() error {
		var listErr error
		items, listErr = listResource(collector.ctx, collector.client, schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}, namespace, definition.Namespaced, options)
		return listErr
	})
	return items, err
}

// add appends one relationship target while enforcing a stable result cap and deduplicating exact rows.
func (collector *relatedCollector) add(item RelatedResource) {
	key := strings.Join([]string{item.Type, item.Source, item.APIVersion, item.Kind, item.Namespace, item.Name, item.UID}, "\x00")
	if _, found := collector.itemKeys[key]; found {
		return
	}
	if len(collector.items) >= relatedResultLimit {
		collector.markTruncated("Related resources are capped at the safe response limit.")
		return
	}
	collector.itemKeys[key] = struct{}{}
	collector.items = append(collector.items, item)
}

// addNotice records one bounded relationship source failure without duplicating identical messages.
func (collector *relatedCollector) addNotice(relationshipType, source, state, message string) {
	collector.addNoticeWithDeferred(relationshipType, source, state, message, false)
}

// addDeferredNotice records a safe source that is intentionally available as a later operation.
func (collector *relatedCollector) addDeferredNotice(relationshipType, source, state, message string) {
	collector.addNoticeWithDeferred(relationshipType, source, state, message, true)
}

// addNoticeWithDeferred records one bounded relationship notice with its phase marker.
func (collector *relatedCollector) addNoticeWithDeferred(relationshipType, source, state, message string, deferred bool) {
	key := strings.Join([]string{relationshipType, source, state, message}, "\x00")
	if _, found := collector.noticeKeys[key]; found {
		return
	}
	if len(collector.notices) >= relatedNoticeLimit {
		collector.truncated = true
		return
	}
	collector.noticeKeys[key] = struct{}{}
	collector.notices = append(collector.notices, RelatedRelationshipNotice{Type: relationshipType, Source: source, State: state, Message: message, Deferred: deferred})
}

// markTruncated makes a bounded scan visible to callers through one explicit notice.
func (collector *relatedCollector) markTruncated(message string) {
	collector.truncated = true
	collector.addNotice("aggregation", "relationship discovery", relatedStatePartial, message)
}

// result finalizes the aggregate state and guarantees JSON arrays are never null.
func (collector *relatedCollector) result() RelatedResourceList {
	resolved := false
	nonResolved := false
	denied := false
	errored := false
	unavailable := false
	for _, item := range collector.items {
		if item.State == relatedItemResolved {
			resolved = true
			continue
		}
		nonResolved = true
		switch item.State {
		case relatedItemPermissionDenied:
			denied = true
		case relatedItemError:
			errored = true
		case relatedItemUnavailable:
			unavailable = true
		}
	}
	for _, notice := range collector.notices {
		nonResolved = true
		switch notice.State {
		case relatedItemPermissionDenied:
			denied = true
		case relatedItemError:
			errored = true
		case relatedItemUnavailable:
			unavailable = true
		}
	}
	if collector.truncated {
		nonResolved = true
	}
	state := relatedStateEmpty
	message := "No native relationships were found for this resource."
	switch {
	case resolved && !nonResolved:
		state = relatedStateAvailable
		message = "Relationships resolved from Kubernetes metadata and native spec fields."
	case resolved:
		state = relatedStatePartial
		message = "Some relationship sources were incomplete; each limitation is listed below."
	case denied && !errored && !unavailable:
		state = relatedStatePermissionDenied
		message = "Kubernetes RBAC denied one or more relationship reads."
	case errored && !denied && !unavailable:
		state = relatedStateError
		message = "One or more relationship sources could not be read."
	case unavailable && !denied && !errored:
		state = relatedStateUnavailable
		message = "The available Kubernetes fields were not sufficient to resolve every relationship."
	case nonResolved:
		state = relatedStatePartial
		message = "Relationship discovery returned explicit unresolved or limited states."
	}
	return RelatedResourceList{State: state, Message: message, Items: collector.items, Notices: collector.notices, Truncated: collector.truncated}
}

// relatedRequestFailure converts an essential read failure into a usable safe panel result.
func relatedRequestFailure(err error, budget *relatedCallBudget, started time.Time, deadline time.Duration) RelatedResourceList {
	state := relatedStateError
	noticeState := relatedNoticeState(err)
	message := "The current resource could not be read for relationship discovery."
	noticeMessage := "Kubernetes could not read the current resource needed for relationship discovery."
	truncated := false
	switch {
	case errors.Is(err, errRelatedCallBudget):
		state = relatedStatePartial
		noticeState = relatedStatePartial
		message = "Relationship discovery stopped at the safe Kubernetes-call budget."
		noticeMessage = "The relationship query reached its Kubernetes-call budget before the current resource could be read."
		truncated = true
	case errors.Is(err, context.DeadlineExceeded):
		state = relatedStatePartial
		noticeState = relatedStatePartial
		message = "Relationship discovery reached its safe request deadline."
		noticeMessage = "The relationship query deadline expired before the current resource could be read."
		truncated = true
	case errors.Is(err, context.Canceled):
		state = relatedStatePartial
		noticeState = relatedStatePartial
		message = "Relationship discovery was canceled before all sources could be read."
		noticeMessage = "The relationship query was canceled before the current resource could be read."
		truncated = true
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		state = relatedStatePermissionDenied
		noticeState = relatedItemPermissionDenied
		noticeMessage = "Kubernetes RBAC denied access to the current resource needed for relationship discovery."
	case apierrors.IsNotFound(err):
		state = relatedStateUnavailable
		noticeState = relatedItemUnresolved
		noticeMessage = "The current resource no longer exists or is not visible in this context."
	}
	return RelatedResourceList{
		State:       state,
		Message:     message,
		Items:       []RelatedResource{},
		Notices:     []RelatedRelationshipNotice{{Type: "resource", Source: "current resource", State: noticeState, Message: noticeMessage}},
		Truncated:   truncated,
		Diagnostics: relatedDiagnostics(budget, started, deadline),
	}
}

// relatedNoticeState maps Kubernetes access errors to safe relationship notice states.
func relatedNoticeState(err error) string {
	switch {
	case errors.Is(err, errRelatedCallBudget), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return relatedStatePartial
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return relatedItemPermissionDenied
	default:
		return relatedItemError
	}
}

// relatedItemErrorState maps a target GET error to an explicit relationship item state.
func relatedItemErrorState(err error) (string, string) {
	switch {
	case errors.Is(err, errRelatedCallBudget):
		return relatedStatePartial, "The relationship query reached its Kubernetes-call budget; this target lookup was skipped."
	case errors.Is(err, context.DeadlineExceeded):
		return relatedStatePartial, "The relationship query deadline expired before this target could be read."
	case errors.Is(err, context.Canceled):
		return relatedStatePartial, "The relationship query was canceled before this target could be read."
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return relatedItemPermissionDenied, "Kubernetes RBAC denied access to this relationship target."
	case apierrors.IsNotFound(err):
		return relatedItemUnresolved, "The relationship target no longer exists or is not visible in this context."
	default:
		return relatedItemError, "Kubernetes returned an error while reading this relationship target."
	}
}

// relatedDefinitionPointer returns a stable pointer suitable for a response item.
func relatedDefinitionPointer(definition ResourceDefinition) *ResourceDefinition {
	copy := definition
	return &copy
}

// apiVersionForDefinition converts a generic definition into its Kubernetes API version string.
func apiVersionForDefinition(definition ResourceDefinition) string {
	if definition.Group == "" {
		return definition.Version
	}
	return definition.Group + "/" + definition.Version
}

// relatedObjectKind returns an unstructured object's kind with a safe fallback.
func relatedObjectKind(object *unstructured.Unstructured, fallback string) string {
	if kind := object.GetKind(); kind != "" {
		return kind
	}
	return fallback
}
