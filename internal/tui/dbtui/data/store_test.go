package data

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the project root by walking up from this test file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	// file is data/store_test.go → go up 1 level to repo root
	return filepath.Join(filepath.Dir(file), "..")
}

// ---------- SQLite tests ----------

func TestSQLiteOpen(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
}

func TestSQLiteTableNames(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	names, err := store.TableNames()
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

func TestSQLiteTableColumns(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	cols, err := store.TableColumns("researchers")
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

func TestSQLiteQueryTable(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	colNames, rows, nullFlags, rowIDs, err := store.QueryTable("researchers")
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
	// rowIDs should be positive for SQLite
	if rowIDs[0] <= 0 {
		t.Errorf("expected positive rowID, got %d", rowIDs[0])
	}
}

func TestSQLiteTableRowCount(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	count, err := store.TableRowCount("researchers")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count == 0 {
		t.Error("expected non-zero row count")
	}
}

func TestSQLiteReadOnlyQuery(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	cols, rows, err := store.ReadOnlyQuery("SELECT name, department FROM researchers LIMIT 3")
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

func TestSQLiteReadOnlyQueryRejectsInsert(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, err = store.ReadOnlyQuery("INSERT INTO researchers (name) VALUES ('evil')")
	if err == nil {
		t.Fatal("expected error for INSERT query")
	}
}

func TestSQLiteInterface(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify it satisfies DataStore.
	var _ DataStore = store
}

// ---------- IsSafeIdentifier tests ----------

func TestIsSafeIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"researchers", true},
		{"my_table", true},
		{"Table123", true},
		{"", false},
		{"table name", true}, // spaces allowed (DuckDB compat)
		{"drop;--", false},
		{"table\"name", false},
	}
	for _, tt := range tests {
		if got := IsSafeIdentifier(tt.input); got != tt.want {
			t.Errorf("IsSafeIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// copyFixture copies a fixture file to a temp directory and returns the path.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), name)
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

// ---------- SQLite mutation tests ----------

func TestSQLiteUpdateCell(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Get the rowid for researcher id=1.
	_, rows, _, rowIDs, err := store.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(rowIDs) == 0 {
		t.Fatal("expected at least one row")
	}
	rowID := rowIDs[0]
	_ = rows

	// Update the name cell.
	newName := "Test Researcher"
	if err := store.UpdateCell("researchers", "name", rowID, nil, &newName); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}

	// Verify via ReadOnlyQuery.
	cols, resultRows, err := store.ReadOnlyQuery(
		"SELECT name FROM researchers WHERE rowid = " + fmt.Sprintf("%d", rowID),
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

	// Restore original value.
	orig := "Maria Chen"
	if err := store.UpdateCell("researchers", "name", rowID, nil, &orig); err != nil {
		t.Fatalf("restore UpdateCell: %v", err)
	}
}

func TestSQLiteDropTable(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.DropTable("equipment"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}

	names, err := store.TableNames()
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

func TestSQLiteRenameTable(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.RenameTable("equipment", "gear"); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}

	names, err := store.TableNames()
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
	// Total count should be unchanged.
	if len(names) != 4 {
		t.Fatalf("expected 4 tables, got %d: %v", len(names), names)
	}

	// Verify data is accessible under the new name.
	count, err := store.TableRowCount("gear")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count == 0 {
		t.Error("expected rows in renamed table")
	}
}

func TestSQLiteRenameTableInvalidName(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.RenameTable("equipment", "bad;name"); err == nil {
		t.Error("expected error for invalid new name")
	}
	if err := store.RenameTable("bad;name", "gear"); err == nil {
		t.Error("expected error for invalid old name")
	}
}

// ---------- ReadOnlyQuery edge cases ----------

func TestReadOnlyQueryMaxRows(t *testing.T) {
	// Create a temp SQLite DB with >200 rows and verify the cap.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "maxrows.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a table and insert 250 rows.
	if _, err := store.Exec("CREATE TABLE big(id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for i := 0; i < 250; i++ {
		if _, err := store.Exec(fmt.Sprintf("INSERT INTO big(id, val) VALUES (%d, 'row%d')", i, i)); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	// ReadOnlyQuery should cap at 200 rows.
	_, rows, err := store.ReadOnlyQuery("SELECT * FROM big")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(rows) != 200 {
		t.Errorf("expected 200 rows (cap), got %d", len(rows))
	}
}

func TestReadOnlyQueryEmptyQuery(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, err = store.ReadOnlyQuery("")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "empty query") {
		t.Errorf("error %q should mention 'empty query'", err.Error())
	}
}

func TestReadOnlyQuerySemicolon(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, err = store.ReadOnlyQuery("SELECT 1; DROP TABLE foo")
	if err == nil {
		t.Fatal("expected error for multi-statement query")
	}
	if !strings.Contains(err.Error(), "multiple statements") {
		t.Errorf("error %q should mention 'multiple statements'", err.Error())
	}
}

func TestSQLiteUpdateCellNoMatchingRow(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	val := "ghost"
	err = store.UpdateCell("researchers", "name", 99999, nil, &val)
	if err == nil {
		t.Fatal("expected error for non-existent rowid")
	}
}

func TestSQLiteQueryTableEmptyResult(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Use ReadOnlyQuery with a WHERE clause that matches nothing.
	cols, rows, err := store.ReadOnlyQuery(
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

// ---------- IsSafeIdentifier edge cases ----------

func TestIsSafeIdentifierEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		desc  string
	}{
		{"SELECT", true, "SQL keyword is still alphanumeric"},
		{"a", true, "single char"},
		{strings.Repeat("a", 100), true, "100-char name"},
		{"has-hyphen", false, "hyphen not allowed"},
		{"has.dot", false, "dot not allowed"},
		{"tab\there", false, "tab char not allowed"},
		{"new\nline", false, "newline not allowed"},
		{"DROP TABLE", true, "space allowed (DuckDB compat)"},
		{"résumé", false, "unicode not allowed"},
		{"_leading", true, "leading underscore ok"},
		{"123num", true, "leading digit ok"},
	}
	for _, tt := range tests {
		if got := IsSafeIdentifier(tt.input); got != tt.want {
			t.Errorf("IsSafeIdentifier(%q) [%s] = %v, want %v", tt.input, tt.desc, got, tt.want)
		}
	}
}

// ---------- ContainsWriteKeyword boundary tests ----------

func TestContainsWriteKeyword(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		desc  string
	}{
		{"WITH x AS (SELECT 1) INSERT INTO t VALUES (1)", true, "space-delimited INSERT"},
		{"WITH x AS (SELECT 1)INSERT INTO t VALUES (1)", true, "no space before INSERT"},
		{"WITH x AS (SELECT 1) UPDATE t SET a=1", true, "UPDATE"},
		{"WITH x AS (SELECT 1) DELETE FROM t", true, "DELETE"},
		{"WITH x AS (SELECT 1) DROP TABLE t", true, "DROP"},
		{"WITH x AS (SELECT 1) ALTER TABLE t ADD COLUMN x INT", true, "ALTER"},
		{"WITH x AS (SELECT 1) CREATE TABLE t(a INT)", true, "CREATE"},
		{"WITH x AS (SELECT 1) SELECT * FROM x", false, "pure SELECT CTE"},
		{"WITH INSERTED AS (SELECT 1) SELECT * FROM INSERTED", false, "INSERTED is not INSERT keyword"},
		{"WITH x AS (SELECT 1) SELECT UPDATED FROM x", false, "UPDATED is not UPDATE keyword"},
		{"WITH x AS (SELECT 1) SELECT DELETER FROM x", false, "DELETER is not DELETE keyword"},
	}
	for _, tt := range tests {
		upper := strings.ToUpper(tt.input)
		if got := ContainsWriteKeyword(upper); got != tt.want {
			t.Errorf("ContainsWriteKeyword(%q) [%s] = %v, want %v", tt.input, tt.desc, got, tt.want)
		}
	}
}

// ---------- DeleteRows tests ----------

func TestSQLiteDeleteRows(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Use equipment (no FK constraints referencing it).
	_, _, _, rowIDs, err := store.QueryTable("equipment")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	initialCount := len(rowIDs)
	if initialCount < 2 {
		t.Fatalf("need at least 2 rows, got %d", initialCount)
	}

	// Delete the first two rows by rowID.
	ids := []RowIdentifier{
		{RowID: rowIDs[0]},
		{RowID: rowIDs[1]},
	}
	deleted, err := store.DeleteRows("equipment", ids)
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	// Verify count decreased.
	count, err := store.TableRowCount("equipment")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != initialCount-2 {
		t.Errorf("expected %d rows after delete, got %d", initialCount-2, count)
	}
}

func TestSQLiteDeleteRowsInvalidTable(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.DeleteRows("drop;--", []RowIdentifier{{RowID: 1}})
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestSQLiteDeleteRowsEmpty(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	deleted, err := store.DeleteRows("researchers", nil)
	if err != nil {
		t.Fatalf("DeleteRows with nil: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted for empty list, got %d", deleted)
	}
}

// ---------- InsertRows tests ----------

func TestSQLiteInsertRows(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	initialCount, err := store.TableRowCount("researchers")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}

	cols := []string{"name", "department"}
	rows := [][]string{
		{"Test Person", "Physics"},
		{"Another Person", "Math"},
	}
	if err := store.InsertRows("researchers", cols, rows); err != nil {
		t.Fatalf("InsertRows: %v", err)
	}

	count, err := store.TableRowCount("researchers")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != initialCount+2 {
		t.Errorf("expected %d rows after insert, got %d", initialCount+2, count)
	}
}

func TestSQLiteInsertRowsNullHandling(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Empty string should become NULL.
	cols := []string{"name", "department"}
	rows := [][]string{{"NullTest", ""}}
	if err := store.InsertRows("researchers", cols, rows); err != nil {
		t.Fatalf("InsertRows: %v", err)
	}

	_, result, err := store.ReadOnlyQuery(
		"SELECT department FROM researchers WHERE name = 'NullTest'",
	)
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}
	// NULL comes back as empty string from ReadOnlyQuery.
	if result[0][0] != "" {
		t.Errorf("expected empty (NULL), got %q", result[0][0])
	}
}

func TestInsertRowsInvalidTable(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	err = store.InsertRows("drop;--", []string{"a"}, [][]string{{"1"}})
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

// ---------- ReadOnlyQuery rejects writable CTEs ----------

func TestReadOnlyQueryRejectsWritableCTE(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, err = store.ReadOnlyQuery("WITH x AS (SELECT 1) INSERT INTO researchers(name) VALUES('hack')")
	if err == nil {
		t.Fatal("expected error for writable CTE")
	}
}

// ---------- ImportFile tests ----------

func TestSQLiteImportFile(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "test.csv")
	if err := os.WriteFile(csvPath, []byte("x,y\n1,hello\n2,world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(csvPath, "test"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	count, err := store.TableRowCount("test")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

// ---------- ImportableExtensions tests ----------

func TestImportableExtensions(t *testing.T) {
	exts := ImportableExtensions()
	want := map[string]bool{".csv": true, ".tsv": true, ".json": true, ".jsonl": true, ".ndjson": true}
	if len(exts) != len(want) {
		t.Fatalf("expected %d extensions, got %d", len(want), len(exts))
	}
	for _, ext := range exts {
		if !want[ext] {
			t.Errorf("unexpected extension %q", ext)
		}
	}
}

// ---------- TableNameFromFile tests ----------

func TestTableNameFromFile(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"data.csv", "data"},
		{"123.json", "_123"},
		{"hello world.csv", "hello_world"},
		{"/some/path/test.jsonl", "test"},
		{"----.csv", "____"},
	}
	for _, tt := range tests {
		got := TableNameFromFile(tt.path)
		if got != tt.want {
			t.Errorf("TableNameFromFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// ---------- CreateEmptyTable tests ----------

func TestSQLiteCreateEmptyTable(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	err = store.CreateEmptyTable("new_table")
	if err != nil {
		t.Fatalf("CreateEmptyTable: %v", err)
	}

	// Verify the table exists and is empty.
	count, err := store.TableRowCount("new_table")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}

	// Verify default schema columns exist.
	cols, err := store.TableColumns("new_table")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 3 {
		t.Errorf("expected 3 columns (id, name, value), got %d", len(cols))
	}
}

func TestSQLiteCreateEmptyTableExisting(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// "researchers" already exists in the fixture.
	err = store.CreateEmptyTable("researchers")
	if err == nil {
		t.Fatal("expected error when creating table that already exists")
	}
}

// ---------- Additional SQLite tests ----------

func TestSQLiteUpdateCellNull(t *testing.T) {
	dbPath := copyFixture(t, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, _, rowIDs, err := store.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(rowIDs) == 0 {
		t.Fatal("expected at least one row")
	}

	// Set cell to NULL by passing nil.
	if err := store.UpdateCell("researchers", "department", rowIDs[0], nil, nil); err != nil {
		t.Fatalf("UpdateCell(nil): %v", err)
	}

	// Verify it reads back as NULL (empty string from ReadOnlyQuery).
	_, rows, err := store.ReadOnlyQuery(
		fmt.Sprintf("SELECT department FROM researchers WHERE rowid = %d", rowIDs[0]),
	)
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "" {
		t.Errorf("expected empty (NULL), got %q", rows[0][0])
	}

	// Also check via nullFlags from QueryTable.
	_, _, nullFlags, _, err := store.QueryTable("researchers")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	// Find the department column index.
	cols, _ := store.TableColumns("researchers")
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

func TestSQLiteImportFileJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "data.json")
	content := `[{"name":"alice","age":"30"},{"name":"bob","age":"25"}]`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(jsonPath, "people"); err != nil {
		t.Fatalf("ImportFile JSON: %v", err)
	}

	count, err := store.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}

	cols, err := store.TableColumns("people")
	if err != nil {
		t.Fatal(err)
	}
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
	}
	if !strings.Contains(strings.Join(colNames, ","), "name") {
		t.Errorf("expected 'name' column, got %v", colNames)
	}
}

func TestSQLiteImportFileJSONL(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "data.jsonl")
	content := "{\"x\":\"1\",\"y\":\"a\"}\n{\"x\":\"2\",\"y\":\"b\"}\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(jsonlPath, "lines"); err != nil {
		t.Fatalf("ImportFile JSONL: %v", err)
	}

	count, err := store.TableRowCount("lines")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestSQLiteImportFileUnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(txtPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	err = store.ImportFile(txtPath, "bad")
	if err == nil {
		t.Fatal("expected error for unsupported file extension")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error, got: %v", err)
	}
}

func TestSQLiteImportFileExistingTable(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// First import succeeds.
	if err := store.ImportFile(csvPath, "data"); err != nil {
		t.Fatalf("first ImportFile: %v", err)
	}

	// Second import with same table name should fail.
	err = store.ImportFile(csvPath, "data")
	if err == nil {
		t.Fatal("expected error when importing into existing table")
	}
}

func TestSQLiteTableSummaries(t *testing.T) {
	dbPath := filepath.Join(repoRoot(t), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	summaries, err := store.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}

	if len(summaries) != 4 {
		t.Fatalf("expected 4 table summaries, got %d", len(summaries))
	}

	// Verify each summary has a name and positive column count.
	for _, s := range summaries {
		if s.Name == "" {
			t.Error("expected non-empty table name")
		}
		if s.Columns <= 0 {
			t.Errorf("table %q: expected positive column count, got %d", s.Name, s.Columns)
		}
		if s.Rows < 0 {
			t.Errorf("table %q: expected non-negative row count, got %d", s.Name, s.Rows)
		}
	}
}
