package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestValidateNodeActionRejectsUnsafeInputs verifies Node operation validation before client creation.
func TestValidateNodeActionRejectsUnsafeInputs(t *testing.T) {
	grace := int64(nodeMaxGracePeriodSeconds + 1)
	tests := []struct {
		name    string
		node    string
		request NodeActionRequest
		want    string
	}{
		{name: "missing node", request: NodeActionRequest{Action: "cordon"}, want: "Node name is required"},
		{name: "invalid action", node: "worker-a", request: NodeActionRequest{Action: "delete"}, want: "unsupported Node action"},
		{name: "invalid taint key", node: "worker-a", request: NodeActionRequest{Action: "add-taint", TaintKey: "not valid", TaintEffect: "NoSchedule"}, want: "taint key is invalid"},
		{name: "invalid taint effect", node: "worker-a", request: NodeActionRequest{Action: "add-taint", TaintKey: "example.com/pool", TaintEffect: "Invalid"}, want: "taint effect"},
		{name: "force drain", node: "worker-a", request: NodeActionRequest{Action: "drain", Force: true}, want: "force deletion is not supported"},
		{name: "grace too long", node: "worker-a", request: NodeActionRequest{Action: "drain", GracePeriodSeconds: &grace}, want: "grace period"},
		{name: "drain option on cordon", node: "worker-a", request: NodeActionRequest{Action: "cordon", IgnoreDaemonSets: true}, want: "only valid for a Node drain"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateNodeAction(test.node, normalizeNodeActionRequest(test.request))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// TestPerformNodeActionUsesVersionedCordonDryRun verifies typed Node updates preserve concurrency and dry-run state.
func TestPerformNodeActionUsesVersionedCordonDryRun(t *testing.T) {
	node := testNode("worker-a", "7")
	client := k8sfake.NewSimpleClientset(node)
	var captured *corev1.Node
	var dryRun []string
	client.PrependReactor("update", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		update := action.(clienttesting.UpdateAction)
		captured = update.GetObject().(*corev1.Node).DeepCopy()
		dryRun = append([]string(nil), action.(clienttesting.UpdateActionImpl).UpdateOptions.DryRun...)
		return true, captured, nil
	})

	result, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{
		Action:                  "cordon",
		DryRun:                  true,
		ExpectedResourceVersion: "7",
	})
	if err != nil {
		t.Fatalf("cordon dry-run failed: %v", err)
	}
	if result.Status != "dry-run passed" || result.ResourceVersion != "7" || captured == nil || !captured.Spec.Unschedulable {
		t.Fatalf("unexpected cordon result: %+v node=%+v", result, captured)
	}
	if len(dryRun) != 1 || dryRun[0] != metav1.DryRunAll {
		t.Fatalf("Node update was not server-side dry-run: %v", dryRun)
	}
	if len(captured.ManagedFields) != 0 {
		t.Fatal("managedFields were sent in Node update")
	}
}

// TestPerformNodeActionRejectsStaleVersion verifies stale Node actions do not attempt an update.
func TestPerformNodeActionRejectsStaleVersion(t *testing.T) {
	client := k8sfake.NewSimpleClientset(testNode("worker-a", "8"))
	updates := 0
	client.PrependReactor("update", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		updates++
		return true, nil, nil
	})

	_, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{Action: "cordon", ExpectedResourceVersion: "7"})
	if err == nil || !strings.Contains(err.Error(), "resource version conflict") {
		t.Fatalf("expected resource-version conflict, got %v", err)
	}
	if updates != 0 {
		t.Fatalf("stale Node action attempted an update: %d", updates)
	}
}

// TestPerformNodeActionReportsRBACDeniedUpdate verifies typed Node update failures remain actionable.
func TestPerformNodeActionReportsRBACDeniedUpdate(t *testing.T) {
	client := k8sfake.NewSimpleClientset(testNode("worker-a", "7"))
	client.PrependReactor("update", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "worker-a", nil)
	})

	_, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{
		Action:                  "cordon",
		ExpectedResourceVersion: "7",
	})
	if err == nil || !strings.Contains(err.Error(), "RBAC") {
		t.Fatalf("expected RBAC Node update denial, got %v", err)
	}
}

