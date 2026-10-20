package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

const (
	// pvcOperationFieldManager identifies native storage mutations made by KubeHaggis.
	pvcOperationFieldManager = "kube-haggis"
	// snapshotGroupVersion is the only VolumeSnapshot API version supported by this workflow.
	snapshotGroupVersion = "snapshot.storage.k8s.io/v1"
	// pvcActionResize identifies a PersistentVolumeClaim expansion request.
	pvcActionResize = "resize"
	// pvcActionSnapshot identifies a VolumeSnapshot creation request.
	pvcActionSnapshot = "snapshot"
	// storageCapabilityAvailable identifies a verified StorageClass expansion capability.
	storageCapabilityAvailable = "available"
	// storageCapabilityUnsupported identifies a storage setup that cannot be expanded safely.
	storageCapabilityUnsupported = "unsupported"
	// storageCapabilityPermissionDenied identifies a capability read blocked by Kubernetes RBAC.
	storageCapabilityPermissionDenied = "permission-denied"
	// storageCapabilityError identifies a capability read that failed for another reason.
	storageCapabilityError = "error"
	// snapshotCapabilityAvailable identifies a usable VolumeSnapshot API and class list.
	snapshotCapabilityAvailable = "available"
	// snapshotCapabilityEmpty identifies an installed API with no readable snapshot classes.
	snapshotCapabilityEmpty = "empty"
	// snapshotCapabilityUnavailable identifies an absent or incomplete snapshot API.
	snapshotCapabilityUnavailable = "unavailable"
	// snapshotCapabilityPermissionDenied identifies snapshot discovery or class reads blocked by RBAC.
	snapshotCapabilityPermissionDenied = "permission-denied"
	// snapshotCapabilityError identifies another snapshot discovery failure.
	snapshotCapabilityError = "error"
)

var (
	// persistentVolumeClaimGVR identifies core/v1 PersistentVolumeClaims.
	persistentVolumeClaimGVR = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	// storageClassGVR identifies storage.k8s.io/v1 StorageClasses.
	storageClassGVR = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
	// volumeSnapshotGVR identifies snapshot.storage.k8s.io/v1 VolumeSnapshots.
	volumeSnapshotGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}
	// volumeSnapshotClassGVR identifies cluster-scoped VolumeSnapshotClasses.
	volumeSnapshotClassGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
)

// PVCActionRequest describes one explicit resize or VolumeSnapshot operation.
type PVCActionRequest struct {
	Action                  string `json:"action"`
	DryRun                  bool   `json:"dryRun"`
	Confirmed               bool   `json:"confirmed"`
	ExpectedResourceVersion string `json:"expectedResourceVersion,omitempty"`
	Storage                 string `json:"storage,omitempty"`
	SnapshotName            string `json:"snapshotName,omitempty"`
	SnapshotClassName       string `json:"snapshotClassName,omitempty"`
}

// PVCActionResult reports the redacted outcome of one PVC storage operation.
type PVCActionResult struct {
	Action            string `json:"action"`
	Resource          string `json:"resource"`
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	DryRun            bool   `json:"dryRun"`
	ResourceVersion   string `json:"resourceVersion,omitempty"`
	SnapshotName      string `json:"snapshotName,omitempty"`
	SnapshotClassName string `json:"snapshotClassName,omitempty"`
	Status            string `json:"status"`
}

// PVCResizeCapability describes whether the PVC's StorageClass permits expansion.
type PVCResizeCapability struct {
	State                string `json:"state"`
	Message              string `json:"message,omitempty"`
	StorageClassName     string `json:"storageClassName,omitempty"`
	CurrentStorage       string `json:"currentStorage,omitempty"`
	AllowVolumeExpansion bool   `json:"allowVolumeExpansion"`
}

