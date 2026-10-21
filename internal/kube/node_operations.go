package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	policytyped "k8s.io/client-go/kubernetes/typed/policy/v1"
)

const (
	// nodeFieldManager identifies native Node updates made by KubeHaggis.
	nodeFieldManager = "kube-haggis"
	// nodeMirrorPodAnnotation identifies a static Pod mirror that cannot be evicted.
	nodeMirrorPodAnnotation = "kubernetes.io/config.mirror"
	// nodeMaxGracePeriodSeconds bounds an explicitly requested eviction grace period.
	nodeMaxGracePeriodSeconds int64 = 24 * 60 * 60
)

// NodeActionRequest describes one explicit operation against a cluster-scoped Node.
type NodeActionRequest struct {
	Action                  string `json:"action"`
	DryRun                  bool   `json:"dryRun"`
	ExpectedResourceVersion string `json:"expectedResourceVersion,omitempty"`
	TaintKey                string `json:"taintKey,omitempty"`
	TaintValue              string `json:"taintValue,omitempty"`
	TaintEffect             string `json:"taintEffect,omitempty"`
	DeleteEmptyDirData      bool   `json:"deleteEmptyDirData,omitempty"`
	IgnoreDaemonSets        bool   `json:"ignoreDaemonSets,omitempty"`
	GracePeriodSeconds      *int64 `json:"gracePeriodSeconds,omitempty"`
	Force                   bool   `json:"force,omitempty"`
}

// NodeTaint is the redacted, browser-facing representation of a Node taint.
type NodeTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

// NodePodImpact describes how a drain review will treat one Pod on the Node.
type NodePodImpact struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Phase        string `json:"phase"`
	Controller   string `json:"controller,omitempty"`
	Action       string `json:"action"`
	UsesEmptyDir bool   `json:"usesEmptyDir,omitempty"`
	Limitation   string `json:"limitation,omitempty"`
}

// NodeActionResult reports a redacted outcome and drain review for one Node action.
type NodeActionResult struct {
	Action          string          `json:"action"`
	Resource        string          `json:"resource"`
	Name            string          `json:"name"`
	DryRun          bool            `json:"dryRun"`
	Ready           bool            `json:"ready"`
	ResourceVersion string          `json:"resourceVersion,omitempty"`
	Status          string          `json:"status"`
	Taints          []NodeTaint     `json:"taints,omitempty"`
	Pods            []NodePodImpact `json:"pods,omitempty"`
	PodCount        int             `json:"podCount,omitempty"`
	EvictedCount    int             `json:"evictedCount,omitempty"`
	SkippedCount    int             `json:"skippedCount,omitempty"`
	BlockedCount    int             `json:"blockedCount,omitempty"`
	Limitations     []string        `json:"limitations,omitempty"`
}

// nodeDrainPlan contains the safe, non-secret plan produced before a drain mutation.
type nodeDrainPlan struct {
	impacts     []NodePodImpact
	evictable   []*corev1.Pod
	limitations []string
	blockers    []string
}

// NodeAction applies a native cordon, drain, or taint operation to one Node.
func (s *Service) NodeAction(ctx context.Context, contextName, name string, request NodeActionRequest) (NodeActionResult, error) {
	request = normalizeNodeActionRequest(request)
	if err := validateNodeAction(name, request); err != nil {
		return NodeActionResult{}, err
	}
	clientset, _, err := s.typedClient(contextName)
	if err != nil {
		return NodeActionResult{}, err
	}
	return performNodeAction(ctx, clientset, name, request)
}

