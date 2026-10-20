package kube

import (
	"context"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// newWorkloadDynamicClient creates a dynamic fake with list kinds for workload revision and Job reads.
func newWorkloadDynamicClient(objects ...runtime.Object) *fake.FakeDynamicClient {
	listKinds := map[schema.GroupVersionResource]string{
		appsGVR("replicasets"): "ReplicaSetList",
		batchGVR("jobs"):       "JobList",
	}
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
}

// workloadDeployment returns a resource-versioned Deployment with one regular container.
func workloadDeployment(name, resourceVersion, revision, image string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":            name,
			"namespace":       "default",
			"uid":             "deployment-uid",
			"resourceVersion": resourceVersion,
			"annotations":     map[string]interface{}{"deployment.kubernetes.io/revision": revision},
			"managedFields":   []interface{}{map[string]interface{}{"manager": "controller"}},
		},
		"spec": map[string]interface{}{
			"replicas": int64(2),
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{},
				"spec": map[string]interface{}{
					"containers": []interface{}{map[string]interface{}{"name": "app", "image": image}},
				},
			},
		},
	}}
}

// TestValidateWorkloadActionRejectsUnsafeAndUnsupportedRequests verifies local operation validation.
func TestValidateWorkloadActionRejectsUnsafeAndUnsupportedRequests(t *testing.T) {
	zero := int64(-1)
	tests := []struct {
		name     string
		group    string
		resource string
		request  WorkloadActionRequest
		want     string
	}{
		{name: "missing namespace", group: "apps", resource: "deployments", request: WorkloadActionRequest{Action: "restart"}, want: "namespace is required"},
		{name: "daemonset scale", group: "apps", resource: "daemonsets", request: WorkloadActionRequest{Action: "scale"}, want: "unsupported workload action"},
		{name: "negative scale", group: "apps", resource: "deployments", request: WorkloadActionRequest{Action: "scale", Replicas: &zero}, want: "cannot be negative"},
		{name: "missing image container", group: "apps", resource: "deployments", request: WorkloadActionRequest{Action: "image", Image: "app:v2"}, want: "container is required"},
		{name: "schedule control character", group: "batch", resource: "cronjobs", request: WorkloadActionRequest{Action: "schedule", Schedule: "0\n* * * *"}, want: "invalid control"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			namespace := "default"
			if test.name == "missing namespace" {
				namespace = ""
			}
			err := validateWorkloadAction(test.group, "v1", test.resource, namespace, "worker", test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// TestPerformWorkloadActionUsesResourceVersionedRestartUpdate verifies native restart mutation safety.
func TestPerformWorkloadActionUsesResourceVersionedRestartUpdate(t *testing.T) {
	client := newWorkloadDynamicClient(workloadDeployment("api", "7", "3", "app:v1"))
	var captured *unstructured.Unstructured
	client.PrependReactor("update", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		captured = action.(clienttesting.UpdateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		return true, captured, nil
	})

	result, err := performWorkloadAction(context.Background(), client, "apps", "v1", "deployments", "default", "api", WorkloadActionRequest{
		Action:                  "restart",
		ExpectedResourceVersion: "7",
		DryRun:                  true,
	})
	if err != nil {
		t.Fatalf("restart action failed: %v", err)
	}
	if result.Status != "dry-run passed" || captured == nil {
		t.Fatalf("unexpected restart result: %+v captured=%v", result, captured)
	}
	if captured.GetResourceVersion() != "7" {
		t.Fatalf("resource version was not preserved: %q", captured.GetResourceVersion())
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(captured.Object, "metadata", "managedFields"); found {
		t.Fatal("managedFields were sent in workload update")
	}
	annotation, found, err := unstructured.NestedString(captured.Object, "spec", "template", "metadata", "annotations", workloadRestartAnnotation)
	if err != nil || !found || annotation == "" {
		t.Fatalf("restart annotation missing: %q found=%t err=%v", annotation, found, err)
	}
}

// TestPerformWorkloadActionUsesScaleSubresource verifies Deployment scaling uses the native scale endpoint.
func TestPerformWorkloadActionUsesScaleSubresource(t *testing.T) {
	client := newWorkloadDynamicClient(workloadDeployment("api", "7", "3", "app:v1"))
	var scaleUpdate *unstructured.Unstructured
	client.PrependReactor("get", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "autoscaling/v1",
			"kind":       "Scale",
			"metadata":   map[string]interface{}{"name": "api", "namespace": "default", "resourceVersion": "7"},
			"spec":       map[string]interface{}{"replicas": int64(2)},
		}}, nil
	})
	client.PrependReactor("update", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		scaleUpdate = action.(clienttesting.UpdateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		return true, scaleUpdate, nil
	})
	replicas := int64(5)
	result, err := performWorkloadAction(context.Background(), client, "apps", "v1", "deployments", "default", "api", WorkloadActionRequest{
		Action:                  "scale",
		Replicas:                &replicas,
		ExpectedResourceVersion: "7",
	})
	if err != nil {
		t.Fatalf("scale action failed: %v", err)
	}
	if scaleUpdate == nil || result.Status != "applied" || result.ResourceVersion != "7" {
		t.Fatalf("unexpected scale result: %+v update=%v", result, scaleUpdate)
	}
	got, _, err := unstructured.NestedInt64(scaleUpdate.Object, "spec", "replicas")
	if err != nil || got != replicas {
		t.Fatalf("unexpected scale payload: %d err=%v", got, err)
	}
}

// TestPerformWorkloadActionRejectsStaleResourceVersion verifies stale UI actions do not update a newer object.
func TestPerformWorkloadActionRejectsStaleResourceVersion(t *testing.T) {
	client := newWorkloadDynamicClient(workloadDeployment("api", "8", "3", "app:v1"))
	updates := 0
	client.PrependReactor("update", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
		updates++
		return true, nil, nil
	})
	_, err := performWorkloadAction(context.Background(), client, "apps", "v1", "deployments", "default", "api", WorkloadActionRequest{
		Action:                  "restart",
		ExpectedResourceVersion: "7",
	})
	if err == nil || !strings.Contains(err.Error(), "resource version conflict") {
		t.Fatalf("expected resource-version conflict, got %v", err)
	}
	if updates != 0 {
		t.Fatalf("stale action attempted an update: %d", updates)
	}
}

// TestPerformWorkloadActionRollsBackToOwnedReplicaSet verifies revision selection and template restoration.
func TestPerformWorkloadActionRollsBackToOwnedReplicaSet(t *testing.T) {
	deployment := workloadDeployment("api", "7", "3", "app:v3")
	ownedRevision := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "ReplicaSet",
		"metadata": map[string]interface{}{
			"name":            "api-old",
			"namespace":       "default",
			"ownerReferences": []interface{}{map[string]interface{}{"kind": "Deployment", "name": "api", "uid": "deployment-uid"}},
			"annotations":     map[string]interface{}{"deployment.kubernetes.io/revision": "1"},
		},
		"spec": map[string]interface{}{"template": map[string]interface{}{"metadata": map[string]interface{}{}, "spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "app", "image": "app:v1"}}}}},
	}}
	unownedRevision := ownedRevision.DeepCopy()
	unownedRevision.SetName("other")
	unownedRevision.Object["metadata"].(map[string]interface{})["ownerReferences"] = []interface{}{map[string]interface{}{"kind": "Deployment", "name": "other", "uid": "other-uid"}}
	client := newWorkloadDynamicClient(deployment, ownedRevision, unownedRevision)
	var rollbackUpdate *unstructured.Unstructured
	client.PrependReactor("update", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		rollbackUpdate = action.(clienttesting.UpdateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		return true, rollbackUpdate, nil
	})
	revision := int64(1)
	_, err := performWorkloadAction(context.Background(), client, "apps", "v1", "deployments", "default", "api", WorkloadActionRequest{
		Action:                  "rollback",
		ExpectedResourceVersion: "7",
		Revision:                &revision,
		DryRun:                  true,
	})
	if err != nil {
		t.Fatalf("rollback action failed: %v", err)
	}
	containers, _, err := unstructured.NestedSlice(rollbackUpdate.Object, "spec", "template", "spec", "containers")
	if err != nil || len(containers) != 1 {
		t.Fatalf("rollback containers were not restored: %+v err=%v", containers, err)
	}
	image, _, err := unstructured.NestedString(containers[0].(map[string]interface{}), "image")
	if err != nil || image != "app:v1" {
		t.Fatalf("rollback template was not restored: %q err=%v", image, err)
	}
}

// TestPerformWorkloadActionRunsCronJobNow verifies one-off Job creation strips controller identity.
func TestPerformWorkloadActionRunsCronJobNow(t *testing.T) {
	cronJob := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata":   map[string]interface{}{"name": "nightly", "namespace": "default", "resourceVersion": "4"},
		"spec": map[string]interface{}{
			"schedule": "0 * * * *",
			"jobTemplate": map[string]interface{}{
				"metadata": map[string]interface{}{"labels": map[string]interface{}{"team": "platform"}},
				"spec":     map[string]interface{}{"template": map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "nightly"}}, "spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "worker", "image": "worker:v1"}}, "restartPolicy": "Never"}}},
			},
		},
	}}
	client := newWorkloadDynamicClient(cronJob)
	var created *unstructured.Unstructured
	client.PrependReactor("create", "jobs", func(action clienttesting.Action) (bool, runtime.Object, error) {
		created = action.(clienttesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		created.SetName("nightly-manual-12345")
		return true, created, nil
	})
	result, err := performWorkloadAction(context.Background(), client, "batch", "v1", "cronjobs", "default", "nightly", WorkloadActionRequest{
		Action:                  "run-now",
		ExpectedResourceVersion: "4",
		DryRun:                  true,
	})
	if err != nil {
		t.Fatalf("run-now action failed: %v", err)
	}
	if result.CreatedName != "nightly-manual-12345" || created == nil {
		t.Fatalf("unexpected one-off Job result: %+v object=%v", result, created)
	}
	if generated, _, _ := unstructured.NestedString(created.Object, "metadata", "generateName"); generated != "nightly-manual-" {
		t.Fatalf("unexpected generated Job prefix: %q", generated)
	}
	if namespace := created.GetNamespace(); namespace != "default" {
		t.Fatalf("unexpected generated Job namespace: %q", namespace)
	}
}

// TestDescribeWorkloadErrorsExplainsRBACAndUnsupportedAPIs verifies operator-facing native errors.
func TestDescribeWorkloadErrorsExplainsRBACAndUnsupportedAPIs(t *testing.T) {
	if message := describeWorkloadScaleError(apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api", nil)).Error(); !strings.Contains(message, "RBAC") {
		t.Fatalf("RBAC error was unclear: %s", message)
	}
	if message := describeWorkloadScaleError(apierrors.NewMethodNotSupported(schema.GroupResource{Group: "apps", Resource: "deployments"}, "scale")).Error(); !strings.Contains(message, "not supported") {
		t.Fatalf("unsupported scale error was unclear: %s", message)
	}
}
