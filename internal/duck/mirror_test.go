package duck

import (
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/store/sqlite"
)

// TestBuildSQLiteMirrorMulti materialises a multi-table .duckdb file as
// a SQLite database and verifies the result opens through the standard
// SQLite store with the same table names and row counts.
func TestBuildSQLiteMirrorMulti(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "mirror.db")
	if err := BuildSQLiteMirror(tinyDuck, dest); err != nil {
		t.Fatalf("BuildSQLiteMirror: %v", err)
	}
	store, err := sqlite.Open(dest)
	if err != nil {
		t.Fatalf("open mirror as sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()

	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d tables, want 2: %v", len(names), names)
	}

	people, err := store.TableRowCount("people")
	if err != nil {
		t.Fatalf("TableRowCount(people): %v", err)
	}
	if people != 3 {
		t.Errorf("people rows = %d, want 3", people)
	}

	extras, err := store.TableRowCount("extras")
	if err != nil {
		t.Fatalf("TableRowCount(extras): %v", err)
	}
	if extras != 2 {
		t.Errorf("extras rows = %d, want 2", extras)
	}
}

// TestBuildSQLiteMirrorEmpty handles a duckdb file with no tables —
// resulting SQLite file is empty (but valid).
func TestBuildSQLiteMirrorEmpty(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.duckdb")
	if err := CreateEmpty(src); err != nil {
		t.Fatalf("seed CreateEmpty: %v", err)
	}
	dest := filepath.Join(dir, "mirror.db")
	if err := BuildSQLiteMirror(src, dest); err != nil {
		t.Fatalf("BuildSQLiteMirror: %v", err)
	}
	store, err := sqlite.Open(dest)
	if err != nil {
		t.Fatalf("open empty mirror: %v", err)
	}
	defer func() { _ = store.Close() }()
	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected no tables in empty mirror, got %v", names)
	}
}