// validateNodeAction checks Node coordinates and action-specific fields before client creation.
func validateNodeAction(name string, request NodeActionRequest) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("Node name is required")
	}
	if errors := validation.IsDNS1123Subdomain(name); len(errors) > 0 {
		return fmt.Errorf("Node name is invalid: %s", strings.Join(errors, "; "))
	}
	switch request.Action {
	case "cordon", "uncordon":
		if request.DeleteEmptyDirData || request.IgnoreDaemonSets || request.GracePeriodSeconds != nil || request.Force {
			return fmt.Errorf("drain options are only valid for a Node drain")
		}
	case "drain":
		if request.Force {
			return fmt.Errorf("force deletion is not supported for Node drains; KubeHaggis uses eviction APIs only")
		}
		if request.GracePeriodSeconds != nil && (*request.GracePeriodSeconds < 0 || *request.GracePeriodSeconds > nodeMaxGracePeriodSeconds) {
			return fmt.Errorf("drain grace period must be between 0 and %d seconds", nodeMaxGracePeriodSeconds)
		}
	case "add-taint", "remove-taint":
		if request.TaintKey == "" {
			return fmt.Errorf("taint key is required")
		}
		if errors := validation.IsQualifiedName(request.TaintKey); len(errors) > 0 {
			return fmt.Errorf("taint key is invalid: %s", strings.Join(errors, "; "))
		}
		if request.TaintValue != "" {
			if errors := validation.IsValidLabelValue(request.TaintValue); len(errors) > 0 {
				return fmt.Errorf("taint value is invalid: %s", strings.Join(errors, "; "))
			}
		}
		if !isSupportedTaintEffect(request.TaintEffect) {
			return fmt.Errorf("taint effect must be NoSchedule, PreferNoSchedule, or NoExecute")
		}
		if request.DeleteEmptyDirData || request.IgnoreDaemonSets || request.GracePeriodSeconds != nil || request.Force {
			return fmt.Errorf("drain options are only valid for a Node drain")
		}
	default:
		return fmt.Errorf("unsupported Node action %q", request.Action)
	}
	return nil
}

// normalizeNodeActionRequest trims operator-entered Node action fields before validation and mutation.
func normalizeNodeActionRequest(request NodeActionRequest) NodeActionRequest {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ExpectedResourceVersion = strings.TrimSpace(request.ExpectedResourceVersion)
	request.TaintKey = strings.TrimSpace(request.TaintKey)
	request.TaintValue = strings.TrimSpace(request.TaintValue)
	request.TaintEffect = strings.TrimSpace(request.TaintEffect)
	return request
}

// performNodeAction runs one validated Node operation through typed Kubernetes clients.
func performNodeAction(ctx context.Context, clientset kubernetes.Interface, name string, request NodeActionRequest) (NodeActionResult, error) {
	request = normalizeNodeActionRequest(request)
	if err := validateNodeAction(name, request); err != nil {
		return NodeActionResult{}, err
	}
	nodes := clientset.CoreV1().Nodes()
	node, err := nodes.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return NodeActionResult{}, describeNodeError("read Node", err)
	}
	if request.ExpectedResourceVersion != "" && request.ExpectedResourceVersion != node.ResourceVersion {
		return NodeActionResult{}, fmt.Errorf("resource version conflict for Node %q: expected %q, current %q; refresh before retrying", name, request.ExpectedResourceVersion, node.ResourceVersion)
	}

	result := NodeActionResult{
		Action:          request.Action,
		Resource:        "nodes",
		Name:            name,
		DryRun:          request.DryRun,
		Ready:           true,
		ResourceVersion: node.ResourceVersion,
		Taints:          nodeTaintSummaries(node.Spec.Taints),
	}
	switch request.Action {
	case "cordon":
		updated, updateErr := updateNode(ctx, nodes, node, true, request.DryRun)
		if updateErr != nil {
			return NodeActionResult{}, describeNodeError("cordon Node", updateErr)
		}
		return finishNodeUpdateResult(result, updated, request.DryRun), nil
	case "uncordon":
		updated, updateErr := updateNode(ctx, nodes, node, false, request.DryRun)
		if updateErr != nil {
			return NodeActionResult{}, describeNodeError("uncordon Node", updateErr)
		}
		return finishNodeUpdateResult(result, updated, request.DryRun), nil
	case "add-taint", "remove-taint":
		return performNodeTaintAction(ctx, nodes, node, request, result)
	case "drain":
		return performNodeDrain(ctx, nodes, clientset.CoreV1().Pods(""), clientset.PolicyV1(), node, request, result)
	default:
		return NodeActionResult{}, fmt.Errorf("unsupported Node action %q", request.Action)
	}
}

