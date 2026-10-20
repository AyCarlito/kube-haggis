package kube

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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

// relatedDefinitions returns the native definitions exercised by relationship tests.
func relatedDefinitions() []ResourceDefinition {
	return []ResourceDefinition{
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "services", Kind: "Service", Namespaced: true, Verbs: []string{"get", "list"}},
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
	listKinds := map[schema.GroupVersionResource]string{}
	for _, definition := range relatedDefinitions() {
		listKinds[schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}] = definition.Kind + "List"
	}
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
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
