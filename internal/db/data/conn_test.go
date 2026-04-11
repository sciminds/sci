package data

import (
	"path/filepath"
	"testing"
)

func TestOpenMemory(t *testing.T) {
	t.Parallel()
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var n int
	if err := db.NewQuery("SELECT 42").Row(&n); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if n != 42 {
		t.Errorf("got %d, want 42", n)
	}
}

func TestOpenFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.NewQuery("CREATE TABLE t (x INTEGER)").Execute()
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	_, err = db.NewQuery("INSERT INTO t VALUES (7)").Execute()
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var x int
	if err := db.NewQuery("SELECT x FROM t").Row(&x); err != nil {
		t.Fatalf("select: %v", err)
	}
	if x != 7 {
		t.Errorf("got %d, want 7", x)
	}
}

func TestConcurrentReads(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Create a table with some rows.
	if _, err := db.NewQuery("CREATE TABLE nums (n INTEGER)").Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewQuery(`INSERT INTO nums
		WITH RECURSIVE cnt(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM cnt WHERE n < 999)
		SELECT n FROM cnt`).Execute(); err != nil {
		t.Fatal(err)
	}

	// Run concurrent reads — would deadlock with MaxOpenConns(1).
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			var count int
			errs <- db.NewQuery("SELECT COUNT(*) FROM nums").Row(&count)
		}()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent read failed: %v", err)
		}
	}
}

func TestMmapPragmaSet(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mmap.db")
	db, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var mmapSize int64
	if err := db.NewQuery("PRAGMA mmap_size").Row(&mmapSize); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmapSize == 0 {
		t.Error("mmap_size is 0, want > 0")
	}
}
