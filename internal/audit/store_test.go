package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreRoundTrip verifies that mutation history survives a SQLite round trip.
func TestStoreRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()

	want := Entry{
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		Context:   "field",
		Operation: "apply",
		Group:     "apps",
		Version:   "v1",
		Resource:  "deployments",
		Namespace: "default",
		Name:      "api",
		DryRun:    true,
		Success:   true,
		Detail:    "server dry-run passed",
	}
	if err := store.Record(context.Background(), want); err != nil {
		t.Fatalf("record audit entry: %v", err)
	}

	entries, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list audit entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(entries))
	}
	got := entries[0]
	if got.Context != want.Context || got.Operation != want.Operation || got.Resource != want.Resource || !got.DryRun || !got.Success {
		t.Fatalf("unexpected audit entry: %+v", got)
	}
}
