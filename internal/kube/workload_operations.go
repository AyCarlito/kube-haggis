package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

const (
	workloadRestartAnnotation = "kubectl.kubernetes.io/restartedAt"
	workloadFieldManager      = "kube-haggis"
)

// WorkloadActionRequest describes one explicit mutation of a supported workload.
type WorkloadActionRequest struct {
	Action                  string `json:"action"`
	DryRun                  bool   `json:"dryRun"`
	ExpectedResourceVersion string `json:"expectedResourceVersion,omitempty"`
	Replicas                *int64 `json:"replicas,omitempty"`
	Container               string `json:"container,omitempty"`
	Image                   string `json:"image,omitempty"`
	Revision                *int64 `json:"revision,omitempty"`
	Schedule                string `json:"schedule,omitempty"`
}

// WorkloadActionResult reports the redacted outcome of one workload action.
type WorkloadActionResult struct {
	Action          string `json:"action"`
	Resource        string `json:"resource"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	DryRun          bool   `json:"dryRun"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
	CreatedName     string `json:"createdName,omitempty"`
	Status          string `json:"status"`
}

// WorkloadAction applies a targeted native operation to a supported workload.
func (s *Service) WorkloadAction(ctx context.Context, contextName, group, version, resource, namespace, name string, request WorkloadActionRequest) (WorkloadActionResult, error) {
	request = normalizeWorkloadActionRequest(request)
	if err := validateWorkloadAction(group, version, resource, namespace, name, request); err != nil {
		return WorkloadActionResult{}, err
	}
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return WorkloadActionResult{}, err
	}
	return performWorkloadAction(ctx, dynamicClient, group, version, resource, namespace, name, request)
}

