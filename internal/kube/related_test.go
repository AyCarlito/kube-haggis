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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestCollectRelatedResourcesResolvesWorkloadDescendantsAndSelectors verifies generic workload relationships.
func TestCollectRelatedResourcesResolvesWorkloadDescendantsAndSelectors(t *testing.T) {
	deployment := relatedObject("apps/v1", "Deployment", "api", "default", "deployment-uid", map[string]interface{}{
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "api"}},
			"template": map[string]interface{}{"spec": map[string]interface{}{}},
		},
	})
	replicaSet := relatedObjectWithOwners("apps/v1", "ReplicaSet", "api-7d9f", "default", "replicaset-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "deployment-uid", Controller: boolPointer(true)}})
	pod := relatedObjectWithLabelsAndOwners("v1", "Pod", "api-7d9f-x", "default", "pod-uid", map[string]string{"app": "api"}, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7d9f", UID: "replicaset-uid", Controller: boolPointer(true)}})
	client := newRelatedDynamicClient(deployment, replicaSet, pod)

	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "apps", "v1", "deployments", "default", true, deployment)
	if result.State != relatedStateAvailable {
		t.Fatalf("unexpected aggregate state: %+v", result)
	}
	if !hasRelatedItem(result.Items, relatedTypeOwnedDescendant, "api-7d9f", relatedItemResolved) {
		t.Fatalf("ReplicaSet descendant was not resolved: %+v", result.Items)
	}
	if !hasRelatedItem(result.Items, relatedTypeSelectorMatch, "api-7d9f-x", relatedItemResolved) {
		t.Fatalf("selector-matched Pod was not resolved: %+v", result.Items)
	}

	replicaSetResult := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "apps", "v1", "replicasets", "default", true, replicaSet)
	owner := findRelatedItem(replicaSetResult.Items, relatedTypeOwner, "api")
	if owner == nil || owner.State != relatedItemResolved || !owner.Controller {
		t.Fatalf("direct owner navigation relationship was not preserved: %+v", replicaSetResult)
	}
}

// TestCollectRelatedResourcesResolvesServiceSelector verifies native Service selector matching.
func TestCollectRelatedResourcesResolvesServiceSelector(t *testing.T) {
	service := relatedObject("v1", "Service", "api", "default", "service-uid", map[string]interface{}{
		"spec": map[string]interface{}{"selector": map[string]interface{}{"app": "api"}},
	})
	matchingPod := relatedObjectWithLabelsAndOwners("v1", "Pod", "api-0", "default", "pod-uid", map[string]string{"app": "api"}, nil)
	otherPod := relatedObjectWithLabelsAndOwners("v1", "Pod", "worker-0", "default", "other-pod-uid", map[string]string{"app": "worker"}, nil)
	client := newRelatedDynamicClient(service, matchingPod, otherPod)

	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "", "v1", "services", "default", true, service)
	if result.State != relatedStateAvailable {
		t.Fatalf("unexpected Service relationship state: %+v", result)
	}
	if !hasRelatedItem(result.Items, relatedTypeSelectorMatch, "api-0", relatedItemResolved) || hasRelatedItem(result.Items, relatedTypeSelectorMatch, "worker-0", relatedItemResolved) {
		t.Fatalf("Service selector results were incorrect: %+v", result.Items)
	}
}