// updateNode applies one resource-versioned Node scheduling update with optional server dry-run.
func updateNode(ctx context.Context, nodes interface {
	Update(context.Context, *corev1.Node, metav1.UpdateOptions) (*corev1.Node, error)
}, node *corev1.Node, unschedulable, dryRun bool) (*corev1.Node, error) {
	candidate := node.DeepCopy()
	candidate.Spec.Unschedulable = unschedulable
	// ManagedFields are owned by the API server and are not part of the operator payload.
	candidate.ManagedFields = nil
	dryRunValues := []string(nil)
	if dryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	return nodes.Update(ctx, candidate, metav1.UpdateOptions{DryRun: dryRunValues, FieldManager: nodeFieldManager})
}

// performNodeTaintAction validates and updates one exact key/effect/value taint tuple.
func performNodeTaintAction(ctx context.Context, nodes interface {
	Update(context.Context, *corev1.Node, metav1.UpdateOptions) (*corev1.Node, error)
}, node *corev1.Node, request NodeActionRequest, result NodeActionResult) (NodeActionResult, error) {
	target := corev1.Taint{Key: request.TaintKey, Value: request.TaintValue, Effect: corev1.TaintEffect(request.TaintEffect)}
	candidate := node.DeepCopy()
	if request.Action == "add-taint" {
		for _, existing := range candidate.Spec.Taints {
			if existing.MatchTaint(&target) {
				if existing.Value == target.Value {
					result.Status = nodeDryRunStatus(request.DryRun)
					result.Taints = nodeTaintSummaries(candidate.Spec.Taints)
					return result, nil
				}
				return NodeActionResult{}, fmt.Errorf("taint %s:%s already exists with a different value; remove it explicitly before adding a replacement", target.Key, target.Effect)
			}
		}
		candidate.Spec.Taints = append(candidate.Spec.Taints, target)
	} else {
		removed := false
		remaining := make([]corev1.Taint, 0, len(candidate.Spec.Taints))
		for _, existing := range candidate.Spec.Taints {
			if existing.MatchTaint(&target) {
				if existing.Value != target.Value {
					return NodeActionResult{}, fmt.Errorf("taint %s:%s changed value from %q; refresh before removing it", target.Key, target.Effect, existing.Value)
				}
				removed = true
				continue
			}
			remaining = append(remaining, existing)
		}
		if !removed {
			return NodeActionResult{}, fmt.Errorf("taint %s:%s with the requested value was not found; refresh before retrying", target.Key, target.Effect)
		}
		candidate.Spec.Taints = remaining
	}
	candidate.ManagedFields = nil
	dryRunValues := []string(nil)
	if request.DryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	updated, err := nodes.Update(ctx, candidate, metav1.UpdateOptions{DryRun: dryRunValues, FieldManager: nodeFieldManager})
	if err != nil {
		return NodeActionResult{}, describeNodeError("update Node taints", err)
	}
	result = finishNodeUpdateResult(result, updated, request.DryRun)
	if updated == nil {
		result.Taints = nodeTaintSummaries(candidate.Spec.Taints)
	}
	return result, nil
}