// validateWorkloadAction checks workload coordinates and action fields before client creation.
func validateWorkloadAction(group, version, resource, namespace, name string, request WorkloadActionRequest) error {
	if version != "v1" || (group != "apps" && group != "batch") {
		return fmt.Errorf("workload actions require an apps/v1 or batch/v1 workload")
	}
	if !isSupportedWorkloadResource(group, version, resource) {
		return fmt.Errorf("resource %s/%s/%s is not a supported workload", group, version, resource)
	}
	if strings.TrimSpace(namespace) == "" || namespace == "all" {
		return fmt.Errorf("namespace is required for workload actions")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("workload name is required")
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	if !isSupportedWorkloadAction(resource, request.Action) {
		return fmt.Errorf("unsupported workload action %q for %s", request.Action, resource)
	}
	if request.ExpectedResourceVersion != "" && strings.TrimSpace(request.ExpectedResourceVersion) == "" {
		return fmt.Errorf("expected resource version cannot be blank")
	}
	switch request.Action {
	case "scale":
		if request.Replicas == nil {
			return fmt.Errorf("replica count is required for scale")
		}
		if *request.Replicas < 0 {
			return fmt.Errorf("replica count cannot be negative")
		}
		if *request.Replicas > int64(1<<31-1) {
			return fmt.Errorf("replica count exceeds Kubernetes limits")
		}
	case "image":
		if strings.TrimSpace(request.Container) == "" {
			return fmt.Errorf("container is required for image changes")
		}
		if strings.TrimSpace(request.Image) == "" {
			return fmt.Errorf("image is required for image changes")
		}
		if strings.ContainsAny(request.Image, "\x00\r\n\t") {
			return fmt.Errorf("image contains invalid control characters")
		}
	case "rollback":
		if request.Revision != nil && *request.Revision < 0 {
			return fmt.Errorf("rollback revision cannot be negative")
		}
	case "schedule":
		if strings.TrimSpace(request.Schedule) == "" {
			return fmt.Errorf("schedule is required for CronJob scheduling")
		}
		if strings.ContainsAny(request.Schedule, "\x00\r\n\t") {
			return fmt.Errorf("schedule contains invalid control characters")
		}
	}
	return nil
}

// performWorkloadAction runs one validated workload operation through a dynamic client.
func performWorkloadAction(ctx context.Context, client dynamic.Interface, group, version, resource, namespace, name string, request WorkloadActionRequest) (WorkloadActionResult, error) {
	request = normalizeWorkloadActionRequest(request)
	if err := validateWorkloadAction(group, version, resource, namespace, name, request); err != nil {
		return WorkloadActionResult{}, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	resourceClient := client.Resource(gvr).Namespace(namespace)
	current, err := resourceClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return WorkloadActionResult{}, describeWorkloadError("read workload", err)
	}
	if request.ExpectedResourceVersion != "" && request.ExpectedResourceVersion != current.GetResourceVersion() {
		return WorkloadActionResult{}, fmt.Errorf("resource version conflict for %s/%s: expected %q, current %q; refresh before retrying", resource, name, request.ExpectedResourceVersion, current.GetResourceVersion())
	}

	result := WorkloadActionResult{
		Action:    request.Action,
		Resource:  resource,
		Namespace: namespace,
		Name:      name,
		DryRun:    request.DryRun,
	}
	var updated *unstructured.Unstructured
	switch request.Action {
	case "scale":
		updated, err = scaleWorkload(ctx, resourceClient, current, request)
	case "restart":
		updated, err = restartWorkload(current)
		if err == nil {
			updated, err = updateWorkload(ctx, resourceClient, updated, request.DryRun)
		}
	case "pause", "resume":
		updated, err = setWorkloadPaused(current, request.Action == "pause")
		if err == nil {
			updated, err = updateWorkload(ctx, resourceClient, updated, request.DryRun)
		}
	case "image":
		updated, err = changeWorkloadImage(current, request.Container, request.Image)
		if err == nil {
			updated, err = updateWorkload(ctx, resourceClient, updated, request.DryRun)
		}
	case "schedule":
		updated, err = scheduleCronJob(current, request.Schedule)
		if err == nil {
			updated, err = updateWorkload(ctx, resourceClient, updated, request.DryRun)
		}
	case "rollback":
		updated, err = rollbackDeployment(ctx, client, namespace, current, request.Revision, request.DryRun)
	case "run-now":
		updated, err = runWorkloadNow(ctx, client, namespace, current, request.DryRun)
	}
	if err != nil {
		return WorkloadActionResult{}, describeWorkloadError("perform workload action", err)
	}
	if updated != nil {
		result.ResourceVersion = updated.GetResourceVersion()
		if request.Action == "run-now" {
			result.CreatedName = updated.GetName()
		}
	}
	if request.DryRun {
		result.Status = "dry-run passed"
	} else {
		result.Status = "applied"
	}
	return result, nil
}

// normalizeWorkloadActionRequest trims operator-entered coordinates before validation and mutation.
func normalizeWorkloadActionRequest(request WorkloadActionRequest) WorkloadActionRequest {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ExpectedResourceVersion = strings.TrimSpace(request.ExpectedResourceVersion)
	request.Container = strings.TrimSpace(request.Container)
	request.Image = strings.TrimSpace(request.Image)
	request.Schedule = strings.TrimSpace(request.Schedule)
	return request
}

// isSupportedWorkloadResource identifies the built-in workload resources with targeted operations.
func isSupportedWorkloadResource(group, version, resource string) bool {
	if version != "v1" {
		return false
	}
	if group == "apps" {
		return resource == "deployments" || resource == "statefulsets" || resource == "daemonsets"
	}
	return group == "batch" && (resource == "jobs" || resource == "cronjobs")
}

// isSupportedWorkloadAction identifies actions available for one workload resource.
func isSupportedWorkloadAction(resource, action string) bool {
	switch resource {
	case "deployments":
		return action == "scale" || action == "restart" || action == "pause" || action == "resume" || action == "rollback" || action == "image"
	case "statefulsets":
		return action == "scale" || action == "restart" || action == "image"
	case "daemonsets":
		return action == "restart" || action == "image"
	case "jobs":
		return action == "run-now"
	case "cronjobs":
		return action == "run-now" || action == "pause" || action == "resume" || action == "schedule" || action == "image"
	default:
		return false
	}
}

// scaleWorkload updates a Deployment or StatefulSet through its native scale subresource.
func scaleWorkload(ctx context.Context, resourceClient dynamic.ResourceInterface, current *unstructured.Unstructured, request WorkloadActionRequest) (*unstructured.Unstructured, error) {
	scale, err := resourceClient.Get(ctx, current.GetName(), metav1.GetOptions{}, "scale")
	if err != nil {
		return nil, describeWorkloadScaleError(err)
	}
	if scale.GetResourceVersion() == "" {
		scale.SetResourceVersion(current.GetResourceVersion())
	}
	if current.GetResourceVersion() != "" && scale.GetResourceVersion() != current.GetResourceVersion() {
		return nil, fmt.Errorf("resource version conflict while reading scale subresource: workload is now %q", scale.GetResourceVersion())
	}
	if err := unstructured.SetNestedField(scale.Object, *request.Replicas, "spec", "replicas"); err != nil {
		return nil, fmt.Errorf("set scale replicas: %w", err)
	}
	dryRun := []string(nil)
	if request.DryRun {
		dryRun = []string{metav1.DryRunAll}
	}
	updated, err := resourceClient.Update(ctx, scale, metav1.UpdateOptions{DryRun: dryRun, FieldManager: workloadFieldManager}, "scale")
	if err != nil {
		return nil, describeWorkloadScaleError(err)
	}
	return updated, nil
}

// restartWorkload adds the standard Kubernetes rollout-restart annotation to a pod template.
func restartWorkload(current *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	templatePath, ok := workloadTemplatePath(current.GetKind(), current.GetAPIVersion())
	if !ok {
		return nil, fmt.Errorf("restart is not supported for %s", current.GetKind())
	}
	annotations, _, err := unstructured.NestedStringMap(current.Object, append(templatePath, "metadata", "annotations")...)
	if err != nil {
		return nil, fmt.Errorf("read pod template annotations: %w", err)
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[workloadRestartAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := unstructured.SetNestedStringMap(current.Object, annotations, append(templatePath, "metadata", "annotations")...); err != nil {
		return nil, fmt.Errorf("set rollout restart annotation: %w", err)
	}
	return current, nil
}

// setWorkloadPaused changes Deployment pause state or CronJob scheduling suspension.
func setWorkloadPaused(current *unstructured.Unstructured, paused bool) (*unstructured.Unstructured, error) {
	var field string
	switch current.GetKind() {
	case "Deployment":
		field = "paused"
	case "CronJob":
		field = "suspend"
	default:
		return nil, fmt.Errorf("pause and resume are not supported for %s", current.GetKind())
	}
	if err := unstructured.SetNestedField(current.Object, paused, "spec", field); err != nil {
		return nil, fmt.Errorf("set %s state: %w", field, err)
	}
	return current, nil
}

// changeWorkloadImage updates one named regular container in a supported pod template.
func changeWorkloadImage(current *unstructured.Unstructured, containerName, image string) (*unstructured.Unstructured, error) {
	templatePath, ok := workloadTemplatePath(current.GetKind(), current.GetAPIVersion())
	if !ok {
		return nil, fmt.Errorf("image changes are not supported for %s", current.GetKind())
	}
	containers, found, err := unstructured.NestedSlice(current.Object, append(templatePath, "spec", "containers")...)
	if err != nil {
		return nil, fmt.Errorf("read workload containers: %w", err)
	}
	if !found || len(containers) == 0 {
		return nil, fmt.Errorf("workload has no regular containers")
	}
	changed := false
	for index, rawContainer := range containers {
		container, ok := rawContainer.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := container["name"].(string)
		if name != containerName {
			continue
		}
		container["image"] = image
		containers[index] = container
		changed = true
	}
	if !changed {
		return nil, fmt.Errorf("container %q was not found in workload", containerName)
	}
	if err := unstructured.SetNestedSlice(current.Object, containers, append(templatePath, "spec", "containers")...); err != nil {
		return nil, fmt.Errorf("set workload image: %w", err)
	}
	return current, nil
}

// scheduleCronJob changes a CronJob's Kubernetes schedule expression.
func scheduleCronJob(current *unstructured.Unstructured, schedule string) (*unstructured.Unstructured, error) {
	if current.GetKind() != "CronJob" {
		return nil, fmt.Errorf("scheduling is only supported for CronJobs")
	}
	if err := unstructured.SetNestedField(current.Object, schedule, "spec", "schedule"); err != nil {
		return nil, fmt.Errorf("set CronJob schedule: %w", err)
	}
	return current, nil
}

// rollbackDeployment restores a Deployment pod template from an owned ReplicaSet revision.
func rollbackDeployment(ctx context.Context, client dynamic.Interface, namespace string, current *unstructured.Unstructured, revision *int64, dryRun bool) (*unstructured.Unstructured, error) {
	if current.GetKind() != "Deployment" {
		return nil, fmt.Errorf("rollback is only supported for Deployments")
	}
	replicaSets, err := client.Resource(appsGVR("replicasets")).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Deployment revisions: %w", err)
	}
	candidates := make([]rollbackRevision, 0, len(replicaSets.Items))
	for index := range replicaSets.Items {
		replicaSet := &replicaSets.Items[index]
		if !ownedByDeployment(replicaSet, current) {
			continue
		}
		annotations, _, annotationErr := unstructured.NestedStringMap(replicaSet.Object, "metadata", "annotations")
		if annotationErr != nil {
			return nil, fmt.Errorf("read ReplicaSet revision: %w", annotationErr)
		}
		revisionValue, parseErr := strconv.ParseInt(annotations["deployment.kubernetes.io/revision"], 10, 64)
		if parseErr != nil || revisionValue < 1 {
			continue
		}
		template, found, templateErr := unstructured.NestedMap(replicaSet.Object, "spec", "template")
		if templateErr != nil {
			return nil, fmt.Errorf("read ReplicaSet pod template: %w", templateErr)
		}
		if !found {
			continue
		}
		candidates = append(candidates, rollbackRevision{revision: revisionValue, template: template})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no owned ReplicaSet revisions are available for Deployment %q", current.GetName())
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].revision < candidates[right].revision })
	target, err := chooseRollbackRevision(candidates, current, revision)
	if err != nil {
		return nil, err
	}
	template, ok := runtime.DeepCopyJSONValue(target.template).(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Deployment revision %d has an invalid pod template", target.revision)
	}
	if err := unstructured.SetNestedField(current.Object, template, "spec", "template"); err != nil {
		return nil, fmt.Errorf("set Deployment rollback template: %w", err)
	}
	updated, err := updateWorkload(ctx, client.Resource(appsGVR("deployments")).Namespace(namespace), current, dryRun)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// rollbackRevision contains the safe portion of one owned ReplicaSet revision.
type rollbackRevision struct {
	revision int64
	template map[string]interface{}
}

// chooseRollbackRevision selects an explicit revision or the latest revision before the current one.
func chooseRollbackRevision(candidates []rollbackRevision, current *unstructured.Unstructured, revision *int64) (rollbackRevision, error) {
	if revision != nil && *revision > 0 {
		for _, candidate := range candidates {
			if candidate.revision == *revision {
				return candidate, nil
			}
		}
		return rollbackRevision{}, fmt.Errorf("Deployment revision %d was not found", *revision)
	}
	currentRevision := int64(0)
	annotations, _, err := unstructured.NestedStringMap(current.Object, "metadata", "annotations")
	if err == nil {
		currentRevision, _ = strconv.ParseInt(annotations["deployment.kubernetes.io/revision"], 10, 64)
	}
	for index := len(candidates) - 1; index >= 0; index-- {
		if candidates[index].revision < currentRevision || currentRevision == 0 && index < len(candidates)-1 {
			return candidates[index], nil
		}
	}
	return rollbackRevision{}, fmt.Errorf("no previous Deployment revision is available")
}

// ownedByDeployment verifies that a ReplicaSet belongs to the selected Deployment.
func ownedByDeployment(replicaSet, deployment *unstructured.Unstructured) bool {
	owners, found, err := unstructured.NestedSlice(replicaSet.Object, "metadata", "ownerReferences")
	if err != nil || !found {
		return false
	}
	for _, rawOwner := range owners {
		owner, ok := rawOwner.(map[string]interface{})
		if !ok {
			continue
		}
		kind, _ := owner["kind"].(string)
		name, _ := owner["name"].(string)
		uid, _ := owner["uid"].(string)
		if kind != "Deployment" {
			continue
		}
		if deployment.GetUID() != "" && uid == string(deployment.GetUID()) {
			return true
		}
		if deployment.GetUID() == "" && name == deployment.GetName() {
			return true
		}
	}
	return false
}

// runWorkloadNow creates a fresh Job from a Job or CronJob template.
func runWorkloadNow(ctx context.Context, client dynamic.Interface, namespace string, current *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	var object map[string]interface{}
	switch current.GetKind() {
	case "Job":
		object = rerunJobObject(current, namespace)
	case "CronJob":
		object = cronJobRunObject(current, namespace)
	default:
		return nil, fmt.Errorf("run now is not supported for %s", current.GetKind())
	}
	if object == nil {
		return nil, fmt.Errorf("workload does not contain a valid Job template")
	}
	job := &unstructured.Unstructured{Object: object}
	dryRunValues := []string(nil)
	if dryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	created, err := client.Resource(batchGVR("jobs")).Namespace(namespace).Create(ctx, job, metav1.CreateOptions{DryRun: dryRunValues, FieldManager: workloadFieldManager})
	if err != nil {
		return nil, fmt.Errorf("create one-off Job: %w", err)
	}
	return created, nil
}

// rerunJobObject builds a new Job document from an existing Job without controller identity.
func rerunJobObject(current *unstructured.Unstructured, namespace string) map[string]interface{} {
	spec, found, err := unstructured.NestedMap(current.Object, "spec")
	if err != nil || !found {
		return nil
	}
	specCopy, ok := runtime.DeepCopyJSONValue(spec).(map[string]interface{})
	if !ok {
		return nil
	}
	delete(specCopy, "selector")
	delete(specCopy, "manualSelector")
	removeJobControllerLabels(specCopy)
	return map[string]interface{}{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   copiedJobMetadata(current.GetName(), namespace, current.Object),
		"spec":       specCopy,
	}
}

// cronJobRunObject builds a one-off Job document from a CronJob's jobTemplate.
func cronJobRunObject(current *unstructured.Unstructured, namespace string) map[string]interface{} {
	jobTemplate, found, err := unstructured.NestedMap(current.Object, "spec", "jobTemplate")
	if err != nil || !found {
		return nil
	}
	templateCopy, ok := runtime.DeepCopyJSONValue(jobTemplate).(map[string]interface{})
	if !ok {
		return nil
	}
	spec, found, err := unstructured.NestedMap(templateCopy, "spec")
	if err != nil || !found {
		return nil
	}
	delete(spec, "selector")
	delete(spec, "manualSelector")
	removeJobControllerLabels(spec)
	return map[string]interface{}{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   copiedJobMetadata(current.GetName(), namespace, templateCopy),
		"spec":       spec,
	}
}

// copiedJobMetadata creates safe metadata for a generated one-off Job.
func copiedJobMetadata(sourceName, namespace string, source map[string]interface{}) map[string]interface{} {
	metadata := map[string]interface{}{
		"generateName": generatedJobPrefix(sourceName),
		"namespace":    namespace,
	}
	if sourceMetadata, found, err := unstructured.NestedMap(source, "metadata"); err == nil && found {
		if labels, ok := sourceMetadata["labels"].(map[string]interface{}); ok {
			labelsCopy, copyOK := runtime.DeepCopyJSONValue(labels).(map[string]interface{})
			if copyOK {
				delete(labelsCopy, "controller-uid")
				delete(labelsCopy, "job-name")
				metadata["labels"] = labelsCopy
			}
		}
		if annotations, ok := sourceMetadata["annotations"].(map[string]interface{}); ok {
			metadata["annotations"] = runtime.DeepCopyJSONValue(annotations)
		}
	}
	return metadata
}

// generatedJobPrefix returns a DNS-safe, bounded GenerateName prefix for a one-off Job.
func generatedJobPrefix(sourceName string) string {
	prefix := strings.Trim(strings.ToLower(sourceName), "-") + "-manual-"
	if len(prefix) > 54 {
		prefix = prefix[:54]
	}
	if errors := validation.IsDNS1123Subdomain(strings.TrimSuffix(prefix, "-")); len(errors) > 0 {
		prefix = "kube-haggis-manual-"
	}
	return prefix
}

// removeJobControllerLabels removes controller-owned labels from a copied Job template.
func removeJobControllerLabels(spec map[string]interface{}) {
	template, found, err := unstructured.NestedMap(spec, "template")
	if err != nil || !found {
		return
	}
	metadata, found, err := unstructured.NestedMap(template, "metadata")
	if err != nil || !found {
		return
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		return
	}
	delete(labels, "controller-uid")
	delete(labels, "job-name")
}

// workloadTemplatePath returns the pod-template location for an API workload kind.
func workloadTemplatePath(kind, apiVersion string) ([]string, bool) {
	switch {
	case apiVersion == "apps/v1" && (kind == "Deployment" || kind == "StatefulSet" || kind == "DaemonSet"):
		return []string{"spec", "template"}, true
	case apiVersion == "batch/v1" && kind == "Job":
		return []string{"spec", "template"}, true
	case apiVersion == "batch/v1" && kind == "CronJob":
		return []string{"spec", "jobTemplate", "spec", "template"}, true
	default:
		return nil, false
	}
}

// updateWorkload performs a resource-versioned dynamic full-object update with optional dry-run.
func updateWorkload(ctx context.Context, resourceClient dynamic.ResourceInterface, object *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	unstructured.RemoveNestedField(object.Object, "metadata", "managedFields")
	dryRunValues := []string(nil)
	if dryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	updated, err := resourceClient.Update(ctx, object, metav1.UpdateOptions{DryRun: dryRunValues, FieldManager: workloadFieldManager})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// describeWorkloadScaleError gives scale subresource failures an operator-facing explanation.
func describeWorkloadScaleError(err error) error {
	if apierrors.IsMethodNotSupported(err) {
		return fmt.Errorf("workload scale subresource is not supported by this Kubernetes API server: %w", err)
	}
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("workload scale subresource was not found; this workload or Kubernetes API server may not support scaling: %w", err)
	}
	if apierrors.IsForbidden(err) {
		return fmt.Errorf("workload scale was denied by Kubernetes RBAC: %w", err)
	}
	if apierrors.IsConflict(err) {
		return fmt.Errorf("workload scale conflicted with another update; refresh before retrying: %w", err)
	}
	return err
}

// describeWorkloadError gives common Kubernetes workload failures a clear API message.
func describeWorkloadError(operation string, err error) error {
	if apierrors.IsForbidden(err) {
		return fmt.Errorf("%s was denied by Kubernetes RBAC: %w", operation, err)
	}
	if apierrors.IsConflict(err) {
		return fmt.Errorf("%s conflicted with another update; refresh before retrying: %w", operation, err)
	}
	if apierrors.IsMethodNotSupported(err) {
		return fmt.Errorf("%s is not supported by this Kubernetes API server: %w", operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
