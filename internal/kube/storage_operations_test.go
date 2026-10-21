package kube

import (
	"context"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// newStorageDynamicClient creates a dynamic fake with list kinds for storage APIs.
func newStorageDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	listKinds := map[schema.GroupVersionResource]string{
		storageClassGVR:          "StorageClassList",
		volumeSnapshotClassGVR:   "VolumeSnapshotClassList",
		volumeSnapshotGVR:        "VolumeSnapshotList",
		persistentVolumeClaimGVR: "PersistentVolumeClaimList",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
}

// storagePVC returns a resource-versioned PVC with a StorageClass and a requested size.
func storagePVC(resourceVersion, storage string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]interface{}{
			"name":            "data",
			"namespace":       "default",
			"resourceVersion": resourceVersion,
			"managedFields":   []interface{}{map[string]interface{}{"manager": "controller"}},
		},
		"spec": map[string]interface{}{
			"storageClassName": "fast",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{"storage": storage},
			},
		},
		"status": map[string]interface{}{
			"capacity": map[string]interface{}{"storage": storage},
		},
	}}
}

// expansionStorageClass returns a StorageClass that explicitly permits volume expansion.
func expansionStorageClass(allow bool) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion":           "storage.k8s.io/v1",
		"kind":                 "StorageClass",
		"metadata":             map[string]interface{}{"name": "fast"},
		"provisioner":          "example.test/provisioner",
		"allowVolumeExpansion": allow,
	}}
}

// snapshotClass returns a non-secret VolumeSnapshotClass summary object.
func snapshotClass(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion":     snapshotGroupVersion,
		"kind":           "VolumeSnapshotClass",
		"metadata":       map[string]interface{}{"name": name},
		"driver":         "example.test/provisioner",
		"deletionPolicy": "Delete",
	}}
}

// snapshotDiscovery returns a fake discovery client with the required v1 resources.
func snapshotDiscovery() *fake.FakeDiscovery {
	return &fake.FakeDiscovery{Fake: &clienttesting.Fake{Resources: []*metav1.APIResourceList{{
		GroupVersion: snapshotGroupVersion,
		APIResources: []metav1.APIResource{
			{Name: "volumesnapshots", Namespaced: true, Verbs: []string{"get", "list", "create"}},
			{Name: "volumesnapshotclasses", Namespaced: false, Verbs: []string{"get", "list"}},
		},
	}}}}
}

// storageActionRecorder captures dynamic mutation options that the Kubernetes fake client omits.
type storageActionRecorder struct {
	updateOptions metav1.UpdateOptions
	createOptions metav1.CreateOptions
}

// recordingDynamicClient preserves mutation options while delegating all Kubernetes behavior to a fake client.
type recordingDynamicClient struct {
	dynamic.Interface
	recorder *storageActionRecorder
}

// Resource wraps each dynamic resource so namespaced mutations can be recorded.
func (c *recordingDynamicClient) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &recordingNamespaceableResource{
		NamespaceableResourceInterface: c.Interface.Resource(gvr),
		recorder:                       c.recorder,
	}
}

// recordingNamespaceableResource preserves namespace selection while wrapping mutations.
type recordingNamespaceableResource struct {
	dynamic.NamespaceableResourceInterface
	recorder *storageActionRecorder
}

// Namespace returns a mutation-recording resource client for one namespace.
func (r *recordingNamespaceableResource) Namespace(namespace string) dynamic.ResourceInterface {
	return &recordingResourceInterface{
		ResourceInterface: r.NamespaceableResourceInterface.Namespace(namespace),
		recorder:          r.recorder,
	}
}

// recordingResourceInterface captures create and update options before delegation.
type recordingResourceInterface struct {
	dynamic.ResourceInterface
	recorder *storageActionRecorder
}

// Create records server-side dry-run options used for native creates.
func (r *recordingResourceInterface) Create(ctx context.Context, object *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	r.recorder.createOptions = options
	return r.ResourceInterface.Create(ctx, object, options, subresources...)
}

// Update records server-side dry-run options used for native updates.
func (r *recordingResourceInterface) Update(ctx context.Context, object *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	r.recorder.updateOptions = options
	return r.ResourceInterface.Update(ctx, object, options, subresources...)
}