// TestCollectRelatedResourcesResolvesCronJobAndStorageBindings verifies owner and PVC/PV native fields.
func TestCollectRelatedResourcesResolvesCronJobAndStorageBindings(t *testing.T) {
	cronJob := relatedObject("batch/v1", "CronJob", "nightly", "default", "cronjob-uid", map[string]interface{}{
		"spec": map[string]interface{}{"jobTemplate": map[string]interface{}{"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{}}}}},
	})
	job := relatedObjectWithOwners("batch/v1", "Job", "nightly-123", "default", "job-uid", nil, []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: "nightly", UID: "cronjob-uid", Controller: boolPointer(true)}})
	client := newRelatedDynamicClient(cronJob, job)
	cronResult := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "batch", "v1", "cronjobs", "default", true, cronJob)
	if !hasRelatedItem(cronResult.Items, relatedTypeOwnedDescendant, "nightly-123", relatedItemResolved) {
		t.Fatalf("CronJob Job descendant was not resolved: %+v", cronResult)
	}

	pvc := relatedObject("v1", "PersistentVolumeClaim", "data", "default", "pvc-uid", map[string]interface{}{"spec": map[string]interface{}{"volumeName": "pv-data"}})
	pv := relatedObject("v1", "PersistentVolume", "pv-data", "", "pv-uid", map[string]interface{}{"spec": map[string]interface{}{"claimRef": map[string]interface{}{"name": "data", "namespace": "default", "uid": "pvc-uid"}}})
	storageClient := newRelatedDynamicClient(pvc, pv)
	pvcResult := collectRelatedResources(context.Background(), storageClient, relatedDefinitions(), nil, "", "v1", "persistentvolumeclaims", "default", true, pvc)
	if !hasRelatedItem(pvcResult.Items, relatedTypeBoundVolume, "pv-data", relatedItemResolved) {
		t.Fatalf("PVC bound PV was not resolved: %+v", pvcResult)
	}
	pvResult := collectRelatedResources(context.Background(), storageClient, relatedDefinitions(), nil, "", "v1", "persistentvolumes", "", false, pv)
	if !hasRelatedItem(pvResult.Items, relatedTypeBoundClaim, "data", relatedItemResolved) {
		t.Fatalf("PV bound PVC was not resolved: %+v", pvResult)
	}
}

// TestCollectRelatedResourcesReportsPermissionDenied verifies RBAC failures remain explicit and bounded.
func TestCollectRelatedResourcesReportsPermissionDenied(t *testing.T) {
	service := relatedObject("v1", "Service", "api", "default", "service-uid", map[string]interface{}{"spec": map[string]interface{}{"selector": map[string]interface{}{"app": "api"}}})
	client := newRelatedDynamicClient(service)
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pods", nil)
	})

	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "", "v1", "services", "default", true, service)
	if result.State != relatedStatePermissionDenied {
		t.Fatalf("permission denial was not classified: %+v", result)
	}
	if len(result.Items) != 0 || len(result.Notices) == 0 || !hasNoticeState(result.Notices, relatedItemPermissionDenied) {
		t.Fatalf("permission denial was not explicit: %+v", result)
	}
}

// TestCollectRelatedResourcesReportsUnresolvedOwner verifies stale owner references do not become broken links.
func TestCollectRelatedResourcesReportsUnresolvedOwner(t *testing.T) {
	child := relatedObjectWithOwners("apps/v1", "ReplicaSet", "gone", "default", "child-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "missing", UID: "missing-uid"}})
	client := newRelatedDynamicClient(child)
	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "apps", "v1", "replicasets", "default", true, child)

	if result.State != relatedStatePartial || !hasRelatedItem(result.Items, relatedTypeOwner, "missing", relatedItemUnresolved) {
		t.Fatalf("stale owner was not explicit: %+v", result)
	}
	owner := findRelatedItem(result.Items, relatedTypeOwner, "missing")
	if owner == nil || owner.Message == "" || owner.Definition == nil {
		t.Fatalf("stale owner lost navigation context: %+v", owner)
	}
}

