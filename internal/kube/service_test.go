package kube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// newMetricsDynamicClient creates a dynamic fake with list kinds for metrics.k8s.io resources.
func newMetricsDynamicClient(objects ...runtime.Object) *fake.FakeDynamicClient {
	listKinds := map[schema.GroupVersionResource]string{
		metricsGVR("nodes"): "NodeMetricsList",
		metricsGVR("pods"):  "PodMetricsList",
	}
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
}

// addMetricsListReactors makes the dynamic fake return metrics objects for the native resource names.
func addMetricsListReactors(client *fake.FakeDynamicClient, nodes, pods []unstructured.Unstructured) {
	client.PrependReactor("list", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{Items: nodes}, nil
	})
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{Items: pods}, nil
	})
}

// TestSummarizeContext verifies that kubeconfig identity is safe for the UI.
func TestSummarizeContext(t *testing.T) {
	config := &clientcmdapi.Config{
		CurrentContext: "field",
		Contexts: map[string]*clientcmdapi.Context{
			"field": {Cluster: "cluster-a", AuthInfo: "operator"},
		},
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster-a": {Server: "https://api.example.test:6443"},
		},
	}

	summary := summarizeContext(config, "field")
	if summary.Server != "https://api.example.test:6443" || !summary.IsCurrent || summary.User != "operator" {
		t.Fatalf("unexpected context summary: %+v", summary)
	}
}

// TestSummarizeContextRedactsServerUserInfo verifies context summaries cannot echo URL credentials.
func TestSummarizeContextRedactsServerUserInfo(t *testing.T) {
	config := &clientcmdapi.Config{
		Contexts: map[string]*clientcmdapi.Context{
			"field": {Cluster: "cluster-a", AuthInfo: "operator"},
		},
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster-a": {Server: "https://operator:secret@example.test:6443"},
		},
	}

	summary := summarizeContext(config, "field")
	if summary.Server != "https://example.test:6443" || strings.Contains(summary.Server, "secret") {
		t.Fatalf("server URL credentials were not redacted: %q", summary.Server)
	}
}

// TestBuildNodeOverview verifies ready, pressure, and cordon counters.
func TestBuildNodeOverview(t *testing.T) {
	items := []unstructured.Unstructured{
		{Object: map[string]interface{}{
			"spec": map[string]interface{}{"unschedulable": true},
			"status": map[string]interface{}{"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True"},
				map[string]interface{}{"type": "MemoryPressure", "status": "True"},
			}},
		}},
		{Object: map[string]interface{}{
			"status": map[string]interface{}{"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "False"},
			}},
		}},
	}

	overview := buildNodeOverview(items)
	if overview.Total != 2 || overview.Ready != 1 || overview.NotReady != 1 || overview.Cordoned != 1 || overview.Pressure != 1 {
		t.Fatalf("unexpected node overview: %+v", overview)
	}
}

// TestBuildPodOverview verifies Pod phase and restart aggregation.
func TestBuildPodOverview(t *testing.T) {
	items := []unstructured.Unstructured{
		{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"phase":             "Running",
				"containerStatuses": []interface{}{map[string]interface{}{"restartCount": int64(2)}},
			},
		}},
		{Object: map[string]interface{}{"status": map[string]interface{}{"phase": "Pending"}}},
		{Object: map[string]interface{}{"status": map[string]interface{}{"phase": "Failed"}}},
	}

	overview := buildPodOverview(items)
	if overview.Total != 3 || overview.Running != 1 || overview.Pending != 1 || overview.Failed != 1 || overview.Restarting != 1 {
		t.Fatalf("unexpected Pod overview: %+v", overview)
	}
}

// TestBuildWarningEvents verifies warning selection and timestamp ordering.
func TestBuildWarningEvents(t *testing.T) {
	items := []unstructured.Unstructured{
		{Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": "backoff-event", "namespace": "default"},
			"type":     "Normal",
			"reason":   "Scheduled",
		}},
		{Object: map[string]interface{}{
			"metadata":       map[string]interface{}{"name": "backoff-event", "namespace": "default"},
			"type":           "Warning",
			"reason":         "BackOff",
			"message":        "container restart back-off",
			"lastTimestamp":  time.Now().UTC().Format(time.RFC3339),
			"involvedObject": map[string]interface{}{"kind": "Pod", "name": "api-0"},
		}},
	}

	warnings := buildWarningEvents(items)
	if len(warnings) != 1 || warnings[0].Reason != "BackOff" || warnings[0].Object != "Pod/api-0" {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
}