// performNodeDrain plans, reviews, and then evicts eligible Pods without force deletion.
func performNodeDrain(ctx context.Context, nodes interface {
	Update(context.Context, *corev1.Node, metav1.UpdateOptions) (*corev1.Node, error)
}, pods interface {
	List(context.Context, metav1.ListOptions) (*corev1.PodList, error)
}, evictions policytyped.PolicyV1Interface, node *corev1.Node, request NodeActionRequest, result NodeActionResult) (NodeActionResult, error) {
	plan, err := buildNodeDrainPlan(ctx, pods, node.Name, request)
	if err != nil {
		return NodeActionResult{}, err
	}
	result = applyDrainPlanResult(result, plan)
	if len(plan.blockers) > 0 {
		result.Ready = false
		result.Status = "dry-run blocked"
		if request.DryRun {
			return result, nil
		}
		return NodeActionResult{}, fmt.Errorf("drain blocked by Pod limitations: %s", strings.Join(plan.blockers, "; "))
	}

	if node.Spec.Unschedulable == false {
		preview, updateErr := updateNode(ctx, nodes, node, true, true)
		if updateErr != nil {
			return NodeActionResult{}, describeNodeError("review Node cordon for drain", updateErr)
		}
		result.ResourceVersion = preview.ResourceVersion
	}
	blocked, reviewErr := reviewNodeEvictions(ctx, evictions, plan, request, &result)
	if reviewErr != nil {
		return NodeActionResult{}, reviewErr
	}
	if blocked {
		result.Ready = false
		result.Status = "dry-run blocked"
		if request.DryRun {
			return result, nil
		}
		return NodeActionResult{}, fmt.Errorf("drain blocked by Kubernetes eviction policy; refresh and resolve the listed Pod limitations before retrying")
	}
	if request.DryRun {
		result.Status = "dry-run passed"
		return result, nil
	}

	if !node.Spec.Unschedulable {
		updated, updateErr := updateNode(ctx, nodes, node, true, false)
		if updateErr != nil {
			return NodeActionResult{}, describeNodeError("cordon Node for drain", updateErr)
		}
		result.ResourceVersion = updated.ResourceVersion
	}
	for _, pod := range plan.evictable {
		if evictErr := evictNodePod(ctx, evictions.Evictions(pod.Namespace), pod, request, false); evictErr != nil {
			if apierrors.IsNotFound(evictErr) {
				result.SkippedCount++
				result.Limitations = appendUniqueString(result.Limitations, fmt.Sprintf("Pod %s/%s disappeared before eviction", pod.Namespace, pod.Name))
				result.Pods[indexOfPodImpact(result.Pods, pod.Namespace, pod.Name)].Action = "skipped"
				continue
			}
			return NodeActionResult{}, fmt.Errorf("drain stopped after evicting %d of %d Pods: %w", result.EvictedCount, len(plan.evictable), describeNodeEvictionError(pod, evictErr))
		}
		result.EvictedCount++
	}
	result.Status = "applied"
	return result, nil
}

// buildNodeDrainPlan lists Node Pods and records every safe drain limitation before mutation.
func buildNodeDrainPlan(ctx context.Context, pods interface {
	List(context.Context, metav1.ListOptions) (*corev1.PodList, error)
}, nodeName string, request NodeActionRequest) (nodeDrainPlan, error) {
	items, err := listNodePods(ctx, pods, nodeName)
	if err != nil {
		return nodeDrainPlan{}, err
	}
	return planNodeDrain(items, nodeName, request), nil
}

// planNodeDrain classifies scheduled Pods without making any Kubernetes mutation.
func planNodeDrain(pods []corev1.Pod, nodeName string, request NodeActionRequest) nodeDrainPlan {
	plan := nodeDrainPlan{impacts: make([]NodePodImpact, 0, len(pods))}
	sort.Slice(pods, func(left, right int) bool {
		if pods[left].Namespace == pods[right].Namespace {
			return pods[left].Name < pods[right].Name
		}
		return pods[left].Namespace < pods[right].Namespace
	})
	for index := range pods {
		pod := &pods[index]
		if pod.Spec.NodeName != nodeName {
			continue
		}
		impact := NodePodImpact{
			Namespace:    pod.Namespace,
			Name:         pod.Name,
			Phase:        string(pod.Status.Phase),
			Controller:   podController(pod),
			Action:       "evict",
			UsesEmptyDir: podUsesEmptyDir(pod),
		}
		if impact.Phase == "" {
			impact.Phase = "Unknown"
		}
		switch {
		case pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed:
			impact.Action = "skipped"
			impact.Limitation = "terminal Pod does not require eviction"
		case pod.DeletionTimestamp != nil:
			impact.Action = "skipped"
			impact.Limitation = "Pod is already terminating"
		case isMirrorPod(pod):
			impact.Action = "blocked"
			impact.Limitation = "static mirror Pods cannot be evicted through the Kubernetes eviction API"
			plan.blockers = append(plan.blockers, podLimitation(pod, impact.Limitation))
		case isDaemonSetPod(pod) && request.IgnoreDaemonSets:
			impact.Action = "skipped"
			impact.Limitation = "DaemonSet Pod was explicitly left in place"
		case isDaemonSetPod(pod):
			impact.Action = "blocked"
			impact.Limitation = "DaemonSet Pod requires explicit ignore-daemonsets acknowledgement"
			plan.blockers = append(plan.blockers, podLimitation(pod, impact.Limitation))
		case impact.Controller == "":
			impact.Action = "blocked"
			impact.Limitation = "unmanaged Pod would require force deletion, which this workflow does not support"
			plan.blockers = append(plan.blockers, podLimitation(pod, impact.Limitation))
		case impact.UsesEmptyDir && !request.DeleteEmptyDirData:
			impact.Action = "blocked"
			impact.Limitation = "Pod uses emptyDir data; explicitly allow its deletion to continue"
			plan.blockers = append(plan.blockers, podLimitation(pod, impact.Limitation))
		default:
			plan.evictable = append(plan.evictable, pod.DeepCopy())
		}
		if impact.Limitation != "" {
			plan.limitations = appendUniqueString(plan.limitations, podLimitation(pod, impact.Limitation))
		}
		plan.impacts = append(plan.impacts, impact)
	}
	return plan
}

