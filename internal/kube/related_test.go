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

// TestCollectRelatedResourcesResolvesEventInvolvedObject verifies exact Event target identity and navigation data.
func TestCollectRelatedResourcesResolvesEventInvolvedObject(t *testing.T) {
	event := relatedObject("v1", "Event", "warning", "default", "event-uid", map[string]interface{}{
		"involvedObject": map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "api-0",
			"namespace":  "default",
			"uid":        "pod-uid",
		},
	})
	pod := relatedObject("v1", "Pod", "api-0", "default", "pod-uid", nil)
	client := newRelatedDynamicClient(event, pod)

	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "", "v1", "events", "default", true, event)
	item := findRelatedItem(result.Items, relatedTypeInvolvedObject, "api-0")
	if item == nil || item.State != relatedItemResolved || item.APIVersion != "v1" || item.Kind != "Pod" || item.Namespace != "default" || item.UID != "pod-uid" {
		t.Fatalf("Event involvedObject was not resolved safely: %+v", result)
	}
	if item.Definition == nil || item.Definition.Group != "" || item.Definition.Version != "v1" || item.Definition.Resource != "pods" || !item.Definition.Namespaced {
		t.Fatalf("Event involvedObject did not retain the exact target GVR: %+v", item)
	}
	if result.Diagnostics.QueryCount != 1 || listActionCount(client) != 0 {
		t.Fatalf("Event relationship used an unexpected reverse/list query: diagnostics=%+v actions=%#v", result.Diagnostics, client.Actions())
	}
}

// TestCollectRelatedResourcesRejectsEventTargetUIDMismatch verifies stale Event references remain explicit and unclickable.
func TestCollectRelatedResourcesRejectsEventTargetUIDMismatch(t *testing.T) {
	event := relatedObject("v1", "Event", "warning", "default", "event-uid", map[string]interface{}{
		"involvedObject": map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "api-0",
			"namespace":  "default",
			"uid":        "stale-pod-uid",
		},
	})
	pod := relatedObject("v1", "Pod", "api-0", "default", "current-pod-uid", nil)
	client := newRelatedDynamicClient(event, pod)

	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "", "v1", "events", "default", true, event)
	item := findRelatedItem(result.Items, relatedTypeInvolvedObject, "api-0")
	if item == nil || item.State != relatedItemUnresolved || item.Message == "" || item.Definition == nil {
		t.Fatalf("stale Event target was not explicit: %+v", result)
	}
}

// TestCollectRelatedResourcesDoesNotReverseLoadEvents verifies Event relationships omit the deferred reverse phase.
func TestCollectRelatedResourcesDoesNotReverseLoadEvents(t *testing.T) {
	event := relatedObject("v1", "Event", "warning", "default", "event-uid", map[string]interface{}{
		"involvedObject": map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "name": "missing", "namespace": "default"},
	})
	client := newRelatedDynamicClient(event)

	result := collectRelatedResourcesWithBudget(context.Background(), client, relatedDefinitions(), nil, "", "v1", "events", "default", true, event, newRelatedCallBudget(context.Background()), false, true)
	if result.Diagnostics.QueryCount != 0 || listActionCount(client) != 0 || len(result.Items) != 0 || len(result.Notices) != 0 {
		t.Fatalf("Event reverse phase was not omitted: diagnostics=%+v result=%+v actions=%#v", result.Diagnostics, result, client.Actions())
	}
}