// VolumeSnapshotClass describes the safe, non-secret fields needed to select a snapshot class.
type VolumeSnapshotClass struct {
	Name           string `json:"name"`
	Driver         string `json:"driver,omitempty"`
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// VolumeSnapshotCapability describes native snapshot API availability and readable classes.
type VolumeSnapshotCapability struct {
	State           string                `json:"state"`
	Message         string                `json:"message,omitempty"`
	APIGroupVersion string                `json:"apiGroupVersion,omitempty"`
	Classes         []VolumeSnapshotClass `json:"classes"`
}

// PVCOperationInfo is the read-only capability model used by the PVC Operations UI.
type PVCOperationInfo struct {
	Name             string                   `json:"name"`
	Namespace        string                   `json:"namespace"`
	ResourceVersion  string                   `json:"resourceVersion"`
	RequestedStorage string                   `json:"requestedStorage,omitempty"`
	StorageClassName string                   `json:"storageClassName,omitempty"`
	Resize           PVCResizeCapability      `json:"resize"`
	Snapshot         VolumeSnapshotCapability `json:"snapshot"`
}

// PVCOperations returns storage expansion and VolumeSnapshot capabilities for one PVC.
func (s *Service) PVCOperations(ctx context.Context, contextName, namespace, name string) (PVCOperationInfo, error) {
	dynamicClient, discoveryClient, _, err := s.clients(contextName)
	if err != nil {
		return PVCOperationInfo{}, err
	}
	return pvcOperationInfo(ctx, dynamicClient, discoveryClient, namespace, name)
}

// PVCAction applies one validated native resize or VolumeSnapshot operation to a PVC.
func (s *Service) PVCAction(ctx context.Context, contextName, namespace, name string, request PVCActionRequest) (PVCActionResult, error) {
	request = normalizePVCActionRequest(request)
	if err := validatePVCAction(namespace, name, request); err != nil {
		return PVCActionResult{}, err
	}
	dynamicClient, discoveryClient, _, err := s.clients(contextName)
	if err != nil {
		return PVCActionResult{}, err
	}
	return performPVCAction(ctx, dynamicClient, discoveryClient, namespace, name, request)
}

// validatePVCCoordinates checks the namespaced coordinates shared by read and write paths.
func validatePVCCoordinates(namespace, name string) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || namespace == "all" {
		return fmt.Errorf("namespace is required for PVC operations")
	}
	if errors := validation.IsDNS1123Label(namespace); len(errors) > 0 {
		return fmt.Errorf("PVC namespace is invalid: %s", strings.Join(errors, "; "))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("PVC name is required")
	}
	if errors := validation.IsDNS1123Subdomain(name); len(errors) > 0 {
		return fmt.Errorf("PVC name is invalid: %s", strings.Join(errors, "; "))
	}
	return nil
}

// validatePVCAction checks operation fields before any Kubernetes client is created.
func validatePVCAction(namespace, name string, request PVCActionRequest) error {
	if err := validatePVCCoordinates(namespace, name); err != nil {
		return err
	}
	if request.ExpectedResourceVersion == "" {
		return fmt.Errorf("expected resource version is required for PVC operations")
	}
	if !request.DryRun && !request.Confirmed {
		return fmt.Errorf("explicit confirmation is required before applying a PVC operation")
	}
	switch request.Action {
	case pvcActionResize:
		if strings.TrimSpace(request.Storage) == "" {
			return fmt.Errorf("target storage size is required for PVC resize")
		}
		quantity, err := resource.ParseQuantity(request.Storage)
		if err != nil || quantity.Sign() <= 0 {
			return fmt.Errorf("target storage size must be a positive Kubernetes quantity")
		}
	case pvcActionSnapshot:
		if err := validateSnapshotName("snapshot name", request.SnapshotName); err != nil {
			return err
		}
		if err := validateSnapshotName("VolumeSnapshotClass name", request.SnapshotClassName); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported PVC operation %q", request.Action)
	}
	return nil
}

// validateSnapshotName validates a required Kubernetes object name used by snapshot creation.
func validateSnapshotName(label, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s is required", label)
	}
	if errors := validation.IsDNS1123Subdomain(name); len(errors) > 0 {
		return fmt.Errorf("%s is invalid: %s", label, strings.Join(errors, "; "))
	}
	return nil
}

// normalizePVCActionRequest trims operator-entered PVC operation fields before validation.
func normalizePVCActionRequest(request PVCActionRequest) PVCActionRequest {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ExpectedResourceVersion = strings.TrimSpace(request.ExpectedResourceVersion)
	request.Storage = strings.TrimSpace(request.Storage)
	request.SnapshotName = strings.TrimSpace(request.SnapshotName)
	request.SnapshotClassName = strings.TrimSpace(request.SnapshotClassName)
	return request
}