// TestBuildEventSummaries verifies native Event fields used by resource detail pages.
func TestBuildEventSummaries(t *testing.T) {
	items := []unstructured.Unstructured{
		{Object: map[string]interface{}{
			"metadata":       map[string]interface{}{"name": "backoff-event", "namespace": "default"},
			"type":           "Warning",
			"reason":         "BackOff",
			"message":        "container restart back-off",
			"count":          int64(3),
			"firstTimestamp": "2026-10-20T10:00:00Z",
			"lastTimestamp":  "2026-10-20T10:05:00Z",
			"involvedObject": map[string]interface{}{"kind": "Pod", "name": "api-0"},
		}},
		{Object: map[string]interface{}{
			"type":           "Normal",
			"reason":         "Pulled",
			"message":        "image pulled",
			"involvedObject": map[string]interface{}{"kind": "Pod", "name": "api-0"},
		}},
	}

	events := buildEventSummaries(items, false)
	if len(events) != 2 || events[0].Name != "backoff-event" || events[0].Count != 3 || events[0].Object != "Pod/api-0" || events[0].LastSeen.IsZero() {
		t.Fatalf("unexpected event summaries: %+v", events)
	}
	warnings := buildEventSummaries(items, true)
	if len(warnings) != 1 || warnings[0].Type != "Warning" {
		t.Fatalf("unexpected warning summaries: %+v", warnings)
	}
}

// TestParseNodeMetricsNormalizesQuantities verifies CPU, memory, and capacity percentages.
func TestParseNodeMetricsNormalizesQuantities(t *testing.T) {
	timestamp := "2026-10-20T10:05:00Z"
	items := []unstructured.Unstructured{{Object: map[string]interface{}{
		"metadata":  map[string]interface{}{"name": "worker-a"},
		"timestamp": timestamp,
		"usage":     map[string]interface{}{"cpu": "500m", "memory": "1Gi"},
	}}}
	capacities := map[string]MetricUsage{"worker-a": {CPUCores: 2, MemoryBytes: 2 * 1024 * 1024 * 1024}}

	parsed, updatedAt, err := parseNodeMetrics(items, capacities)
	if err != nil {
		t.Fatalf("parse node metrics: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Name != "worker-a" {
		t.Fatalf("unexpected node metrics: %+v", parsed)
	}
	if parsed[0].Usage.CPUCores != 0.5 || parsed[0].Usage.MemoryBytes != 1024*1024*1024 {
		t.Fatalf("unexpected normalized usage: %+v", parsed[0].Usage)
	}
	if parsed[0].CPUPercent != 25 || parsed[0].MemoryPercent != 50 {
		t.Fatalf("unexpected utilization: %+v", parsed[0])
	}
	if updatedAt.Format(time.RFC3339) != timestamp {
		t.Fatalf("unexpected timestamp: %s", updatedAt.Format(time.RFC3339))
	}
}

// TestParsePodMetricsAggregatesContainers verifies one Pod row includes every container.
func TestParsePodMetricsAggregatesContainers(t *testing.T) {
	items := []unstructured.Unstructured{{Object: map[string]interface{}{
		"metadata":  map[string]interface{}{"name": "api-7d9f", "namespace": "default"},
		"timestamp": "2026-10-20T10:05:00Z",
		"containers": []interface{}{
			map[string]interface{}{"name": "app", "usage": map[string]interface{}{"cpu": "250m", "memory": "128Mi"}},
			map[string]interface{}{"name": "proxy", "usage": map[string]interface{}{"cpu": "750m", "memory": "256Mi"}},
		},
	}}}

	parsed, _, err := parsePodMetrics(items)
	if err != nil {
		t.Fatalf("parse Pod metrics: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Namespace != "default" || parsed[0].Name != "api-7d9f" {
		t.Fatalf("unexpected Pod metric: %+v", parsed)
	}
	if parsed[0].Usage.CPUCores != 1 || parsed[0].Usage.MemoryBytes != 384*1024*1024 {
		t.Fatalf("containers were not aggregated: %+v", parsed[0].Usage)
	}
}

// TestCollectMetricsUsesDynamicMetricsAPI verifies native node and Pod collection and stale classification.
func TestCollectMetricsUsesDynamicMetricsAPI(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 10, 0, 0, time.UTC)
	nodeMetrics := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "NodeMetrics",
		"metadata":   map[string]interface{}{"name": "worker-a"},
		"timestamp":  now.Add(-time.Minute).Format(time.RFC3339),
		"usage":      map[string]interface{}{"cpu": "500m", "memory": "1Gi"},
	}}
	podMetrics := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "PodMetrics",
		"metadata":   map[string]interface{}{"name": "api-7d9f", "namespace": "default"},
		"timestamp":  now.Add(-time.Minute).Format(time.RFC3339),
		"containers": []interface{}{map[string]interface{}{"name": "app", "usage": map[string]interface{}{"cpu": "250m", "memory": "128Mi"}}},
	}}
	client := newMetricsDynamicClient(nodeMetrics, podMetrics)
	addMetricsListReactors(client, []unstructured.Unstructured{*nodeMetrics}, []unstructured.Unstructured{*podMetrics})
	nodes := []unstructured.Unstructured{{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "worker-a"},
		"status":   map[string]interface{}{"capacity": map[string]interface{}{"cpu": "2", "memory": "2Gi"}},
	}}}

	overview := collectMetrics(context.Background(), client, nodes, now)
	if overview.State != metricsStateAvailable || overview.NodeState != metricsStateAvailable || overview.PodState != metricsStateAvailable {
		t.Fatalf("unexpected metrics states: %+v", overview)
	}
	if overview.NodeCount != 1 || overview.PodCount != 1 || len(overview.Nodes) != 1 || len(overview.Pods) != 1 {
		t.Fatalf("unexpected metrics counts: %+v", overview)
	}
}