// listNodePods returns all Pods currently assigned to a Node for drain planning.
func listNodePods(ctx context.Context, pods interface {
	List(context.Context, metav1.ListOptions) (*corev1.PodList, error)
}, nodeName string) ([]corev1.Pod, error) {
	items, err := pods.List(ctx, metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("spec.nodeName", nodeName).String()})
	if err != nil {
		return nil, describeNodeError("list Pods for Node drain", err)
	}
	return items.Items, nil
}

// reviewNodeEvictions performs server-side dry-run eviction requests and records PDB limitations.
func reviewNodeEvictions(ctx context.Context, evictions interface {
	Evictions(string) policytyped.EvictionInterface
}, plan nodeDrainPlan, request NodeActionRequest, result *NodeActionResult) (bool, error) {
	blocked := false
	for _, pod := range plan.evictable {
		err := evictNodePod(ctx, evictions.Evictions(pod.Namespace), pod, request, true)
		if err == nil {
			continue
		}
		if apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err) {
			blocked = true
			impactIndex := indexOfPodImpact(result.Pods, pod.Namespace, pod.Name)
			result.Pods[impactIndex].Action = "blocked"
			result.Pods[impactIndex].Limitation = "eviction was rejected by a PodDisruptionBudget or eviction policy"
			result.BlockedCount++
			result.Limitations = appendUniqueString(result.Limitations, podLimitation(pod, result.Pods[impactIndex].Limitation))
			continue
		}
		if isUnsupportedNodeEvictionError(err) {
			return false, fmt.Errorf("Node drain eviction API is not supported by this Kubernetes API server: %w", err)
		}
		if apierrors.IsNotFound(err) {
			blocked = true
			impactIndex := indexOfPodImpact(result.Pods, pod.Namespace, pod.Name)
			result.Pods[impactIndex].Action = "blocked"
			result.Pods[impactIndex].Limitation = "Pod disappeared during review; refresh before applying the drain"
			result.BlockedCount++
			result.Limitations = appendUniqueString(result.Limitations, podLimitation(pod, result.Pods[impactIndex].Limitation))
			continue
		}
		return false, describeNodeEvictionError(pod, err)
	}
	return blocked, nil
}

// isUnsupportedNodeEvictionError distinguishes an unavailable eviction endpoint from a deleted Pod.
func isUnsupportedNodeEvictionError(err error) bool {
	if apierrors.IsMethodNotSupported(err) {
		return true
	}
	if !apierrors.IsNotFound(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "requested resource") || strings.Contains(message, "eviction")
}

// evictNodePod sends one native policy/v1 eviction request with optional dry-run and grace period.
func evictNodePod(ctx context.Context, evictions interface {
	Evict(context.Context, *policyv1.Eviction) error
}, pod *corev1.Pod, request NodeActionRequest, dryRun bool) error {
	deleteOptions := &metav1.DeleteOptions{}
	if dryRun {
		deleteOptions.DryRun = []string{metav1.DryRunAll}
	}
	if request.GracePeriodSeconds != nil {
		grace := *request.GracePeriodSeconds
		deleteOptions.GracePeriodSeconds = &grace
	}
	if pod.UID != "" || pod.ResourceVersion != "" {
		deleteOptions.Preconditions = &metav1.Preconditions{}
		if pod.UID != "" {
			uid := pod.UID
			deleteOptions.Preconditions.UID = &uid
		}
		if pod.ResourceVersion != "" {
			resourceVersion := pod.ResourceVersion
			deleteOptions.Preconditions.ResourceVersion = &resourceVersion
		}
	}
	return evictions.Evict(ctx, &policyv1.Eviction{
		ObjectMeta:    metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace, ResourceVersion: pod.ResourceVersion, UID: pod.UID},
		DeleteOptions: deleteOptions,
	})
}