// TestValidatePVCActionRejectsUnsafeInputs verifies local storage operation validation.
func TestValidatePVCActionRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name    string
		request PVCActionRequest
		want    string
	}{
		{name: "missing namespace", request: PVCActionRequest{Action: pvcActionResize, ExpectedResourceVersion: "7", Storage: "20Gi", DryRun: true}, want: "namespace is required"},
		{name: "missing resource version", request: PVCActionRequest{Action: pvcActionResize, Storage: "20Gi", DryRun: true}, want: "expected resource version"},
		{name: "missing confirmation", request: PVCActionRequest{Action: pvcActionResize, ExpectedResourceVersion: "7", Storage: "20Gi"}, want: "confirmation"},
		{name: "invalid target size", request: PVCActionRequest{Action: pvcActionResize, ExpectedResourceVersion: "7", Storage: "not-a-quantity", DryRun: true}, want: "positive Kubernetes quantity"},
		{name: "missing snapshot class", request: PVCActionRequest{Action: pvcActionSnapshot, ExpectedResourceVersion: "7", SnapshotName: "data-copy", DryRun: true}, want: "VolumeSnapshotClass name is required"},
		{name: "invalid snapshot name", request: PVCActionRequest{Action: pvcActionSnapshot, ExpectedResourceVersion: "7", SnapshotName: "not valid", SnapshotClassName: "csi-class", DryRun: true}, want: "snapshot name is invalid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePVCAction(testNamespaceForStorageTest(test.name), "data", normalizePVCActionRequest(test.request))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// testNamespaceForStorageTest supplies an invalid namespace only to the relevant validation case.
func testNamespaceForStorageTest(name string) string {
	if name == "missing namespace" {
		return ""
	}
	return "default"
}

// TestPerformPVCResizeUsesStorageClassAndServerDryRun verifies versioned full-object expansion updates.
func TestPerformPVCResizeUsesStorageClassAndServerDryRun(t *testing.T) {
	client := newStorageDynamicClient(storagePVC("7", "10Gi"), expansionStorageClass(true))
	var captured *unstructured.Unstructured
	client.PrependReactor("update", "persistentvolumeclaims", func(action clienttesting.Action) (bool, runtime.Object, error) {
		captured = action.(clienttesting.UpdateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		return true, captured, nil
	})
	recorder := &storageActionRecorder{}
	recordingClient := &recordingDynamicClient{Interface: client, recorder: recorder}

	result, err := performPVCAction(context.Background(), recordingClient, snapshotDiscovery(), "default", "data", PVCActionRequest{
		Action:                  pvcActionResize,
		DryRun:                  true,
		ExpectedResourceVersion: "7",
		Storage:                 "20Gi",
	})
	if err != nil {
		t.Fatalf("PVC resize dry-run failed: %v", err)
	}
	if result.Status != "dry-run passed" || result.ResourceVersion != "7" || captured == nil {
		t.Fatalf("unexpected resize result: %+v object=%v", result, captured)
	}
	if len(recorder.updateOptions.DryRun) != 1 || recorder.updateOptions.DryRun[0] != metav1.DryRunAll {
		t.Fatalf("PVC update was not server-side dry-run: %v", recorder.updateOptions.DryRun)
	}
	if captured.GetResourceVersion() != "7" {
		t.Fatalf("resource version was not preserved: %q", captured.GetResourceVersion())
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(captured.Object, "metadata", "managedFields"); found {
		t.Fatal("managedFields were sent in PVC update")
	}
	storage, _, err := unstructured.NestedString(captured.Object, "spec", "resources", "requests", "storage")
	if err != nil || storage != "20Gi" {
		t.Fatalf("unexpected resized storage request: %q err=%v", storage, err)
	}
}

// TestPerformPVCResizeRejectsStaleResourceVersion verifies stale UI state never reaches Update.
func TestPerformPVCResizeRejectsStaleResourceVersion(t *testing.T) {
	client := newStorageDynamicClient(storagePVC("8", "10Gi"), expansionStorageClass(true))
	updates := 0
	client.PrependReactor("update", "persistentvolumeclaims", func(clienttesting.Action) (bool, runtime.Object, error) {
		updates++
		return true, nil, nil
	})

	_, err := performPVCAction(context.Background(), client, snapshotDiscovery(), "default", "data", PVCActionRequest{
		Action:                  pvcActionResize,
		ExpectedResourceVersion: "7",
		Storage:                 "20Gi",
		Confirmed:               true,
	})
	if err == nil || !strings.Contains(err.Error(), "resource version conflict") {
		t.Fatalf("expected resource-version conflict, got %v", err)
	}
	if updates != 0 {
		t.Fatalf("stale PVC resize attempted an update: %d", updates)
	}
}

// TestPerformPVCResizeRejectsStorageClassWithoutExpansion verifies StorageClass capability gating.
func TestPerformPVCResizeRejectsStorageClassWithoutExpansion(t *testing.T) {
	client := newStorageDynamicClient(storagePVC("7", "10Gi"), expansionStorageClass(false))
	updates := 0
	client.PrependReactor("update", "persistentvolumeclaims", func(clienttesting.Action) (bool, runtime.Object, error) {
		updates++
		return true, nil, nil
	})

	_, err := performPVCAction(context.Background(), client, snapshotDiscovery(), "default", "data", PVCActionRequest{
		Action:                  pvcActionResize,
		ExpectedResourceVersion: "7",
		Storage:                 "20Gi",
		Confirmed:               true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not allow volume expansion") {
		t.Fatalf("expected expansion capability error, got %v", err)
	}
	if updates != 0 {
		t.Fatalf("unsupported PVC resize attempted an update: %d", updates)
	}
}

// TestPerformPVCSnapshotUsesDiscoveredAPIAndServerDryRun verifies safe native snapshot creation.
func TestPerformPVCSnapshotUsesDiscoveredAPIAndServerDryRun(t *testing.T) {
	client := newStorageDynamicClient(storagePVC("7", "10Gi"), expansionStorageClass(true), snapshotClass("csi-class"))
	var captured *unstructured.Unstructured
	client.PrependReactor("create", "volumesnapshots", func(action clienttesting.Action) (bool, runtime.Object, error) {
		captured = action.(clienttesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		captured.SetResourceVersion("snapshot-rv")
		return true, captured, nil
	})
	recorder := &storageActionRecorder{}
	recordingClient := &recordingDynamicClient{Interface: client, recorder: recorder}

	result, err := performPVCAction(context.Background(), recordingClient, snapshotDiscovery(), "default", "data", PVCActionRequest{
		Action:                  pvcActionSnapshot,
		DryRun:                  true,
		ExpectedResourceVersion: "7",
		SnapshotName:            "data-copy",
		SnapshotClassName:       "csi-class",
	})
	if err != nil {
		t.Fatalf("VolumeSnapshot dry-run failed: %v", err)
	}
	if result.Status != "dry-run passed" || result.ResourceVersion != "snapshot-rv" || result.SnapshotName != "data-copy" || captured == nil {
		t.Fatalf("unexpected snapshot result: %+v object=%v", result, captured)
	}
	if len(recorder.createOptions.DryRun) != 1 || recorder.createOptions.DryRun[0] != metav1.DryRunAll {
		t.Fatalf("VolumeSnapshot create was not server-side dry-run: %v", recorder.createOptions.DryRun)
	}
	className, _, err := unstructured.NestedString(captured.Object, "spec", "volumeSnapshotClassName")
	if err != nil || className != "csi-class" {
		t.Fatalf("unexpected snapshot class: %q err=%v", className, err)
	}
	sourceName, _, err := unstructured.NestedString(captured.Object, "spec", "source", "persistentVolumeClaimName")
	if err != nil || sourceName != "data" {
		t.Fatalf("unexpected snapshot source: %q err=%v", sourceName, err)
	}
}

// TestSnapshotCapabilitiesClassifiesAbsentAPI verifies missing snapshot CRDs remain explicit and non-fatal to capability reads.
func TestSnapshotCapabilitiesClassifiesAbsentAPI(t *testing.T) {
	client := newStorageDynamicClient()
	capability, err := snapshotCapabilities(context.Background(), client, &fake.FakeDiscovery{Fake: &clienttesting.Fake{}})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected absent snapshot API error, got %v", err)
	}
	if capability.State != snapshotCapabilityUnavailable || !strings.Contains(capability.Message, "CRDs") {
		t.Fatalf("unexpected absent snapshot capability: %+v", capability)
	}
}

// TestSnapshotCapabilitiesClassifiesPermissionDenied verifies discovery RBAC failures are visible to the UI.
func TestSnapshotCapabilitiesClassifiesPermissionDenied(t *testing.T) {
	discoveryClient := snapshotDiscovery()
	discoveryClient.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "snapshot.storage.k8s.io", Resource: "resource"}, "resource", nil)
	})
	capability, err := snapshotCapabilities(context.Background(), newStorageDynamicClient(), discoveryClient)
	if err == nil || !strings.Contains(err.Error(), "RBAC") {
		t.Fatalf("expected snapshot RBAC error, got %v", err)
	}
	if capability.State != snapshotCapabilityPermissionDenied {
		t.Fatalf("unexpected permission capability: %+v", capability)
	}
}

// TestPVCOperationInfoPreservesUnavailableSnapshotState verifies capability reporting still succeeds without snapshot CRDs.
func TestPVCOperationInfoPreservesUnavailableSnapshotState(t *testing.T) {
	info, err := pvcOperationInfo(context.Background(), newStorageDynamicClient(storagePVC("7", "10Gi"), expansionStorageClass(true)), &fake.FakeDiscovery{Fake: &clienttesting.Fake{}}, "default", "data")
	if err != nil {
		t.Fatalf("PVC capability read failed: %v", err)
	}
	if info.Resize.State != storageCapabilityAvailable || info.Snapshot.State != snapshotCapabilityUnavailable {
		t.Fatalf("unexpected PVC capability info: %+v", info)
	}
}

// TestDescribePVCErrorClassifiesRBACAndUnsupported verifies safe operator-facing native errors.
func TestDescribePVCErrorClassifiesRBACAndUnsupported(t *testing.T) {
	if message := describePVCError("resize PVC", apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, "data", nil)).Error(); !strings.Contains(message, "RBAC") {
		t.Fatalf("RBAC error was unclear: %s", message)
	}
	if message := describePVCError("create VolumeSnapshot", apierrors.NewMethodNotSupported(schema.GroupResource{Group: "snapshot.storage.k8s.io", Resource: "volumesnapshots"}, "create")).Error(); !strings.Contains(message, "not supported") {
		t.Fatalf("unsupported snapshot error was unclear: %s", message)
	}
}