// TestCollectRelatedResourcesDoesNotReturnSecretData verifies relationship payloads contain coordinates, never Secret values.
func TestCollectRelatedResourcesDoesNotReturnSecretData(t *testing.T) {
	pod := relatedObject("v1", "Pod", "api", "default", "pod-uid", map[string]interface{}{
		"spec": map[string]interface{}{"volumes": []interface{}{map[string]interface{}{"name": "credentials", "secret": map[string]interface{}{"secretName": "api-credentials"}}}},
	})
	secret := relatedObject("v1", "Secret", "api-credentials", "default", "secret-uid", map[string]interface{}{"data": map[string]interface{}{"token": "super-secret-value"}})
	client := newRelatedDynamicClient(pod, secret)
	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "", "v1", "pods", "default", true, pod)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal relationship result: %v", err)
	}
	if strings.Contains(string(encoded), "super-secret-value") {
		t.Fatalf("Secret value leaked into relationship payload: %s", encoded)
	}
	if !hasRelatedItem(result.Items, relatedTypeReference, "api-credentials", relatedItemResolved) {
		t.Fatalf("Secret reference was not resolved: %+v", result)
	}
}

// TestCollectRelatedResourcesDefersReverseOwnerDiscovery verifies direct links return before broad scans.
func TestCollectRelatedResourcesDefersReverseOwnerDiscovery(t *testing.T) {
	deployment := relatedObject("apps/v1", "Deployment", "api", "default", "deployment-uid", nil)
	replicaSet := relatedObjectWithOwners("apps/v1", "ReplicaSet", "api-rs", "default", "replicaset-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "deployment-uid"}})
	client := newRelatedDynamicClient(deployment, replicaSet)

	result := collectRelatedResourcesWithBudget(context.Background(), client, relatedDefinitions(), nil, "apps", "v1", "replicasets", "default", true, replicaSet, newRelatedCallBudget(context.Background()), true, false)
	if result.State != relatedStatePartial || !result.Truncated {
		t.Fatalf("direct relationship result did not expose deferred reverse discovery: %+v", result)
	}
	if !hasRelatedItem(result.Items, relatedTypeOwner, "api", relatedItemResolved) {
		t.Fatalf("direct owner was not resolved before deferral: %+v", result.Items)
	}
	if hasRelatedItem(result.Items, relatedTypeOwnedDescendant, "api-rs", relatedItemResolved) || !hasDeferredNotice(result.Notices) {
		t.Fatalf("reverse-owner phase was not deferred explicitly: %+v", result)
	}
	if result.Diagnostics.QueryCount != 1 {
		t.Fatalf("unexpected direct-phase query count: %+v", result.Diagnostics)
	}
}

// TestCollectRelatedResourcesEnforcesCallBudget verifies broad discovery never exceeds the deterministic call cap.
func TestCollectRelatedResourcesEnforcesCallBudget(t *testing.T) {
	definitions := syntheticRelatedDefinitions(64)
	current := relatedObject("fixture.example/v1", "Fixture", "current", "default", "current-uid", nil)
	client := newRelatedDynamicClientForDefinitions(definitions, current)

	result := collectRelatedResourcesWithBudget(context.Background(), client, definitions, nil, "fixture.example", "v1", "resources-0", "default", true, current, newRelatedCallBudget(context.Background()), false, true)
	if result.Diagnostics.QueryCount != relatedCallBudgetLimit {
		t.Fatalf("relationship query exceeded or failed to use call budget: %+v", result.Diagnostics)
	}
	if !result.Truncated || !hasNoticeState(result.Notices, relatedStatePartial) {
		t.Fatalf("call budget exhaustion was not explicit: %+v", result)
	}
	if listActionCount(client) > relatedCallBudgetLimit {
		t.Fatalf("fake Kubernetes client observed too many list calls: %d", listActionCount(client))
	}
}

// TestCollectRelatedResourcesMemoizesTargetLookups verifies repeated native references share one target GET.
func TestCollectRelatedResourcesMemoizesTargetLookups(t *testing.T) {
	pod := relatedObject("v1", "Pod", "api", "default", "pod-uid", map[string]interface{}{
		"spec": map[string]interface{}{
			"imagePullSecrets": []interface{}{map[string]interface{}{"name": "shared-secret"}},
			"volumes":          []interface{}{map[string]interface{}{"secret": map[string]interface{}{"secretName": "shared-secret"}}},
		},
	})
	secret := relatedObject("v1", "Secret", "shared-secret", "default", "secret-uid", nil)
	client := newRelatedDynamicClient(pod, secret)

	result := collectRelatedResourcesWithBudget(context.Background(), client, relatedDefinitions(), nil, "", "v1", "pods", "default", true, pod, newRelatedCallBudget(context.Background()), true, false)
	if len(result.Items) != 2 || result.Diagnostics.QueryCount != 2 {
		t.Fatalf("repeated target lookup was not memoized: result=%+v actions=%#v", result, client.Actions())
	}
	if getActionCount(client, "secrets", "shared-secret") != 1 {
		t.Fatalf("expected one Secret GET, actions=%+v", client.Actions())
	}
}

// TestRelatedCallBudgetCapsConcurrentOperations verifies the semaphore does not admit more concurrent calls than configured.
func TestRelatedCallBudgetCapsConcurrentOperations(t *testing.T) {
	budget := newRelatedCallBudget(context.Background())
	started := make(chan struct{}, relatedConcurrencyLimit+1)
	release := make(chan struct{})
	var waitGroup sync.WaitGroup
	for index := 0; index < relatedConcurrencyLimit+1; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := budget.call(func() error {
				started <- struct{}{}
				<-release
				return nil
			}); err != nil {
				t.Errorf("bounded operation failed: %v", err)
			}
		}()
	}
	for index := 0; index < relatedConcurrencyLimit; index++ {
		<-started
	}
	select {
	case <-started:
		t.Fatal("more operations started than the concurrency limit")
	default:
	}
	close(release)
	waitGroup.Wait()
	if budget.count() != relatedConcurrencyLimit+1 {
		t.Fatalf("unexpected admitted operation count: %d", budget.count())
	}
}