// TestCollectMetricsClassifiesPermissionDenied verifies RBAC failures remain visible in the response.
func TestCollectMetricsClassifiesPermissionDenied(t *testing.T) {
	client := newMetricsDynamicClient()
	denied := func(action clienttesting.Action) (bool, runtime.Object, error) {
		resourceName := action.GetResource().Resource
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: resourceName}, "metrics", nil)
	}
	client.PrependReactor("list", "nodes", denied)
	client.PrependReactor("list", "pods", denied)

	overview := collectMetrics(context.Background(), client, nil, time.Now().UTC())
	if overview.State != metricsStatePermissionDenied || overview.NodeState != metricsStatePermissionDenied || overview.PodState != metricsStatePermissionDenied {
		t.Fatalf("permission denial was not classified: %+v", overview)
	}
}

// TestCollectMetricsClassifiesTransportError verifies non-RBAC collection failures become an explicit error state.
func TestCollectMetricsClassifiesTransportError(t *testing.T) {
	client := newMetricsDynamicClient()
	client.PrependReactor("list", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("metrics API transport failed")
	})

	overview := collectMetrics(context.Background(), client, nil, time.Now().UTC())
	if overview.State != metricsStateError || overview.NodeState != metricsStateError || !strings.Contains(overview.NodeMessage, "transport failed") {
		t.Fatalf("transport error was not classified: %+v", overview)
	}
}

