// Package audit stores redacted local mutation history for KubeHaggis.
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Entry is a redacted record of one Kubernetes mutation attempt.
type Entry struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Context   string    `json:"context"`
	Operation string    `json:"operation"`
	Group     string    `json:"group"`
	Version   string    `json:"version"`
	Resource  string    `json:"resource"`
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name,omitempty"`
	DryRun    bool      `json:"dryRun"`
	Success   bool      `json:"success"`
	Detail    string    `json:"detail,omitempty"`
}

// Store manages the local SQLite database used for audit entries.
type Store struct {
	database *sql.DB
}

// Open creates or opens a SQLite audit database at the supplied path.
func Open(path string) (*Store, error) {
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create audit directory: %w", err)
		}
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open audit database %q: %w", path, err)
	}
	store := &Store{database: database}
	if err := store.initialize(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the underlying SQLite database connection.
func (s *Store) Close() error {
	if s == nil || s.database == nil {
		return nil
	}
	return s.database.Close()
}

// Record appends one redacted mutation entry to the audit database.
func (s *Store) Record(ctx context.Context, entry Entry) error {
	if s == nil || s.database == nil {
		return nil
	}
	_, err := s.database.ExecContext(ctx, `
		INSERT INTO mutations (created_at, context_name, operation, api_group, api_version, resource, namespace_name, resource_name, dry_run, success, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.CreatedAt.UTC().Format(time.RFC3339Nano), entry.Context, entry.Operation, entry.Group, entry.Version, entry.Resource, entry.Namespace, entry.Name, boolInt(entry.DryRun), boolInt(entry.Success), entry.Detail)
	if err != nil {
		return fmt.Errorf("record mutation: %w", err)
	}
	return nil
}

// List returns the newest redacted mutation entries first.
func (s *Store) List(ctx context.Context, limit int) ([]Entry, error) {
	if s == nil || s.database == nil {
		return []Entry{}, nil
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, created_at, context_name, operation, api_group, api_version, resource, namespace_name, resource_name, dry_run, success, detail
		FROM mutations ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list mutation history: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		var entry Entry
		var createdAt string
		var dryRun, success int
		if err := rows.Scan(&entry.ID, &createdAt, &entry.Context, &entry.Operation, &entry.Group, &entry.Version, &entry.Resource, &entry.Namespace, &entry.Name, &dryRun, &success, &entry.Detail); err != nil {
			return nil, fmt.Errorf("scan mutation history: %w", err)
		}
		entry.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse mutation timestamp: %w", err)
		}
		entry.DryRun = dryRun != 0
		entry.Success = success != 0
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mutation history: %w", err)
	}
	return entries, nil
}

// initialize creates the audit table and its chronological index.
func (s *Store) initialize() error {
	if _, err := s.database.Exec(`
		CREATE TABLE IF NOT EXISTS mutations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			context_name TEXT NOT NULL,
			operation TEXT NOT NULL,
			api_group TEXT NOT NULL,
			api_version TEXT NOT NULL,
			resource TEXT NOT NULL,
			namespace_name TEXT NOT NULL DEFAULT '',
			resource_name TEXT NOT NULL DEFAULT '',
			dry_run INTEGER NOT NULL DEFAULT 0,
			success INTEGER NOT NULL DEFAULT 0,
			detail TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS mutations_created_at_idx ON mutations (created_at DESC);
	`); err != nil {
		return fmt.Errorf("initialize audit database: %w", err)
	}
	return nil
}

// boolInt converts a Go boolean to SQLite's integer representation.
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