// pvcOperationInfo reads the PVC and returns capability states without hiding snapshot API absence.
func pvcOperationInfo(ctx context.Context, client dynamic.Interface, discoveryClient discovery.DiscoveryInterface, namespace, name string) (PVCOperationInfo, error) {
	if err := validatePVCCoordinates(namespace, name); err != nil {
		return PVCOperationInfo{}, err
	}
	pvc, err := client.Resource(persistentVolumeClaimGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PVCOperationInfo{}, describePVCError("read PVC", err)
	}
	requestedStorage, err := pvcRequestedStorage(pvc)
	if err != nil {
		return PVCOperationInfo{}, err
	}
	storageClassName, err := pvcStorageClassName(pvc)
	if err != nil {
		return PVCOperationInfo{}, err
	}
	info := PVCOperationInfo{
		Name:             pvc.GetName(),
		Namespace:        pvc.GetNamespace(),
		ResourceVersion:  pvc.GetResourceVersion(),
		RequestedStorage: requestedStorage,
		StorageClassName: storageClassName,
		Resize: PVCResizeCapability{
			State:            storageCapabilityUnsupported,
			StorageClassName: storageClassName,
			CurrentStorage:   requestedStorage,
			Message:          "PVC expansion capability has not been verified.",
		},
	}
	resize, resizeErr := pvcResizeCapability(ctx, client, requestedStorage, storageClassName)
	if resizeErr != nil {
		info.Resize = PVCResizeCapability{
			State:            capabilityErrorState(resizeErr),
			StorageClassName: storageClassName,
			CurrentStorage:   requestedStorage,
			Message:          capabilityErrorMessage(resizeErr),
		}
	} else {
		info.Resize = resize
	}
	snapshot, snapshotErr := snapshotCapabilities(ctx, client, discoveryClient)
	if snapshotErr != nil && snapshot.Message == "" {
		snapshot.Message = capabilityErrorMessage(snapshotErr)
	}
	info.Snapshot = snapshot
	return info, nil
}

// pvcResizeCapability evaluates the StorageClass flag required for safe volume expansion.
func pvcResizeCapability(ctx context.Context, client dynamic.Interface, requestedStorage, storageClassName string) (PVCResizeCapability, error) {
	capability := PVCResizeCapability{
		State:            storageCapabilityUnsupported,
		StorageClassName: storageClassName,
		CurrentStorage:   requestedStorage,
	}
	if storageClassName == "" {
		capability.Message = "This PVC has no StorageClass; Kubernetes cannot verify volume expansion support."
		return capability, nil
	}
	storageClass, err := client.Resource(storageClassGVR).Get(ctx, storageClassName, metav1.GetOptions{})
	if err != nil {
		return capability, describePVCError("read StorageClass", err)
	}
	allowExpansion, found, err := unstructured.NestedBool(storageClass.Object, "allowVolumeExpansion")
	if err != nil {
		return capability, fmt.Errorf("read StorageClass expansion capability: %w", err)
	}
	if !found || !allowExpansion {
		capability.Message = fmt.Sprintf("StorageClass %q does not allow volume expansion.", storageClassName)
		return capability, nil
	}
	capability.State = storageCapabilityAvailable
	capability.AllowVolumeExpansion = true
	capability.Message = fmt.Sprintf("StorageClass %q allows volume expansion.", storageClassName)
	return capability, nil
}

// pvcStorageClassName returns the PVC storageClassName without reading any sensitive data.
func pvcStorageClassName(pvc *unstructured.Unstructured) (string, error) {
	value, found, err := unstructured.NestedString(pvc.Object, "spec", "storageClassName")
	if err != nil {
		return "", fmt.Errorf("read PVC StorageClass name: %w", err)
	}
	if !found {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}

// pvcRequestedStorage returns the current requested storage quantity from a PVC spec.
func pvcRequestedStorage(pvc *unstructured.Unstructured) (string, error) {
	value, found, err := unstructured.NestedString(pvc.Object, "spec", "resources", "requests", "storage")
	if err != nil {
		return "", fmt.Errorf("read PVC storage request: %w", err)
	}
	if !found || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("PVC storage request is missing")
	}
	if _, err := resource.ParseQuantity(value); err != nil {
		return "", fmt.Errorf("PVC storage request is invalid: %w", err)
	}
	return value, nil
}