// TestCollectRelatedResourcesResolvesIngressServiceBackend verifies Ingress Service backends are direct related targets.
func TestCollectRelatedResourcesResolvesIngressServiceBackend(t *testing.T) {
	ingress := relatedObject("networking.k8s.io/v1", "Ingress", "edge", "default", "ingress-uid", map[string]interface{}{
		"spec": map[string]interface{}{
			"defaultBackend": map[string]interface{}{"service": map[string]interface{}{"name": "edge-service"}},
			"rules": []interface{}{map[string]interface{}{
				"http": map[string]interface{}{"paths": []interface{}{map[string]interface{}{
					"backend": map[string]interface{}{"service": map[string]interface{}{"name": "edge-service"}},
				}}},
			}},
		},
	})
	service := relatedObject("v1", "Service", "edge-service", "default", "service-uid", nil)
	client := newRelatedDynamicClient(ingress, service)

	result := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "networking.k8s.io", "v1", "ingresses", "default", true, ingress)
	item := findRelatedItem(result.Items, relatedTypeIngressBackend, "edge-service")
	if result.State != relatedStateAvailable || item == nil || item.State != relatedItemResolved {
		t.Fatalf("Ingress Service backend was not resolved: %+v", result)
	}
	if item.Definition == nil || item.Definition.Group != "" || item.Definition.Version != "v1" || item.Definition.Resource != "services" || item.Namespace != "default" {
		t.Fatalf("Ingress backend lost the exact Service target identity: %+v", item)
	}
	backendCount := 0
	for _, related := range result.Items {
		if related.Type == relatedTypeIngressBackend && related.Name == "edge-service" {
			backendCount++
		}
	}
	if backendCount != 1 || getActionCount(client, "services", "edge-service") != 1 {
		t.Fatalf("Ingress backend was not deduplicated to one target GET: count=%d actions=%#v", backendCount, client.Actions())
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

// TestCollectRelatedResourcesResolvesServiceEndpointSlices verifies the native Service label relationship in both directions.
func TestCollectRelatedResourcesResolvesServiceEndpointSlices(t *testing.T) {
	service := relatedObject("v1", "Service", "api", "default", "service-uid", map[string]interface{}{
		"spec": map[string]interface{}{"selector": map[string]interface{}{"app": "api"}},
	})
	matchingSlice := relatedObject("discovery.k8s.io/v1", "EndpointSlice", "api-1", "default", "slice-uid", nil)
	matchingSlice.SetLabels(map[string]string{"kubernetes.io/service-name": "api"})
	matchingSlice.Object["endpoints"] = []interface{}{map[string]interface{}{"targetRef": map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "name": "api-0", "namespace": "default", "uid": "pod-uid"}}}
	otherSlice := relatedObject("discovery.k8s.io/v1", "EndpointSlice", "worker-1", "default", "other-slice-uid", nil)
	otherSlice.SetLabels(map[string]string{"kubernetes.io/service-name": "worker"})
	pod := relatedObject("v1", "Pod", "api-0", "default", "pod-uid", nil)
	client := newRelatedDynamicClient(service, matchingSlice, otherSlice, pod)

	serviceResult := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "", "v1", "services", "default", true, service)
	if !hasRelatedItem(serviceResult.Items, relatedTypeEndpointSlice, "api-1", relatedItemResolved) || hasRelatedItem(serviceResult.Items, relatedTypeEndpointSlice, "worker-1", relatedItemResolved) {
		t.Fatalf("Service EndpointSlice relationships were incorrect: %+v", serviceResult)
	}

	sliceResult := collectRelatedResources(context.Background(), client, relatedDefinitions(), nil, "discovery.k8s.io", "v1", "endpointslices", "default", true, matchingSlice)
	serviceItem := findRelatedItem(sliceResult.Items, relatedTypeEndpointSlice, "api")
	if serviceItem == nil || serviceItem.State != relatedItemResolved || serviceItem.Definition == nil || serviceItem.Definition.Resource != "services" || serviceItem.Namespace != "default" {
		t.Fatalf("EndpointSlice Service relationship was not resolved with exact identity: %+v", sliceResult)
	}
	podItem := findRelatedItem(sliceResult.Items, relatedTypeEndpointPod, "api-0")
	if podItem == nil || podItem.State != relatedItemResolved || podItem.Definition == nil || podItem.Definition.Resource != "pods" || podItem.Namespace != "default" || podItem.UID != "pod-uid" {
		t.Fatalf("EndpointSlice Pod target relationship was not resolved with UID verification: %+v", sliceResult)
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

// TestCollectRelatedResourcesDoesNotScanUnrelatedDiscoveryResources verifies unsupported discovery entries are never reverse-scanned.
func TestCollectRelatedResourcesDoesNotScanUnrelatedDiscoveryResources(t *testing.T) {
	definitions := syntheticRelatedDefinitions(64)
	current := relatedObject("fixture.example/v1", "Fixture", "current", "default", "current-uid", nil)
	client := newRelatedDynamicClientForDefinitions(definitions, current)

	result := collectRelatedResourcesWithBudget(context.Background(), client, definitions, nil, "fixture.example", "v1", "resources-0", "default", true, current, newRelatedCallBudget(context.Background()), false, true)
	if result.Diagnostics.QueryCount != 0 {
		t.Fatalf("unsupported resource unexpectedly issued relationship queries: %+v", result.Diagnostics)
	}
	if result.State != relatedStateEmpty || len(result.Items) != 0 || len(result.Notices) != 0 {
		t.Fatalf("unsupported resource returned reverse relationship data: %+v", result)
	}
	if listActionCount(client) != 0 {
		t.Fatalf("fake Kubernetes client scanned unrelated discovered resources: %d", listActionCount(client))
	}
}

// TestOwnedDescendantQueryPlanCoversNativeChains verifies every Phase 1 native controller child mapping.
func TestOwnedDescendantQueryPlanCoversNativeChains(t *testing.T) {
	cases := []struct {
		name          string
		group         string
		version       string
		resource      string
		kind          string
		childGroup    string
		childVersion  string
		childResource string
	}{
		{name: "Deployment ReplicaSet", group: "apps", version: "v1", resource: "deployments", kind: "Deployment", childGroup: "apps", childVersion: "v1", childResource: "replicasets"},
		{name: "ReplicaSet Pod", group: "apps", version: "v1", resource: "replicasets", kind: "ReplicaSet", childGroup: "", childVersion: "v1", childResource: "pods"},
		{name: "StatefulSet Pod", group: "apps", version: "v1", resource: "statefulsets", kind: "StatefulSet", childGroup: "", childVersion: "v1", childResource: "pods"},
		{name: "DaemonSet Pod", group: "apps", version: "v1", resource: "daemonsets", kind: "DaemonSet", childGroup: "", childVersion: "v1", childResource: "pods"},
		{name: "Job Pod", group: "batch", version: "v1", resource: "jobs", kind: "Job", childGroup: "", childVersion: "v1", childResource: "pods"},
		{name: "CronJob Job", group: "batch", version: "v1", resource: "cronjobs", kind: "CronJob", childGroup: "batch", childVersion: "v1", childResource: "jobs"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			apiVersion := testCase.version
			if testCase.group != "" {
				apiVersion = testCase.group + "/" + testCase.version
			}
			object := relatedObject(apiVersion, testCase.kind, "current", "default", "current-uid", nil)
			collector := &relatedCollector{
				definitions:       relatedDefinitions(),
				object:            object,
				currentGroup:      testCase.group,
				currentVersion:    testCase.version,
				currentResource:   testCase.resource,
				currentNamespaced: true,
			}
			plan := collector.ownedDescendantQueryPlan()
			if len(plan) != 1 || plan[0].definition.Group != testCase.childGroup || plan[0].definition.Version != testCase.childVersion || plan[0].definition.Resource != testCase.childResource || plan[0].namespace != "default" {
				t.Fatalf("unexpected native descendant query plan: %+v", plan)
			}
		})
	}
}