// TestPerformNodeTaintActionUsesExactTaintTuple verifies add and remove operations do not overwrite or broadly remove taints.
func TestPerformNodeTaintActionUsesExactTaintTuple(t *testing.T) {
	node := testNode("worker-a", "4")
	node.Spec.Taints = []corev1.Taint{{Key: "example.com/pool", Value: "system", Effect: corev1.TaintEffectNoSchedule}}
	client := k8sfake.NewSimpleClientset(node)
	var captured *corev1.Node
	client.PrependReactor("update", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		captured = action.(clienttesting.UpdateAction).GetObject().(*corev1.Node).DeepCopy()
		return true, captured, nil
	})

	result, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{
		Action:      "add-taint",
		TaintKey:    "example.com/workload",
		TaintValue:  "batch",
		TaintEffect: string(corev1.TaintEffectPreferNoSchedule),
	})
	if err != nil {
		t.Fatalf("add taint failed: %v", err)
	}
	if len(result.Taints) != 2 || captured == nil || len(captured.Spec.Taints) != 2 {
		t.Fatalf("unexpected add-taint result: %+v node=%+v", result, captured)
	}

	_, err = performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{
		Action:      "remove-taint",
		TaintKey:    "example.com/pool",
		TaintValue:  "changed",
		TaintEffect: string(corev1.TaintEffectNoSchedule),
	})
	if err == nil || !strings.Contains(err.Error(), "changed value") {
		t.Fatalf("expected exact-value removal conflict, got %v", err)
	}
}

// TestPlanNodeDrainSurfacesPodLimitations verifies drain never hides unmanaged, static, DaemonSet, or emptyDir impact.
func TestPlanNodeDrainSurfacesPodLimitations(t *testing.T) {
	controller := true
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unmanaged", Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{Name: "static", Namespace: "kube-system", Annotations: map[string]string{nodeMirrorPodAnnotation: "hash"}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "job", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a", Volumes: []corev1.Volume{{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{Name: "complete", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "job", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
	}

	plan := planNodeDrain(pods, "worker-a", NodeActionRequest{Action: "drain"})
	if len(plan.impacts) != 6 || len(plan.evictable) != 1 || len(plan.blockers) != 4 || len(plan.limitations) != 5 {
		t.Fatalf("unexpected drain plan: %+v", plan)
	}
	for _, limitation := range plan.limitations {
		if strings.Contains(limitation, "secret") {
			t.Fatalf("drain limitation leaked sensitive data: %q", limitation)
		}
	}
}

// TestPerformNodeDrainUsesDryRunEvictions verifies drain cordons and reviews through eviction APIs only.
func TestPerformNodeDrainUsesDryRunEvictions(t *testing.T) {
	controller := true
	node := testNode("worker-a", "9")
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "default", UID: "pod-1", ResourceVersion: "11", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	client := k8sfake.NewSimpleClientset(node, pod)
	var update *corev1.Node
	var evictions []*policyv1.Eviction
	deletes := 0
	client.PrependReactor("update", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		update = action.(clienttesting.UpdateAction).GetObject().(*corev1.Node).DeepCopy()
		return true, update, nil
	})
	client.PrependReactor("create", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		evictions = append(evictions, action.(clienttesting.CreateAction).GetObject().(*policyv1.Eviction).DeepCopy())
		return true, nil, nil
	})
	client.PrependReactor("delete", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		deletes++
		return true, nil, nil
	})

	result, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{Action: "drain", DryRun: true, ExpectedResourceVersion: "9"})
	if err != nil {
		t.Fatalf("drain dry-run failed: %v", err)
	}
	if result.Status != "dry-run passed" || !result.Ready || result.PodCount != 1 || result.EvictedCount != 0 || update == nil || !update.Spec.Unschedulable {
		t.Fatalf("unexpected drain result: %+v update=%+v", result, update)
	}
	if len(evictions) != 1 || len(evictions[0].DeleteOptions.DryRun) != 1 || evictions[0].DeleteOptions.DryRun[0] != metav1.DryRunAll {
		t.Fatalf("eviction was not server-side dry-run: %+v", evictions)
	}
	if deletes != 0 {
		t.Fatalf("drain attempted Pod deletion: %d", deletes)
	}
}