// snapshotCapabilities discovers the v1 snapshot API and lists safe class summaries.
func snapshotCapabilities(ctx context.Context, client dynamic.Interface, discoveryClient discovery.DiscoveryInterface) (VolumeSnapshotCapability, error) {
	capability := VolumeSnapshotCapability{
		State:           snapshotCapabilityUnavailable,
		APIGroupVersion: snapshotGroupVersion,
		Classes:         []VolumeSnapshotClass{},
		Message:         "The VolumeSnapshot API is not available on this Kubernetes API server.",
	}
	resources, err := discoveryClient.ServerResourcesForGroupVersion(snapshotGroupVersion)
	if err != nil {
		capability.State = snapshotErrorState(err)
		capability.Message = snapshotErrorMessage(err)
		return capability, describeSnapshotDiscoveryError(err)
	}
	volumeSnapshots, foundSnapshots := discoveredAPIResource(resources, "volumesnapshots")
	volumeSnapshotClasses, foundClasses := discoveredAPIResource(resources, "volumesnapshotclasses")
	if !foundSnapshots || !foundClasses || !volumeSnapshots.Namespaced || volumeSnapshotClasses.Namespaced || !hasVerb(volumeSnapshots.Verbs, "create") || !hasVerb(volumeSnapshotClasses.Verbs, "list") {
		capability.Message = "The VolumeSnapshot API is present but does not advertise the required VolumeSnapshot and VolumeSnapshotClass operations."
		return capability, fmt.Errorf("VolumeSnapshot API is not available on this Kubernetes API server: required v1 resources or verbs are missing")
	}
	items, err := client.Resource(volumeSnapshotClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		capability.State = snapshotErrorState(err)
		capability.Message = snapshotErrorMessage(err)
		return capability, describeSnapshotClassError(err)
	}
	capability.Classes = make([]VolumeSnapshotClass, 0, len(items.Items))
	for index := range items.Items {
		item := &items.Items[index]
		driver, _, driverErr := unstructured.NestedString(item.Object, "driver")
		if driverErr != nil {
			return capability, fmt.Errorf("read VolumeSnapshotClass driver: %w", driverErr)
		}
		deletionPolicy, _, policyErr := unstructured.NestedString(item.Object, "deletionPolicy")
		if policyErr != nil {
			return capability, fmt.Errorf("read VolumeSnapshotClass deletion policy: %w", policyErr)
		}
		capability.Classes = append(capability.Classes, VolumeSnapshotClass{Name: item.GetName(), Driver: driver, DeletionPolicy: deletionPolicy})
	}
	sort.Slice(capability.Classes, func(left, right int) bool { return capability.Classes[left].Name < capability.Classes[right].Name })
	if len(capability.Classes) == 0 {
		capability.State = snapshotCapabilityEmpty
		capability.Message = "The VolumeSnapshot API is available, but no readable VolumeSnapshotClass is configured."
		return capability, nil
	}
	capability.State = snapshotCapabilityAvailable
	capability.Message = "The native VolumeSnapshot API and at least one VolumeSnapshotClass are available."
	return capability, nil
}

// discoveredAPIResource returns one named resource from an API discovery response.
func discoveredAPIResource(resources *metav1.APIResourceList, name string) (metav1.APIResource, bool) {
	for _, resource := range resources.APIResources {
		if resource.Name == name {
			return resource, true
		}
	}
	return metav1.APIResource{}, false
}

// hasVerb reports whether discovery advertised one operation for an API resource.
func hasVerb(verbs []string, wanted string) bool {
	for _, verb := range verbs {
		if verb == wanted {
			return true
		}
	}
	return false
}