// TestCollectRelatedResourcesFiltersSupportedDescendantsByOwnerUID verifies exact child GVR and owner UID filtering.
func TestCollectRelatedResourcesFiltersSupportedDescendantsByOwnerUID(t *testing.T) {
	deployment := relatedObject("apps/v1", "Deployment", "api", "default", "deployment-uid", nil)
	matchingReplicaSet := relatedObjectWithOwners("apps/v1", "ReplicaSet", "api-rs", "default", "replicaset-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "deployment-uid"}})
	otherReplicaSet := relatedObjectWithOwners("apps/v1", "ReplicaSet", "other-rs", "default", "other-replicaset-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "other", UID: "other-deployment-uid"}})
	customChild := relatedObjectWithOwners("example.test/v1", "Widget", "api-widget", "default", "widget-uid", nil, []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "deployment-uid"}})
	definitions := append(relatedDefinitions(), ResourceDefinition{Group: "example.test", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}})
	client := newRelatedDynamicClientForDefinitions(definitions, deployment, matchingReplicaSet, otherReplicaSet, customChild)

	result := collectRelatedResourcesWithBudget(context.Background(), client, definitions, nil, "apps", "v1", "deployments", "default", true, deployment, newRelatedCallBudget(context.Background()), false, true)
	matching := findRelatedItem(result.Items, relatedTypeOwnedDescendant, "api-rs")
	if matching == nil || matching.State != relatedItemResolved || matching.Definition == nil || matching.Definition.Group != "apps" || matching.Definition.Resource != "replicasets" {
		t.Fatalf("supported ReplicaSet descendant was not resolved: %+v", result)
	}
	if hasRelatedItem(result.Items, relatedTypeOwnedDescendant, "other-rs", relatedItemResolved) || hasRelatedItem(result.Items, relatedTypeOwnedDescendant, "api-widget", relatedItemResolved) {
		t.Fatalf("descendant filtering returned an unrelated object: %+v", result.Items)
	}
	if result.Diagnostics.QueryCount != 1 || listActionCount(client) != 1 {
		t.Fatalf("targeted descendant query did not issue exactly one child list: diagnostics=%+v actions=%#v", result.Diagnostics, client.Actions())
	}
	for _, action := range client.Actions() {
		listAction, ok := action.(clienttesting.ListAction)
		if !ok {
			continue
		}
		if listAction.GetResource().Group != "apps" || listAction.GetResource().Version != "v1" || listAction.GetResource().Resource != "replicasets" || listAction.GetNamespace() != "default" {
			t.Fatalf("targeted query used an unsafe or unrelated list target: %#v", listAction)
		}
	}
}