// TestPerformNodeDrainUsesEvictionForFinalMutation verifies the reviewed final path never deletes Pods directly.
func TestPerformNodeDrainUsesEvictionForFinalMutation(t *testing.T) {
	controller := true
	node := testNode("worker-a", "9")
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "default", UID: "pod-1", ResourceVersion: "11", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	client := k8sfake.NewSimpleClientset(node, pod)
	var evictions []*policyv1.Eviction
	deletes := 0
	client.PrependReactor("update", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, action.(clienttesting.UpdateAction).GetObject(), nil
	})
	client.PrependReactor("create", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		evictions = append(evictions, action.(clienttesting.CreateAction).GetObject().(*policyv1.Eviction).DeepCopy())
		return true, nil, nil
	})
	client.PrependReactor("delete", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		deletes++
		return true, nil, nil
	})

	result, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{Action: "drain", ExpectedResourceVersion: "9"})
	if err != nil {
		t.Fatalf("final drain failed: %v", err)
	}
	if result.Status != "applied" || result.EvictedCount != 1 || len(evictions) != 2 {
		t.Fatalf("unexpected final drain result: %+v evictions=%d", result, len(evictions))
	}
	if len(evictions[0].DeleteOptions.DryRun) != 1 || len(evictions[1].DeleteOptions.DryRun) != 0 {
		t.Fatalf("final drain did not separate review and mutation eviction requests: %+v", evictions)
	}
	if deletes != 0 {
		t.Fatalf("final drain attempted direct Pod deletion: %d", deletes)
	}
}

// TestPerformNodeDrainBlocksPodDisruptionBudget verifies PDB-limited evictions stop review before mutation.
func TestPerformNodeDrainBlocksPodDisruptionBudget(t *testing.T) {
	controller := true
	client := k8sfake.NewSimpleClientset(testNode("worker-a", "3"), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "default", ResourceVersion: "5", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
	updates := 0
	client.PrependReactor("update", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		updates++
		return true, action.(clienttesting.UpdateAction).GetObject(), nil
	})
	client.PrependReactor("create", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "eviction" {
			return true, nil, apierrors.NewTooManyRequests("disruption budget", 1)
		}
		return false, nil, nil
	})

	result, err := performNodeAction(context.Background(), client, "worker-a", NodeActionRequest{Action: "drain", DryRun: true, ExpectedResourceVersion: "3"})
	if err != nil {
		t.Fatalf("PDB drain review returned an error: %v", err)
	}
	if result.Ready || result.Status != "dry-run blocked" || result.BlockedCount != 1 || updates != 1 {
		t.Fatalf("unexpected PDB review: %+v updates=%d", result, updates)
	}
	if !strings.Contains(strings.Join(result.Limitations, " "), "PodDisruptionBudget") {
		t.Fatalf("PDB limitation was not surfaced: %+v", result.Limitations)
	}
}

// TestNodeDrainOptionsAllowExplicitSafeExceptions verifies ignored DaemonSets and emptyDir acknowledgement are opt-in.
func TestNodeDrainOptionsAllowExplicitSafeExceptions(t *testing.T) {
	controller := true
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "job", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "worker-a", Volumes: []corev1.Volume{{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	}
	plan := planNodeDrain(pods, "worker-a", NodeActionRequest{Action: "drain", IgnoreDaemonSets: true, DeleteEmptyDirData: true})
	if len(plan.blockers) != 0 || len(plan.evictable) != 1 || len(plan.impacts) != 2 {
		t.Fatalf("explicit safe exceptions were not applied: %+v", plan)
	}
}

// testNode creates a minimal versioned Node for typed fake-client tests.
func testNode(name, resourceVersion string) *corev1.Node {
	return &corev1.Node{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Node"}, ObjectMeta: metav1.ObjectMeta{Name: name, ResourceVersion: resourceVersion, ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "server"}}}, Spec: corev1.NodeSpec{Unschedulable: false}}
}

// TestNodeTaintValidationAcceptsEmptyValue verifies valueless taints remain valid Kubernetes input.
func TestNodeTaintValidationAcceptsEmptyValue(t *testing.T) {
	if err := validateNodeAction("worker-a", NodeActionRequest{Action: "add-taint", TaintKey: "example.com/maintenance", TaintEffect: string(corev1.TaintEffectNoExecute)}); err != nil {
		t.Fatalf("valueless taint was rejected: %v", err)
	}
}