// performPVCAction executes one validated operation through a dynamic Kubernetes client.
func performPVCAction(ctx context.Context, client dynamic.Interface, discoveryClient discovery.DiscoveryInterface, namespace, name string, request PVCActionRequest) (PVCActionResult, error) {
	request = normalizePVCActionRequest(request)
	if err := validatePVCAction(namespace, name, request); err != nil {
		return PVCActionResult{}, err
	}
	pvcClient := client.Resource(persistentVolumeClaimGVR).Namespace(namespace)
	pvc, err := pvcClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PVCActionResult{}, describePVCError("read PVC", err)
	}
	if request.ExpectedResourceVersion != pvc.GetResourceVersion() {
		return PVCActionResult{}, fmt.Errorf("PVC resource version conflict: expected %q, current %q; refresh before retrying", request.ExpectedResourceVersion, pvc.GetResourceVersion())
	}
	result := PVCActionResult{Action: request.Action, Resource: "persistentvolumeclaims", Namespace: namespace, Name: name, DryRun: request.DryRun}
	switch request.Action {
	case pvcActionResize:
		updated, updateErr := resizePVC(ctx, client, pvcClient, pvc, request)
		if updateErr != nil {
			return PVCActionResult{}, updateErr
		}
		if updated != nil {
			result.ResourceVersion = updated.GetResourceVersion()
		}
	case pvcActionSnapshot:
		created, createErr := createPVCVolumeSnapshot(ctx, client, discoveryClient, pvc, request)
		if createErr != nil {
			return PVCActionResult{}, createErr
		}
		result.SnapshotName = request.SnapshotName
		result.SnapshotClassName = request.SnapshotClassName
		if created != nil {
			result.ResourceVersion = created.GetResourceVersion()
		}
	}
	result.Status = pvcDryRunStatus(request.DryRun)
	return result, nil
}

// resizePVC validates expansion capability and updates only the PVC storage request.
func resizePVC(ctx context.Context, client dynamic.Interface, pvcClient dynamic.ResourceInterface, pvc *unstructured.Unstructured, request PVCActionRequest) (*unstructured.Unstructured, error) {
	requestedStorage, err := pvcRequestedStorage(pvc)
	if err != nil {
		return nil, err
	}
	storageClassName, err := pvcStorageClassName(pvc)
	if err != nil {
		return nil, err
	}
	capability, err := pvcResizeCapability(ctx, client, requestedStorage, storageClassName)
	if err != nil {
		return nil, err
	}
	if capability.State != storageCapabilityAvailable {
		return nil, fmt.Errorf("PVC resize is unsupported: %s", capability.Message)
	}
	currentQuantity, err := resource.ParseQuantity(requestedStorage)
	if err != nil {
		return nil, fmt.Errorf("parse current PVC storage request: %w", err)
	}
	targetQuantity, err := resource.ParseQuantity(request.Storage)
	if err != nil {
		return nil, fmt.Errorf("parse target PVC storage request: %w", err)
	}
	if targetQuantity.Cmp(currentQuantity) <= 0 {
		return nil, fmt.Errorf("target storage must be greater than the current PVC request of %s", requestedStorage)
	}
	candidate := pvc.DeepCopy()
	unstructured.RemoveNestedField(candidate.Object, "metadata", "managedFields")
	if err := unstructured.SetNestedField(candidate.Object, targetQuantity.String(), "spec", "resources", "requests", "storage"); err != nil {
		return nil, fmt.Errorf("set PVC storage request: %w", err)
	}
	dryRun := []string(nil)
	if request.DryRun {
		dryRun = []string{metav1.DryRunAll}
	}
	updated, err := pvcClient.Update(ctx, candidate, metav1.UpdateOptions{DryRun: dryRun, FieldManager: pvcOperationFieldManager})
	if err != nil {
		return nil, describePVCError("resize PVC", err)
	}
	return updated, nil
}

