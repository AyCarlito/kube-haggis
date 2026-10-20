package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kube-haggis/kube-haggis/internal/audit"
	"github.com/kube-haggis/kube-haggis/internal/kube"
	"net/http"
	"net/http/httptest"
)

// TestHandleWorkloadActionRequiresWriteMode verifies targeted workload mutations honor the global write switch.
func TestHandleWorkloadActionRequiresWriteMode(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/workload/action?resource=deployments", strings.NewReader(`{"action":"restart"}`))
	response := httptest.NewRecorder()

	server.handleWorkloadAction(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "write mode") {
		t.Fatalf("expected write-mode rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandleWorkloadActionRunsSeamAndRedactsAuditDetails verifies handler dispatch and mutation history safety.
func TestHandleWorkloadActionRunsSeamAndRedactsAuditDetails(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	called := false
	server := &Server{
		audit: store,
		workloadAction: func(_ context.Context, contextName, group, version, resource, namespace, name string, request kube.WorkloadActionRequest) (kube.WorkloadActionResult, error) {
			called = true
			if contextName != "field" || group != "apps" || version != "v1" || resource != "deployments" || namespace != "default" || name != "api" || request.Action != "image" || !request.DryRun || request.Image != "private:secret-value" {
				t.Fatalf("unexpected workload dispatch: %q %q %q %q %q %q %+v", contextName, group, version, resource, namespace, name, request)
			}
			return kube.WorkloadActionResult{Action: "image", Resource: "deployments", Namespace: "default", Name: "api", DryRun: true, Status: "dry-run passed"}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/workload/action?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api", strings.NewReader(`{"action":"image","dryRun":true,"container":"app","image":"private:secret-value"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleWorkloadAction(response, request)

	if response.Code != http.StatusOK || !called {
		t.Fatalf("workload action failed: %d %s called=%t", response.Code, response.Body.String(), called)
	}
	var result kube.WorkloadActionResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode workload result: %v", err)
	}
	if result.Status != "dry-run passed" {
		t.Fatalf("unexpected workload result: %+v", result)
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list workload audit: %v", err)
	}
	if len(entries) != 1 || entries[0].Operation != "workload-image" || !entries[0].DryRun || !entries[0].Success {
		t.Fatalf("unexpected workload audit entry: %+v", entries)
	}
	if strings.Contains(entries[0].Detail, "secret-value") || strings.Contains(entries[0].Detail, "private") {
		t.Fatalf("audit detail leaked request data: %q", entries[0].Detail)
	}
}

// TestHandleWorkloadActionRecordsRedactedFailure verifies RBAC failures are surfaced and safely audited.
func TestHandleWorkloadActionRecordsRedactedFailure(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	server := &Server{
		audit: store,
		workloadAction: func(context.Context, string, string, string, string, string, string, kube.WorkloadActionRequest) (kube.WorkloadActionResult, error) {
			return kube.WorkloadActionResult{}, errString("image was denied by Kubernetes RBAC for private:secret-value")
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/workload/action?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api", strings.NewReader(`{"action":"image","image":"private:secret-value"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleWorkloadAction(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "RBAC") {
		t.Fatalf("expected RBAC response, got %d %s", response.Code, response.Body.String())
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected failure audit entries: %v %+v", err, entries)
	}
	if strings.Contains(entries[0].Detail, "secret-value") || strings.Contains(entries[0].Detail, "private") {
		t.Fatalf("failure audit leaked request data: %q", entries[0].Detail)
	}
}
