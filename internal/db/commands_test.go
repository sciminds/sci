package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/sci/internal/store/sqlite"
)

// TestOpenStore checks that openStore dispatches on file extension: .db opens
// the SQLite store, a flat file opens a read-only file view, and a missing
// path errors before any backend is touched.
func TestOpenStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A real SQLite database.
	dbPath := filepath.Join(dir, "test.db")
	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}
	if err := st.CreateEmptyTable("people"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	_ = st.Close()

	// A flat CSV file → opens as a read-only file view.
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,value\nAda,1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		ds, err := openStore(dbPath)
		if err != nil {
			t.Fatalf("openStore(.db): %v", err)
		}
		defer func() { _ = ds.Close() }()
		names, err := ds.TableNames()
		if err != nil {
			t.Fatalf("TableNames: %v", err)
		}
		if len(names) != 1 || names[0] != "people" {
			t.Errorf("tables = %v, want [people]", names)
		}
	})

	t.Run("file view", func(t *testing.T) {
		t.Parallel()
		ds, err := openStore(csvPath)
		if err != nil {
			t.Fatalf("openStore(.csv): %v", err)
		}
		defer func() { _ = ds.Close() }()
		names, err := ds.TableNames()
		if err != nil {
			t.Fatalf("TableNames: %v", err)
		}
		if len(names) != 1 {
			t.Errorf("tables = %v, want one table", names)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		t.Parallel()
		if _, err := openStore(filepath.Join(dir, "nope.db")); err == nil {
			t.Fatal("expected error for missing path, got nil")
		}
	})
}