// createPVCVolumeSnapshot validates discovery and creates a PVC-backed VolumeSnapshot.
func createPVCVolumeSnapshot(ctx context.Context, client dynamic.Interface, discoveryClient discovery.DiscoveryInterface, pvc *unstructured.Unstructured, request PVCActionRequest) (*unstructured.Unstructured, error) {
	capability, err := snapshotCapabilities(ctx, client, discoveryClient)
	if err != nil {
		return nil, err
	}
	if capability.State != snapshotCapabilityAvailable {
		return nil, fmt.Errorf("VolumeSnapshot API is unavailable: %s", capability.Message)
	}
	classFound := false
	for _, snapshotClass := range capability.Classes {
		if snapshotClass.Name == request.SnapshotClassName {
			classFound = true
			break
		}
	}
	if !classFound {
		return nil, fmt.Errorf("VolumeSnapshotClass %q was not found or is not readable", request.SnapshotClassName)
	}
	snapshot := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": snapshotGroupVersion,
		"kind":       "VolumeSnapshot",
		"metadata": map[string]interface{}{
			"name":      request.SnapshotName,
			"namespace": pvc.GetNamespace(),
		},
		"spec": map[string]interface{}{
			"volumeSnapshotClassName": request.SnapshotClassName,
			"source": map[string]interface{}{
				"persistentVolumeClaimName": pvc.GetName(),
			},
		},
	}}
	dryRun := []string(nil)
	if request.DryRun {
		dryRun = []string{metav1.DryRunAll}
	}
	created, err := client.Resource(volumeSnapshotGVR).Namespace(pvc.GetNamespace()).Create(ctx, snapshot, metav1.CreateOptions{DryRun: dryRun, FieldManager: pvcOperationFieldManager})
	if err != nil {
		return nil, describePVCError("create VolumeSnapshot", err)
	}
	return created, nil
}

// pvcDryRunStatus returns the stable result status shared by resize and snapshot operations.
func pvcDryRunStatus(dryRun bool) string {
	if dryRun {
		return "dry-run passed"
	}
	return "applied"
}

// describePVCError translates native API errors into safe operator-facing storage messages.
func describePVCError(operation string, err error) error {
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return fmt.Errorf("%s denied by Kubernetes RBAC", operation)
	case apierrors.IsConflict(err):
		return fmt.Errorf("%s conflicted with a newer resource version; refresh before retrying", operation)
	case apierrors.IsMethodNotSupported(err):
		return fmt.Errorf("%s is not supported by this Kubernetes API server", operation)
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%s failed because the Kubernetes resource was not found", operation)
	default:
		return fmt.Errorf("%s failed: %w", operation, err)
	}
}

// describeSnapshotDiscoveryError classifies snapshot API discovery failures without exposing request data.
func describeSnapshotDiscoveryError(err error) error {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return fmt.Errorf("VolumeSnapshot API access denied by Kubernetes RBAC during discovery")
	}
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("VolumeSnapshot API is not available on this Kubernetes API server")
	}
	return fmt.Errorf("VolumeSnapshot API discovery failed: %w", err)
}

// describeSnapshotClassError classifies snapshot class list failures for UI capability reporting.
func describeSnapshotClassError(err error) error {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return fmt.Errorf("VolumeSnapshotClass access denied by Kubernetes RBAC")
	}
	if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) {
		return fmt.Errorf("VolumeSnapshot API is not available on this Kubernetes API server")
	}
	return fmt.Errorf("read VolumeSnapshotClasses failed: %w", err)
}

// snapshotErrorState maps an API error to an explicit browser capability state.
func snapshotErrorState(err error) string {
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return snapshotCapabilityPermissionDenied
	case apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) || strings.Contains(strings.ToLower(err.Error()), "not available"):
		return snapshotCapabilityUnavailable
	default:
		return snapshotCapabilityError
	}
}

// snapshotErrorMessage converts a native snapshot error into safe capability copy.
func snapshotErrorMessage(err error) string {
	switch snapshotErrorState(err) {
	case snapshotCapabilityPermissionDenied:
		return "VolumeSnapshot API access is denied by Kubernetes RBAC."
	case snapshotCapabilityUnavailable:
		return "The VolumeSnapshot API is not available on this Kubernetes API server; snapshot CRDs may not be installed."
	default:
		return "The VolumeSnapshot API could not be checked because the Kubernetes API returned an error."
	}
}

// capabilityErrorState maps a StorageClass read error to an explicit resize state.
func capabilityErrorState(err error) string {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) || strings.Contains(strings.ToLower(err.Error()), "rbac") {
		return storageCapabilityPermissionDenied
	}
	return storageCapabilityError
}

// capabilityErrorMessage converts a capability error into a concise browser-safe message.
func capabilityErrorMessage(err error) string {
	if capabilityErrorState(err) == storageCapabilityPermissionDenied {
		return "StorageClass expansion capability is denied by Kubernetes RBAC."
	}
	return "StorageClass expansion capability could not be checked because the Kubernetes API returned an error."
}
