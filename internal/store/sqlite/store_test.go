package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/store"
)

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

// ---------- open / introspection ----------

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

func TestTableNames(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 4 {
		t.Fatalf("expected 4 tables, got %d: %v", len(names), names)
	}
	expected := []string{"equipment", "projects", "publications", "researchers"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("table[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestTableColumns(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, err := s.TableColumns("researchers")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 7 {
		t.Fatalf("expected 7 columns, got %d", len(cols))
	}
	if cols[0].Name != "id" || cols[0].PK != 1 {
		t.Errorf("first column: got %q pk=%d, want 'id' pk=1", cols[0].Name, cols[0].PK)
	}
	if cols[1].Name != "name" {
		t.Errorf("second column: got %q, want 'name'", cols[1].Name)
	}
}

func TestQueryTable(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	colNames, rows, nullFlags, rowIDs, err := s.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(colNames) < 2 {
		t.Fatalf("expected at least 2 columns, got %d", len(colNames))
	}
	if len(rows) == 0 {
		t.Fatal("expected at least 1 row")
	}
	if len(nullFlags) != len(rows) {
		t.Fatalf("nullFlags length %d != rows length %d", len(nullFlags), len(rows))
	}
	if len(rowIDs) != len(rows) {
		t.Fatalf("rowIDs length %d != rows length %d", len(rowIDs), len(rows))
	}
	if rowIDs[0] <= 0 {
		t.Errorf("expected positive rowID, got %d", rowIDs[0])
	}
}

func TestTableRowCount(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	count, err := s.TableRowCount("researchers")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count == 0 {
		t.Error("expected non-zero row count")
	}
}

func TestReadOnlyQuery(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, err := s.ReadOnlyQuery("SELECT name, department FROM researchers LIMIT 3")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if len(rows) > 3 {
		t.Errorf("expected at most 3 rows, got %d", len(rows))
	}
}

func TestReadOnlyQueryRejectsInsert(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, err = s.ReadOnlyQuery("INSERT INTO researchers (name) VALUES ('evil')")
	if err == nil {
		t.Fatal("expected error for INSERT query")
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

// ---------- mutations ----------

func TestUpdateCell(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, _, rowIDs, err := s.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(rowIDs) == 0 {
		t.Fatal("expected at least one row")
	}
	rowID := rowIDs[0]

	newName := "Test Researcher"
	if err := s.UpdateCell("researchers", "name", rowID, nil, &newName); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}

	cols, resultRows, err := s.ReadOnlyQuery(
		fmt.Sprintf("SELECT name FROM researchers WHERE rowid = %d", rowID),
	)
	if err != nil {
		t.Fatalf("ReadOnlyQuery after update: %v", err)
	}
	if len(cols) != 1 || len(resultRows) != 1 {
		t.Fatalf("unexpected result: cols=%d rows=%d", len(cols), len(resultRows))
	}
	if resultRows[0][0] != newName {
		t.Errorf("got %q, want %q", resultRows[0][0], newName)
	}
}

func TestUpdateCellNull(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, _, rowIDs, err := s.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(rowIDs) == 0 {
		t.Fatal("expected at least one row")
	}

	if err := s.UpdateCell("researchers", "department", rowIDs[0], nil, nil); err != nil {
		t.Fatalf("UpdateCell(nil): %v", err)
	}

	_, _, nullFlags, _, err := s.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	cols, _ := s.TableColumns("researchers")
	deptIdx := -1
	for i, c := range cols {
		if c.Name == "department" {
			deptIdx = i
			break
		}
	}
	if deptIdx < 0 {
		t.Fatal("department column not found")
	}
	if !nullFlags[0][deptIdx] {
		t.Error("expected NULL flag for department after setting to nil")
	}
}

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

func TestDropTable(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.DropTable("equipment"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 tables after drop, got %d: %v", len(names), names)
	}
	for _, n := range names {
		if n == "equipment" {
			t.Error("'equipment' still present after DropTable")
		}
	}
}

func TestRenameTable(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.RenameTable("equipment", "gear"); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	found := false
	for _, n := range names {
		if n == "equipment" {
			t.Error("'equipment' still present after rename")
		}
		if n == "gear" {
			found = true
		}
	}
	if !found {
		t.Error("'gear' not found after rename")
	}
	if len(names) != 4 {
		t.Fatalf("expected 4 tables, got %d: %v", len(names), names)
	}

	count, err := s.TableRowCount("gear")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count == 0 {
		t.Error("expected rows in renamed table")
	}
}

func TestRenameTableInvalidName(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.RenameTable("equipment", "bad;name"); err == nil {
		t.Error("expected error for invalid new name")
	}
	if err := s.RenameTable("bad;name", "gear"); err == nil {
		t.Error("expected error for invalid old name")
	}
}

func TestDeleteRows(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, _, rowIDs, err := s.QueryTable("equipment")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	initialCount := len(rowIDs)
	if initialCount < 2 {
		t.Fatalf("need at least 2 rows, got %d", initialCount)
	}

	ids := []store.RowIdentifier{
		{RowID: rowIDs[0]},
		{RowID: rowIDs[1]},
	}
	deleted, err := s.DeleteRows("equipment", ids)
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	count, err := s.TableRowCount("equipment")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != initialCount-2 {
		t.Errorf("expected %d rows after delete, got %d", initialCount-2, count)
	}
}

func TestDeleteRowsInvalidTable(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, err = s.DeleteRows("drop;--", []store.RowIdentifier{{RowID: 1}})
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestDeleteRowsEmpty(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	deleted, err := s.DeleteRows("researchers", nil)
	if err != nil {
		t.Fatalf("DeleteRows with nil: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted for empty list, got %d", deleted)
	}
}

func TestInsertRows(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	initialCount, err := s.TableRowCount("researchers")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}

	cols := []string{"name", "department"}
	rows := [][]string{
		{"Test Person", "Physics"},
		{"Another Person", "Math"},
	}
	if err := s.InsertRows("researchers", cols, rows); err != nil {
		t.Fatalf("InsertRows: %v", err)
	}

	count, err := s.TableRowCount("researchers")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != initialCount+2 {
		t.Errorf("expected %d rows after insert, got %d", initialCount+2, count)
	}
}

func TestInsertRowsNullHandling(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols := []string{"name", "department"}
	rows := [][]string{{"NullTest", ""}}
	if err := s.InsertRows("researchers", cols, rows); err != nil {
		t.Fatalf("InsertRows: %v", err)
	}

	_, result, err := s.ReadOnlyQuery(
		"SELECT department FROM researchers WHERE name = 'NullTest'",
	)
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}
	if result[0][0] != "" {
		t.Errorf("expected empty (NULL), got %q", result[0][0])
	}
}

func TestInsertRowsInvalidTable(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.InsertRows("drop;--", []string{"a"}, [][]string{{"1"}})
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

// ---------- DDL / creation ----------

func TestCreateEmptyTable(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.CreateEmptyTable("new_table"); err != nil {
		t.Fatalf("CreateEmptyTable: %v", err)
	}

	count, err := s.TableRowCount("new_table")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}

	cols, err := s.TableColumns("new_table")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 3 {
		t.Errorf("expected 3 columns (id, name, value), got %d", len(cols))
	}
}

func TestCreateEmptyTableExisting(t *testing.T) {
	path := copyFixture(t, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.CreateEmptyTable("researchers"); err == nil {
		t.Fatal("expected error when creating table that already exists")
	}
}

// ---------- summaries / views / blob formatting ----------

func TestTableSummaries(t *testing.T) {
	s, err := Open(testDB)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	summaries, err := s.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	if len(summaries) != 4 {
		t.Fatalf("expected 4 table summaries, got %d", len(summaries))
	}
	for _, sum := range summaries {
		if sum.Name == "" {
			t.Error("expected non-empty table name")
		}
		if sum.Columns <= 0 {
			t.Errorf("table %q: expected positive column count, got %d", sum.Name, sum.Columns)
		}
		if sum.Rows < 0 {
			t.Errorf("table %q: expected non-negative row count, got %d", sum.Name, sum.Rows)
		}
	}
}

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

// ---------- ImportFile ----------

func TestImportFile(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "test.csv")
	if err := os.WriteFile(csvPath, []byte("x,y\n1,hello\n2,world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.ImportFile(csvPath, "test"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	count, err := s.TableRowCount("test")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestImportFileJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "data.json")
	content := `[{"name":"alice","age":"30"},{"name":"bob","age":"25"}]`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.ImportFile(jsonPath, "people"); err != nil {
		t.Fatalf("ImportFile JSON: %v", err)
	}

	count, err := s.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestImportFileJSONL(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "data.jsonl")
	content := "{\"x\":\"1\",\"y\":\"a\"}\n{\"x\":\"2\",\"y\":\"b\"}\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.ImportFile(jsonlPath, "lines"); err != nil {
		t.Fatalf("ImportFile JSONL: %v", err)
	}

	count, err := s.TableRowCount("lines")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestImportFileUnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(txtPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.ImportFile(txtPath, "bad")
	if err == nil {
		t.Fatal("expected error for unsupported file extension")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error, got: %v", err)
	}
}

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