// BenchmarkDirectRelationshipsWith1000Definitions measures direct relationship work against a large discovery fixture.
func BenchmarkDirectRelationshipsWith1000Definitions(b *testing.B) {
	// The direct-relationship p95 target is 100ms on the local fake-cluster fixture.
	const directRelationshipP95Target = 100 * time.Millisecond
	definitions := syntheticRelatedDefinitions(1000)
	owner := relatedObject("apps/v1", "Deployment", "api", "default", "deployment-uid", nil)
	current := relatedObjectWithOwners("fixture.example/v1", "Fixture", "current", "default", "current-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "deployment-uid"}})
	client := newRelatedDynamicClientForDefinitions(definitions, current, owner)
	b.ReportAllocs()
	b.ReportMetric(float64(directRelationshipP95Target.Milliseconds()), "p95-target-ms")
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result := collectRelatedResourcesWithBudget(context.Background(), client, definitions, nil, "fixture.example", "v1", "resources-0", "default", true, current, newRelatedCallBudget(context.Background()), true, false)
		if result.State != relatedStatePartial || !hasRelatedItem(result.Items, relatedTypeOwner, "api", relatedItemResolved) {
			b.Fatalf("direct relationship benchmark fixture failed: %+v", result)
		}
	}
}

// relatedDefinitions returns the native definitions exercised by relationship tests.
func relatedDefinitions() []ResourceDefinition {
	return []ResourceDefinition{
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "services", Kind: "Service", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "persistentvolumes", Kind: "PersistentVolume", Namespaced: false, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "serviceaccounts", Kind: "ServiceAccount", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "batch", Version: "v1", Resource: "jobs", Kind: "Job", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "batch", Version: "v1", Resource: "cronjobs", Kind: "CronJob", Namespaced: true, Verbs: []string{"get", "list"}},
	}
}

// newRelatedDynamicClient creates a dynamic fake with list kinds for native relationship resources.
func newRelatedDynamicClient(objects ...runtime.Object) *fake.FakeDynamicClient {
	return newRelatedDynamicClientForDefinitions(relatedDefinitions(), objects...)
}

// newRelatedDynamicClientForDefinitions creates a dynamic fake with caller-provided list kinds.
func newRelatedDynamicClientForDefinitions(definitions []ResourceDefinition, objects ...runtime.Object) *fake.FakeDynamicClient {
	listKinds := map[schema.GroupVersionResource]string{}
	for _, definition := range definitions {
		listKinds[schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}] = definition.Kind + "List"
	}
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
}