// TestCollectMetricsClassifiesEmptyAndStaleResponses verifies non-error metrics states are explicit.
func TestCollectMetricsClassifiesEmptyAndStaleResponses(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 10, 0, 0, time.UTC)
	empty := collectMetrics(context.Background(), newMetricsDynamicClient(), nil, now)
	if empty.State != metricsStateEmpty || empty.NodeState != metricsStateEmpty || empty.PodState != metricsStateEmpty {
		t.Fatalf("empty metrics response was not classified: %+v", empty)
	}

	old := now.Add(-(metricsStaleAfter + time.Minute)).Format(time.RFC3339)
	node := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "NodeMetrics",
		"metadata":   map[string]interface{}{"name": "worker-a"},
		"timestamp":  old,
		"usage":      map[string]interface{}{"cpu": "1", "memory": "1Gi"},
	}}
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "PodMetrics",
		"metadata":   map[string]interface{}{"name": "api", "namespace": "default"},
		"timestamp":  old,
		"containers": []interface{}{map[string]interface{}{"name": "app", "usage": map[string]interface{}{"cpu": "1", "memory": "1Gi"}}},
	}}
	client := newMetricsDynamicClient(node, pod)
	addMetricsListReactors(client, []unstructured.Unstructured{*node}, []unstructured.Unstructured{*pod})
	stale := collectMetrics(context.Background(), client, nil, now)
	if stale.State != metricsStateStale || stale.NodeState != metricsStateStale || stale.PodState != metricsStateStale {
		t.Fatalf("stale metrics response was not classified: %+v", stale)
	}
}

// TestParseMetricUsageRejectsMalformedQuantity verifies malformed metrics cannot reach the chart model.
func TestParseMetricUsageRejectsMalformedQuantity(t *testing.T) {
	_, err := parseMetricUsage(map[string]interface{}{"usage": map[string]interface{}{"cpu": "not-a-quantity", "memory": "1Gi"}}, "node \"worker-a\"")
	if err == nil || !strings.Contains(err.Error(), "parse node") {
		t.Fatalf("expected malformed quantity error, got %v", err)
	}
}

// TestStreamPodSessionValidation verifies that invalid terminal requests stop before client creation.
func TestStreamPodSessionValidation(t *testing.T) {
	service := NewService("")
	tests := []struct {
		name      string
		namespace string
		pod       string
		mode      PodSessionMode
		command   []string
		want      string
	}{
		{name: "missing namespace", pod: "api", mode: PodSessionExec, command: []string{"/bin/sh"}, want: "namespace is required"},
		{name: "missing pod", namespace: "default", mode: PodSessionExec, command: []string{"/bin/sh"}, want: "Pod name is required"},
		{name: "missing command", namespace: "default", pod: "api", mode: PodSessionExec, want: "command is required"},
		{name: "unsupported mode", namespace: "default", pod: "api", mode: PodSessionMode("shell"), want: "unsupported Pod session mode"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := service.StreamPodSession(context.Background(), "", test.namespace, test.pod, "", test.mode, test.command, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// TestDeleteResourceUsesDryRunAndResourceVersion verifies generic deletes carry Kubernetes safety options.
func TestDeleteResourceUsesDryRunAndResourceVersion(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":            "api",
			"namespace":       "default",
			"resourceVersion": "7",
		},
	}})

	if err := deleteResource(context.Background(), client, gvr, "default", "api", true, ResourceDeleteOptions{DryRun: true, Force: true, ExpectedResourceVersion: "7"}); err != nil {
		t.Fatalf("delete resource: %v", err)
	}
	if len(client.Actions()) != 1 {
		t.Fatalf("expected one delete action, got %d", len(client.Actions()))
	}
	deleteAction, ok := client.Actions()[0].(clienttesting.DeleteAction)
	if !ok {
		t.Fatalf("expected delete action, got %T", client.Actions()[0])
	}
	options := deleteAction.GetDeleteOptions()
	if len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll || options.GracePeriodSeconds == nil || *options.GracePeriodSeconds != 0 {
		t.Fatalf("delete options lost dry-run or force behavior: %+v", options)
	}
	if options.Preconditions == nil || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "7" {
		t.Fatalf("delete options lost resource-version precondition: %+v", options.Preconditions)
	}
}

// TestDeleteResourceRequiresNamespace verifies namespaced generic deletes cannot accidentally target every namespace.
func TestDeleteResourceRequiresNamespace(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	err := deleteResource(context.Background(), client, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, "all", "credentials", true, ResourceDeleteOptions{ExpectedResourceVersion: "9"})
	if err == nil || !strings.Contains(err.Error(), "namespace is required") {
		t.Fatalf("expected namespace guard, got %v", err)
	}
	if len(client.Actions()) != 0 {
		t.Fatalf("namespace validation should happen before Kubernetes calls: %v", client.Actions())
	}
}