// finishNodeUpdateResult converts one typed update into the stable action response.
func finishNodeUpdateResult(result NodeActionResult, updated *corev1.Node, dryRun bool) NodeActionResult {
	if updated != nil {
		result.ResourceVersion = updated.ResourceVersion
		result.Taints = nodeTaintSummaries(updated.Spec.Taints)
	}
	result.Status = nodeDryRunStatus(dryRun)
	return result
}

// nodeDryRunStatus returns the stable status used by reviewed and applied Node actions.
func nodeDryRunStatus(dryRun bool) string {
	if dryRun {
		return "dry-run passed"
	}
	return "applied"
}

// applyDrainPlanResult copies a safe drain plan into a browser-facing result.
func applyDrainPlanResult(result NodeActionResult, plan nodeDrainPlan) NodeActionResult {
	result.Pods = plan.impacts
	result.PodCount = len(plan.impacts)
	result.SkippedCount = 0
	for _, impact := range plan.impacts {
		if impact.Action == "skipped" {
			result.SkippedCount++
		}
	}
	result.BlockedCount = len(plan.blockers)
	result.Limitations = append([]string(nil), plan.limitations...)
	return result
}

// nodeTaintSummaries converts native taints without exposing unrelated Node fields.
func nodeTaintSummaries(taints []corev1.Taint) []NodeTaint {
	result := make([]NodeTaint, 0, len(taints))
	for _, taint := range taints {
		result = append(result, NodeTaint{Key: taint.Key, Value: taint.Value, Effect: string(taint.Effect)})
	}
	return result
}

// isSupportedTaintEffect identifies the effects accepted by the core Node API.
func isSupportedTaintEffect(effect string) bool {
	switch corev1.TaintEffect(effect) {
	case corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		return true
	default:
		return false
	}
}

// podController returns the controlling owner identity without reading Pod contents.
func podController(pod *corev1.Pod) string {
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			return owner.Kind + "/" + owner.Name
		}
	}
	return ""
}

// isDaemonSetPod reports whether a Pod is controlled by a DaemonSet.
func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// isMirrorPod reports whether a Pod is a static Pod mirror managed by a kubelet.
func isMirrorPod(pod *corev1.Pod) bool {
	_, found := pod.Annotations[nodeMirrorPodAnnotation]
	return found
}

// podUsesEmptyDir reports whether a Pod references local emptyDir data.
func podUsesEmptyDir(pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir != nil {
			return true
		}
	}
	return false
}

// podLimitation formats a non-secret Pod coordinate and its safe-drain limitation.
func podLimitation(pod *corev1.Pod, limitation string) string {
	return fmt.Sprintf("%s/%s: %s", pod.Namespace, pod.Name, limitation)
}

// indexOfPodImpact locates one Pod impact by its namespace and name.
func indexOfPodImpact(impacts []NodePodImpact, namespace, name string) int {
	for index, impact := range impacts {
		if impact.Namespace == namespace && impact.Name == name {
			return index
		}
	}
	return 0
}

// appendUniqueString adds a limitation once while preserving review order.
func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// describeNodeError turns native API errors into actionable RBAC, unsupported, and conflict messages.
func describeNodeError(operation string, err error) error {
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("%s denied by Kubernetes RBAC: %w", operation, err)
	case apierrors.IsConflict(err):
		return fmt.Errorf("%s conflicted with a newer resource version; refresh before retrying: %w", operation, err)
	case apierrors.IsMethodNotSupported(err):
		return fmt.Errorf("%s is not supported by this Kubernetes API server: %w", operation, err)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

// describeNodeEvictionError turns one eviction failure into a safe operator-facing error.
func describeNodeEvictionError(pod *corev1.Pod, err error) error {
	operation := fmt.Sprintf("eviction of Pod %s/%s", pod.Namespace, pod.Name)
	return describeNodeError(operation, err)
}
