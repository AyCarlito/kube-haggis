package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/audit"
	"github.com/AyCarlito/kube-haggis/internal/kube"
	"net/http"
	"net/http/httptest"
)

// TestHandleNodeActionRequiresWriteMode verifies Node mutations honor the application write switch.
func TestHandleNodeActionRequiresWriteMode(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/node/action?name=worker-a", strings.NewReader(`{"action":"cordon"}`))
	response := httptest.NewRecorder()

	server.handleNodeAction(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "write mode") {
		t.Fatalf("expected write-mode rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandleNodeActionDispatchesAndRedactsAudit verifies Node dispatch and taint-value audit safety.
func TestHandleNodeActionDispatchesAndRedactsAudit(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	called := false
	server := &Server{
		audit: store,
		nodeAction: func(_ context.Context, contextName, name string, request kube.NodeActionRequest) (kube.NodeActionResult, error) {
			called = true
			if contextName != "field" || name != "worker-a" || request.Action != "add-taint" || !request.DryRun || request.TaintValue != "sensitive-looking-value" {
				t.Fatalf("unexpected Node dispatch: %q %q %+v", contextName, name, request)
			}
			return kube.NodeActionResult{Action: "add-taint", Resource: "nodes", Name: name, DryRun: true, Ready: true, Status: "dry-run passed"}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/node/action?context=field&name=worker-a", strings.NewReader(`{"action":"add-taint","dryRun":true,"taintKey":"example.com/pool","taintValue":"sensitive-looking-value","taintEffect":"NoSchedule"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleNodeAction(response, request)

	if response.Code != http.StatusOK || !called {
		t.Fatalf("Node action failed: %d %s called=%t", response.Code, response.Body.String(), called)
	}
	var result kube.NodeActionResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode Node result: %v", err)
	}
	if result.Status != "dry-run passed" || !result.Ready {
		t.Fatalf("unexpected Node result: %+v", result)
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list Node audit: %v", err)
	}
	if len(entries) != 1 || entries[0].Operation != "node-add-taint" || !entries[0].DryRun || !entries[0].Success {
		t.Fatalf("unexpected Node audit entry: %+v", entries)
	}
	if strings.Contains(entries[0].Detail, "sensitive-looking-value") || strings.Contains(entries[0].Detail, "example.com") {
		t.Fatalf("Node audit detail leaked request data: %q", entries[0].Detail)
	}
}

// TestHandleNodeActionReturnsRBACErrors verifies permission failures remain actionable and redacted.
func TestHandleNodeActionReturnsRBACErrors(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	server := &Server{
		audit: store,
		nodeAction: func(_ context.Context, _ string, _ string, request kube.NodeActionRequest) (kube.NodeActionResult, error) {
			if request.DryRun {
				return kube.NodeActionResult{Action: "cordon", Resource: "nodes", Name: "worker-a", DryRun: true, Ready: true, Status: "dry-run passed"}, nil
			}
			return kube.NodeActionResult{}, errString("cordon was denied by Kubernetes RBAC for sensitive-looking-value")
		},
	}
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/node/action?context=field&name=worker-a", strings.NewReader(`{"action":"cordon","dryRun":true,"expectedResourceVersion":"7"}`))
	reviewRequest.Header.Set("X-KubeHaggis-Write", "enabled")
	reviewResponse := httptest.NewRecorder()
	server.handleNodeAction(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK || reviewResponse.Header().Get(mutationReviewHeader) == "" {
		t.Fatalf("Node review failed: status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/node/action?context=field&name=worker-a", strings.NewReader(`{"action":"cordon","expectedResourceVersion":"7"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	request.Header.Set(mutationReviewHeader, reviewResponse.Header().Get(mutationReviewHeader))
	response := httptest.NewRecorder()

	server.handleNodeAction(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "RBAC") {
		t.Fatalf("expected RBAC response, got %d %s", response.Code, response.Body.String())
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected Node failure audit entries: %v %+v", err, entries)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Detail, "sensitive-looking-value") || strings.Contains(entry.Detail, "secret") {
			t.Fatalf("Node failure audit was unsafe: %+v", entries)
		}
	}
}

// TestHandleNodeActionReturnsBlockedDrainReview verifies safe drain blockers are returned for UI review.
func TestHandleNodeActionReturnsBlockedDrainReview(t *testing.T) {
	server := &Server{
		nodeAction: func(context.Context, string, string, kube.NodeActionRequest) (kube.NodeActionResult, error) {
			return kube.NodeActionResult{Action: "drain", Resource: "nodes", Name: "worker-a", DryRun: true, Status: "dry-run blocked", Limitations: []string{"default/unmanaged: force deletion is not supported"}}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/node/action?name=worker-a", strings.NewReader(`{"action":"drain","dryRun":true}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleNodeAction(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "dry-run blocked") || !strings.Contains(response.Body.String(), "unmanaged") {
		t.Fatalf("blocked drain review was not returned: %d %s", response.Code, response.Body.String())
	}
}
