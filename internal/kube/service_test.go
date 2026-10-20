package kube

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

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