// TestCollectRelatedResourcesReportsUnavailableSupportedChild verifies one limitation is returned when a native child API is undiscovered.
func TestCollectRelatedResourcesReportsUnavailableSupportedChild(t *testing.T) {
	deployment := relatedObject("apps/v1", "Deployment", "api", "default", "deployment-uid", nil)
	definitions := make([]ResourceDefinition, 0, len(relatedDefinitions())-1)
	for _, definition := range relatedDefinitions() {
		if definition.Group == "apps" && definition.Version == "v1" && definition.Resource == "replicasets" {
			continue
		}
		definitions = append(definitions, definition)
	}
	client := newRelatedDynamicClientForDefinitions(definitions, deployment)

	result := collectRelatedResourcesWithBudget(context.Background(), client, definitions, nil, "apps", "v1", "deployments", "default", true, deployment, newRelatedCallBudget(context.Background()), true, false)
	limitations := 0
	for _, notice := range result.Notices {
		if notice.Type == relatedTypeOwnedDescendant {
			limitations++
			if notice.State != relatedItemUnavailable || notice.Deferred {
				t.Fatalf("unavailable child limitation had the wrong state: %+v", notice)
			}
		}
	}
	if limitations != 1 || len(result.Items) != 0 || listActionCount(client) != 0 {
		t.Fatalf("unavailable supported child was not summarized once without a query: result=%+v actions=%#v", result, client.Actions())
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
		if result.State != relatedStateAvailable || !hasRelatedItem(result.Items, relatedTypeOwner, "api", relatedItemResolved) {
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
		{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices", Kind: "EndpointSlice", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "apps", Version: "v1", Resource: "statefulsets", Kind: "StatefulSet", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "apps", Version: "v1", Resource: "daemonsets", Kind: "DaemonSet", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "batch", Version: "v1", Resource: "jobs", Kind: "Job", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "batch", Version: "v1", Resource: "cronjobs", Kind: "CronJob", Namespaced: true, Verbs: []string{"get", "list"}},
		{Group: "", Version: "v1", Resource: "events", Kind: "Event", Namespaced: true, Verbs: []string{"get", "list"}},
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
