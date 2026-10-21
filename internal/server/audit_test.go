package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AyCarlito/kube-haggis/internal/audit"
)

// TestHandleAuditReturnsSafeRedactedEntries verifies persisted error text cannot reach the browser.
func TestHandleAuditReturnsSafeRedactedEntries(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	if err := store.Record(context.Background(), audit.Entry{
		CreatedAt: time.Now().UTC(),
		Context:   "field",
		Operation: "apply",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "default",
		Name:      "credentials",
		Detail:    "Secret value private-value was denied by Kubernetes RBAC",
	}); err != nil {
		t.Fatalf("record unsafe test entry: %v", err)
	}

	response := httptest.NewRecorder()
	server := &Server{audit: store}
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/audit?limit=10", nil))

	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-value") || strings.Contains(response.Body.String(), "Secret value") {
		t.Fatalf("audit response exposed unsafe detail: status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []audit.Entry `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Detail != "operation denied by Kubernetes RBAC" {
		t.Fatalf("unexpected safe audit payload: %+v", payload.Items)
	}
}

// TestHandleAuditReportsUnavailableStore verifies the read-only API exposes a safe disconnected state.
func TestHandleAuditReportsUnavailableStore(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).handleAudit(response, httptest.NewRequest(http.MethodGet, "/api/audit", nil))

	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "local mutation history is unavailable") {
		t.Fatalf("unexpected unavailable audit response: status=%d body=%s", response.Code, response.Body.String())
	}
}
