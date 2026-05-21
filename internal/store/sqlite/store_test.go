package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/store"
)

// Shared DataStore-iface assertions live in internal/store/contracttest and
// are wired up via [TestStoreContract] in contract_test.go. The tests here
// cover SQLite-specific behaviour only (mmap pragma, concurrent-read pool
// sizing, BLOB rendering, rowid semantics, …).

// testDB is the canonical SQLite fixture (4 tables: equipment, projects,
// publications, researchers). Tests that mutate it must use copyFixture.
const testDB = "testdata/test.db"

// copyFixture copies the named fixture from testdata/ into a t.TempDir
// and returns the writable copy's path.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	tmp := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	return tmp
}

// ---------- open / pool / pragmas ----------

func TestOpen(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
}

func TestOpenMemory(t *testing.T) {
	t.Parallel()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = s.Close() }()

	var n int
	if err := s.db.QueryRow("SELECT 42").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 42 {
		t.Errorf("got %d, want 42", n)
	}
}

func TestMmapPragmaSet(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mmap.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	var mmapSize int64
	if err := s.db.QueryRow("PRAGMA mmap_size").Scan(&mmapSize); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmapSize == 0 {
		t.Error("mmap_size is 0, want > 0")
	}
}

func TestConcurrentReads(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "concurrent.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.db.Exec("CREATE TABLE nums (n INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO nums
		WITH RECURSIVE cnt(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM cnt WHERE n < 999)
		SELECT n FROM cnt`); err != nil {
		t.Fatal(err)
	}

	// 4 concurrent reads — would deadlock with MaxOpenConns(1).
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			var count int
			errs <- s.db.QueryRow("SELECT COUNT(*) FROM nums").Scan(&count)
		}()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent read failed: %v", err)
		}
	}
}

func TestStoreInterface(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	var _ store.DataStore = s
	var _ store.ViewLister = s
	var _ store.VirtualLister = s
}

// ---------- ReadOnlyQuery edge cases (sqlite-specific error messages and CTE handling) ----------

func TestReadOnlyQueryMaxRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "maxrows.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Exec("CREATE TABLE big(id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 250; i++ {
		if _, err := s.Exec(fmt.Sprintf("INSERT INTO big(id, val) VALUES (%d, 'row%d')", i, i)); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	_, rows, err := s.ReadOnlyQuery("SELECT * FROM big")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(rows) != 200 {
		t.Errorf("expected 200 rows (cap), got %d", len(rows))
	}
}

func TestReadOnlyQueryEmptyQuery(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, err = s.ReadOnlyQuery("")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "empty query") {
		t.Errorf("error %q should mention 'empty query'", err.Error())
	}
}

func TestReadOnlyQuerySemicolon(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, err = s.ReadOnlyQuery("SELECT 1; DROP TABLE foo")
	if err == nil {
		t.Fatal("expected error for multi-statement query")
	}
	if !strings.Contains(err.Error(), "multiple statements") {
		t.Errorf("error %q should mention 'multiple statements'", err.Error())
	}
}

func TestReadOnlyQueryRejectsWritableCTE(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, err = s.ReadOnlyQuery("WITH x AS (SELECT 1) INSERT INTO researchers(name) VALUES('hack')")
	if err == nil {
		t.Fatal("expected error for writable CTE")
	}
}

func TestQueryTableEmptyResult(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, err := s.ReadOnlyQuery(
		"SELECT * FROM researchers WHERE name = 'this_name_definitely_does_not_exist'",
	)
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(cols) == 0 {
		t.Error("expected column names even for empty result")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// ---------- rowid semantics (sqlite-specific) ----------

func TestUpdateCellNoMatchingRow(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	val := "ghost"
	err = s.UpdateCell("researchers", "name", 99999, nil, &val)
	if err == nil {
		t.Fatal("expected error for non-existent rowid")
	}
}

// ---------- BLOB rendering (sqlite-specific: libsql vector embeddings) ----------

// TestBlobColumnFormatting verifies that BLOB columns (e.g. F32 vector
// embeddings stored in libsql vector tables) render as a compact placeholder
// instead of dumping the raw byte slice via %v, which would produce ~58KB of
// decimal-number-spam for a 16KB blob and break the table renderer.
func TestBlobColumnFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Exec(
		`CREATE TABLE embeddings (key TEXT PRIMARY KEY, vec BLOB NOT NULL)`,
	); err != nil {
		t.Fatalf("create table: %v", err)
	}
	const blobLen = 16384
	payload := make([]byte, blobLen)
	for i := range payload {
		payload[i] = byte(i)
	}
	if _, err := s.db.Exec(
		`INSERT INTO embeddings (key, vec) VALUES (?, ?)`, "abc", payload,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	wantValue := fmt.Sprintf("<BLOB %d bytes>", blobLen)

	t.Run("QueryTable", func(t *testing.T) {
		_, rows, _, _, err := s.QueryTable("embeddings")
		if err != nil {
			t.Fatalf("QueryTable: %v", err)
		}
		if len(rows) != 1 || len(rows[0]) != 2 {
			t.Fatalf("expected 1 row of 2 cols, got rows=%v", rows)
		}
		if rows[0][1] != wantValue {
			t.Errorf("BLOB cell:\n  got:  %q (len=%d)\n  want: %q",
				trunc(rows[0][1], 80), len(rows[0][1]), wantValue)
		}
	})

	t.Run("ReadOnlyQuery", func(t *testing.T) {
		_, rows, err := s.ReadOnlyQuery(`SELECT vec FROM embeddings`)
		if err != nil {
			t.Fatalf("ReadOnlyQuery: %v", err)
		}
		if len(rows) != 1 || len(rows[0]) != 1 {
			t.Fatalf("expected 1×1, got %v", rows)
		}
		if rows[0][0] != wantValue {
			t.Errorf("BLOB via ReadOnlyQuery:\n  got:  %q (len=%d)\n  want: %q",
				trunc(rows[0][0], 80), len(rows[0][0]), wantValue)
		}
	})

	t.Run("queryView", func(t *testing.T) {
		if _, err := s.Exec(
			`CREATE VIEW embeddings_v AS SELECT vec FROM embeddings`,
		); err != nil {
			t.Fatalf("create view: %v", err)
		}
		if _, err := s.TableNames(); err != nil {
			t.Fatalf("TableNames: %v", err)
		}
		_, rows, _, _, err := s.QueryTable("embeddings_v")
		if err != nil {
			t.Fatalf("QueryTable view: %v", err)
		}
		if len(rows) != 1 || len(rows[0]) != 1 {
			t.Fatalf("expected 1×1 from view, got %v", rows)
		}
		if rows[0][0] != wantValue {
			t.Errorf("BLOB via view:\n  got:  %q (len=%d)\n  want: %q",
				trunc(rows[0][0], 80), len(rows[0][0]), wantValue)
		}
	})
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------- ImportFile (sqlite-specific: existing-table refusal) ----------

func TestImportFileExistingTable(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.ImportFile(csvPath, "data"); err != nil {
		t.Fatalf("first ImportFile: %v", err)
	}

	if err := s.ImportFile(csvPath, "data"); err == nil {
		t.Fatal("expected error when importing into existing table")
	}
}