// syntheticRelatedDefinitions creates a deterministic large discovery fixture for budget and benchmark tests.
func syntheticRelatedDefinitions(count int) []ResourceDefinition {
	definitions := relatedDefinitions()
	for index := 0; len(definitions) < count; index++ {
		definitions = append(definitions, ResourceDefinition{
			Group:      "fixture.example",
			Version:    "v1",
			Resource:   fmt.Sprintf("resources-%d", index),
			Kind:       fmt.Sprintf("Fixture%d", index),
			Namespaced: true,
			Verbs:      []string{"get", "list"},
		})
	}
	return definitions
}

// relatedObject creates an unstructured object with safe metadata and a supplied top-level shape.
func relatedObject(apiVersion, kind, name, namespace, uid string, fields map[string]interface{}) *unstructured.Unstructured {
	object := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name": name,
			"uid":  uid,
		},
	}
	if namespace != "" {
		object["metadata"].(map[string]interface{})["namespace"] = namespace
	}
	for key, value := range fields {
		object[key] = value
	}
	return &unstructured.Unstructured{Object: object}
}

// relatedObjectWithOwners adds owner references to a generic unstructured object.
func relatedObjectWithOwners(apiVersion, kind, name, namespace, uid string, fields map[string]interface{}, owners []metav1.OwnerReference) *unstructured.Unstructured {
	object := relatedObject(apiVersion, kind, name, namespace, uid, fields)
	object.SetOwnerReferences(owners)
	return object
}

// relatedObjectWithLabelsAndOwners creates a resource with labels and optional owner references.
func relatedObjectWithLabelsAndOwners(apiVersion, kind, name, namespace, uid string, labels map[string]string, owners []metav1.OwnerReference) *unstructured.Unstructured {
	object := relatedObject(apiVersion, kind, name, namespace, uid, nil)
	object.SetLabels(labels)
	object.SetOwnerReferences(owners)
	return object
}

// boolPointer returns a pointer suitable for an OwnerReference boolean field.
func boolPointer(value bool) *bool {
	return &value
}

// hasRelatedItem finds a relationship by type, name, and item state.
func hasRelatedItem(items []RelatedResource, relationshipType, name, state string) bool {
	for _, item := range items {
		if item.Type == relationshipType && item.Name == name && item.State == state {
			return true
		}
	}
	return false
}

// findRelatedItem finds the first relationship by type and name.
func findRelatedItem(items []RelatedResource, relationshipType, name string) *RelatedResource {
	for index := range items {
		if items[index].Type == relationshipType && items[index].Name == name {
			return &items[index]
		}
	}
	return nil
}

// hasNoticeState reports whether a relationship notice has the requested state.
func hasNoticeState(notices []RelatedRelationshipNotice, state string) bool {
	for _, notice := range notices {
		if notice.State == state {
			return true
		}
	}
	return false
}

// hasDeferredNotice reports whether a response advertises its secondary relationship phase.
func hasDeferredNotice(notices []RelatedRelationshipNotice) bool {
	for _, notice := range notices {
		if notice.Deferred {
			return true
		}
	}
	return false
}

// listActionCount counts Kubernetes list calls observed by a dynamic fake.
func listActionCount(client *fake.FakeDynamicClient) int {
	count := 0
	for _, action := range client.Actions() {
		if _, ok := action.(clienttesting.ListAction); ok {
			count++
		}
	}
	return count
}

// getActionCount counts target GET calls for one resource and name.
func getActionCount(client *fake.FakeDynamicClient, resource, name string) int {
	count := 0
	for _, action := range client.Actions() {
		getAction, ok := action.(clienttesting.GetAction)
		if ok && getAction.GetResource().Resource == resource && getAction.GetName() == name {
			count++
		}
	}
	return count
}
