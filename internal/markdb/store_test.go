package markdb

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenPragmas(t *testing.T) {
	s := testStore(t)

	var journalMode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var fk int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestInitSchema(t *testing.T) {
	s := testStore(t)
	if err := s.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	// Verify all tables exist.
	wantTables := []string{"_sources", "files", "links", "_schema"}
	for _, table := range wantTables {
		var name string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}

	// Verify FTS virtual table exists.
	var ftsName string
	err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='files_fts'",
	).Scan(&ftsName)
	if err != nil {
		t.Errorf("files_fts not found: %v", err)
	}
}

func TestInitSchemaIdempotent(t *testing.T) {
	s := testStore(t)
	if err := s.InitSchema(); err != nil {
		t.Fatalf("first InitSchema: %v", err)
	}
	if err := s.InitSchema(); err != nil {
		t.Fatalf("second InitSchema: %v", err)
	}
}

func TestAddDynamicColumns(t *testing.T) {
	s := testStore(t)
	if err := s.InitSchema(); err != nil {
		t.Fatal(err)
	}

	cols := []ColumnDef{
		{Key: "title", ColumnName: "title", InferredType: "text", FileCount: 3, Sample: "Hello"},
		{Key: "count", ColumnName: "count", InferredType: "integer", FileCount: 2, Sample: "42"},
		{Key: "pi", ColumnName: "pi", InferredType: "real", FileCount: 1, Sample: "3.14"},
		{Key: "tags", ColumnName: "tags", InferredType: "json", FileCount: 1, Sample: "[a,b]"},
	}

	if err := s.AddDynamicColumns(cols); err != nil {
		t.Fatalf("AddDynamicColumns: %v", err)
	}

	// Verify columns exist with correct types.
	rows, err := s.db.Query("PRAGMA table_info(files)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()

	colTypes := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		colTypes[name] = typ
	}

	wantTypes := map[string]string{
		"title": "TEXT",
		"count": "INTEGER",
		"pi":    "REAL",
		"tags":  "TEXT", // json maps to TEXT
	}
	for col, wantType := range wantTypes {
		if gotType, ok := colTypes[col]; !ok {
			t.Errorf("column %q not found", col)
		} else if gotType != wantType {
			t.Errorf("column %q type = %q, want %q", col, gotType, wantType)
		}
	}
}

func TestAddDynamicColumnsIdempotent(t *testing.T) {
	s := testStore(t)
	if err := s.InitSchema(); err != nil {
		t.Fatal(err)
	}

	cols := []ColumnDef{
		{Key: "title", ColumnName: "title", InferredType: "text", FileCount: 1},
	}
	if err := s.AddDynamicColumns(cols); err != nil {
		t.Fatal(err)
	}
	// Second call should not error.
	if err := s.AddDynamicColumns(cols); err != nil {
		t.Fatalf("second AddDynamicColumns: %v", err)
	}
}

func TestAddDynamicColumnsPopulatesSchema(t *testing.T) {
	s := testStore(t)
	if err := s.InitSchema(); err != nil {
		t.Fatal(err)
	}

	cols := []ColumnDef{
		{Key: "title", ColumnName: "title", InferredType: "text", FileCount: 5, Sample: "My Post"},
	}
	if err := s.AddDynamicColumns(cols); err != nil {
		t.Fatal(err)
	}

	var key, colName, inferredType, sample string
	var fileCount int
	err := s.db.QueryRow(
		"SELECT key, column_name, inferred_type, file_count, sample FROM _schema WHERE key = 'title'",
	).Scan(&key, &colName, &inferredType, &fileCount, &sample)
	if err != nil {
		t.Fatalf("query _schema: %v", err)
	}
	if colName != "title" || inferredType != "text" || fileCount != 5 || sample != "My Post" {
		t.Errorf("_schema row = (%q, %q, %d, %q), want (title, text, 5, My Post)",
			colName, inferredType, fileCount, sample)
	}
}
