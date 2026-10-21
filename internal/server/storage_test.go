package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/audit"
	"github.com/AyCarlito/kube-haggis/internal/kube"
)

// TestHandlePVCActionRequiresWriteMode verifies native PVC operations honor the application write switch.
func TestHandlePVCActionRequiresWriteMode(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/pvc/action?namespace=default&name=data", strings.NewReader(`{"action":"resize","dryRun":true}`))
	response := httptest.NewRecorder()

	server.handlePVCAction(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "write mode") {
		t.Fatalf("expected write-mode rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestHandlePVCActionDispatchesAndAuditsDryRun verifies native PVC dispatch and redacted audit outcomes.
func TestHandlePVCActionDispatchesAndAuditsDryRun(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	called := false
	server := &Server{
		audit: store,
		pvcAction: func(_ context.Context, contextName, namespace, name string, request kube.PVCActionRequest) (kube.PVCActionResult, error) {
			called = true
			if contextName != "field" || namespace != "default" || name != "data" || request.Action != "snapshot" || !request.DryRun || request.SnapshotName != "data-copy" || request.SnapshotClassName != "csi-class" {
				t.Fatalf("unexpected PVC dispatch: %q %q %q %+v", contextName, namespace, name, request)
			}
			return kube.PVCActionResult{Action: "snapshot", Resource: "persistentvolumeclaims", Namespace: namespace, Name: name, DryRun: true, SnapshotName: request.SnapshotName, Status: "dry-run passed"}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/pvc/action?context=field&namespace=default&name=data", strings.NewReader(`{"action":"snapshot","dryRun":true,"expectedResourceVersion":"7","snapshotName":"data-copy","snapshotClassName":"csi-class"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handlePVCAction(response, request)

	if response.Code != http.StatusOK || !called {
		t.Fatalf("PVC action failed: %d %s called=%t", response.Code, response.Body.String(), called)
	}
	var result kube.PVCActionResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode PVC result: %v", err)
	}
	if result.Status != "dry-run passed" || result.SnapshotName != "data-copy" {
		t.Fatalf("unexpected PVC result: %+v", result)
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list PVC audit: %v", err)
	}
	if len(entries) != 1 || entries[0].Operation != "pvc-snapshot" || !entries[0].DryRun || !entries[0].Success {
		t.Fatalf("unexpected PVC audit entry: %+v", entries)
	}
	if strings.Contains(entries[0].Detail, "data-copy") || strings.Contains(entries[0].Detail, "csi-class") {
		t.Fatalf("PVC audit detail leaked request data: %q", entries[0].Detail)
	}
}

// TestHandlePVCActionRedactsRBACFailure verifies permission errors remain actionable without request data in audit history.
func TestHandlePVCActionRedactsRBACFailure(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	server := &Server{
		audit: store,
		pvcAction: func(_ context.Context, _ string, _ string, _ string, request kube.PVCActionRequest) (kube.PVCActionResult, error) {
			if request.DryRun {
				return kube.PVCActionResult{Action: "snapshot", Resource: "persistentvolumeclaims", Namespace: "default", Name: "data", DryRun: true, SnapshotName: request.SnapshotName, SnapshotClassName: request.SnapshotClassName, Status: "dry-run passed"}, nil
			}
			return kube.PVCActionResult{}, errString("VolumeSnapshotClass csi-sensitive was denied by Kubernetes RBAC")
		},
	}
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/pvc/action?context=field&namespace=default&name=data", strings.NewReader(`{"action":"snapshot","dryRun":true,"expectedResourceVersion":"7","snapshotName":"sensitive-copy","snapshotClassName":"csi-sensitive"}`))
	reviewRequest.Header.Set("X-KubeHaggis-Write", "enabled")
	reviewResponse := httptest.NewRecorder()
	server.handlePVCAction(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK || reviewResponse.Header().Get(mutationReviewHeader) == "" {
		t.Fatalf("PVC review failed: status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/pvc/action?context=field&namespace=default&name=data", strings.NewReader(`{"action":"snapshot","confirmed":true,"expectedResourceVersion":"7","snapshotName":"sensitive-copy","snapshotClassName":"csi-sensitive"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	request.Header.Set(mutationReviewHeader, reviewResponse.Header().Get(mutationReviewHeader))
	response := httptest.NewRecorder()

	server.handlePVCAction(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "RBAC") {
		t.Fatalf("expected RBAC response, got %d %s", response.Code, response.Body.String())
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected PVC failure audit entries: %v %+v", err, entries)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Detail, "sensitive") || strings.Contains(entry.Detail, "csi") {
			t.Fatalf("PVC failure audit was unsafe: %+v", entries)
		}
	}
}

// TestHandlePVCOperationsPreservesCapabilityStates verifies absent snapshot APIs remain visible as a normal read response.
func TestHandlePVCOperationsPreservesCapabilityStates(t *testing.T) {
	called := false
	server := &Server{
		pvcOperations: func(_ context.Context, contextName, namespace, name string) (kube.PVCOperationInfo, error) {
			called = true
			if contextName != "field" || namespace != "default" || name != "data" {
				t.Fatalf("unexpected PVC capability coordinates: %q %q %q", contextName, namespace, name)
			}
			return kube.PVCOperationInfo{
				Name:      name,
				Namespace: namespace,
				Resize:    kube.PVCResizeCapability{State: "available"},
				Snapshot:  kube.VolumeSnapshotCapability{State: "unavailable", Message: "snapshot CRDs may not be installed"},
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/pvc/operations?context=field&namespace=default&name=data", nil)
	response := httptest.NewRecorder()

	server.handlePVCOperations(response, request)

	if response.Code != http.StatusOK || !called || !strings.Contains(response.Body.String(), "unavailable") || !strings.Contains(response.Body.String(), "CRDs") {
		t.Fatalf("capability state was not returned: %d %s called=%t", response.Code, response.Body.String(), called)
	}
}

// TestHandlePVCActionClassifiesUnsupportedSnapshotAPI verifies absent native APIs receive a clear HTTP status.
func TestHandlePVCActionClassifiesUnsupportedSnapshotAPI(t *testing.T) {
	server := &Server{
		pvcAction: func(context.Context, string, string, string, kube.PVCActionRequest) (kube.PVCActionResult, error) {
			return kube.PVCActionResult{}, errString("VolumeSnapshot API is not available on this Kubernetes API server")
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/pvc/action?namespace=default&name=data", strings.NewReader(`{"action":"snapshot","dryRun":true,"expectedResourceVersion":"7","snapshotName":"data-copy","snapshotClassName":"csi-class"}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handlePVCAction(response, request)

	if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "not available") {
		t.Fatalf("expected unsupported snapshot response, got %d %s", response.Code, response.Body.String())
	}
}
