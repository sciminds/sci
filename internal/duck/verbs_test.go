package duck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All verb tests share this "binary required" gate. They exercise the JSON
// shape of each result and confirm Human() rendering is non-empty (we trust
// duckdb's box-mode formatter; we verify it ran and produced output).

func TestColsCSV(t *testing.T) {
	requireDuck(t)
	res, err := Cols(tinyCSV, "")
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	if len(res.Columns) != 3 {
		t.Fatalf("got %d columns, want 3 (id,name,score): %+v", len(res.Columns), res.Columns)
	}
	wantNames := []string{"id", "name", "score"}
	for i, c := range res.Columns {
		if c.Name != wantNames[i] {
			t.Errorf("col[%d].Name = %q, want %q", i, c.Name, wantNames[i])
		}
		if c.Type == "" {
			t.Errorf("col[%d].Type is empty", i)
		}
	}
	if res.Human() == "" {
		t.Error("Human() returned empty string")
	}
}

func TestColsParquet(t *testing.T) {
	requireDuck(t)
	res, err := Cols(tinyParquet, "")
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	if len(res.Columns) != 3 {
		t.Errorf("got %d columns, want 3", len(res.Columns))
	}
}

func TestColsXLSXMultiSheetRequiresTable(t *testing.T) {
	requireDuck(t)
	if _, err := Cols(tinyXLSX, ""); err == nil {
		t.Error("expected error for multi-sheet xlsx without --table")
	}
	res, err := Cols(tinyXLSX, "extras")
	if err != nil {
		t.Fatalf("Cols(extras): %v", err)
	}
	if len(res.Columns) != 2 {
		t.Errorf("extras sheet has 2 cols (key,val), got %d", len(res.Columns))
	}
}

func TestColsSQLite(t *testing.T) {
	requireDuck(t)
	res, err := Cols(tinyDB, "people")
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	if len(res.Columns) != 3 {
		t.Errorf("got %d columns, want 3", len(res.Columns))
	}
}

// TestHeadSQLiteMixedTypes pins the regression where duckdb's sqlite_scanner
// errors on SQLite columns whose declared type doesn't match every stored
// cell. The promotion layer wraps the read in TRY_CAST so "" → NULL and the
// column comes through as its declared type.
func TestHeadSQLiteMixedTypes(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(mixedDB); err != nil {
		t.Skipf("mixed_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	res, err := Head(mixedDB, "demo", 0)
	if err != nil {
		t.Fatalf("Head on mixed-typed sqlite: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(res.Rows))
	}
	// Row 2's age was "" in SQLite; promotion normalises to NULL.
	if v := res.Rows[1]["age"]; v != nil {
		t.Errorf("row[1].age = %v (%T), want nil (promoted from empty string)", v, v)
	}
	// Row 0's age came through as a real number.
	if v := res.Rows[0]["age"]; v == nil {
		t.Errorf("row[0].age = nil, want a numeric value")
	}
}

// TestColsSQLiteMixedTypesPromoted: declared-INTEGER column with "" cells
// promotes to BIGINT (NULLIF eats the empty). Cols explain shows the
// declared type alongside the resolved type, with no fallback note.
func TestColsSQLiteMixedTypesPromoted(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(mixedDB); err != nil {
		t.Skipf("mixed_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	res, err := Cols(mixedDB, "demo")
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	byName := map[string]ColumnInfo{}
	for _, c := range res.Columns {
		byName[c.Name] = c
	}
	age, ok := byName["age"]
	if !ok {
		t.Fatalf("no age column in %+v", res.Columns)
	}
	if age.Type != "BIGINT" {
		t.Errorf("age.Type = %q, want BIGINT (promoted)", age.Type)
	}
	if age.Declared == "" {
		t.Errorf("age.Declared is empty; want non-empty for sqlite source")
	}
	if age.FailingCells != 0 {
		t.Errorf("age.FailingCells = %d, want 0 (clean promote)", age.FailingCells)
	}
}

// TestColsSQLiteDirtyTypesFallback: declared-INTEGER column with "abc"
// (a genuinely non-castable cell) must fall back to VARCHAR so the original
// value is preserved. Cols explain reports the failing-cell count.
func TestColsSQLiteDirtyTypesFallback(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(dirtyDB); err != nil {
		t.Skipf("dirty_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	res, err := Cols(dirtyDB, "demo")
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	byName := map[string]ColumnInfo{}
	for _, c := range res.Columns {
		byName[c.Name] = c
	}
	age := byName["age"]
	if age.Type != "VARCHAR" {
		t.Errorf("age.Type = %q, want VARCHAR (fallback)", age.Type)
	}
	if age.Declared == "" {
		t.Errorf("age.Declared is empty; want the original declared type")
	}
	if age.FailingCells == 0 {
		t.Errorf("age.FailingCells = 0, want > 0 (one cell didn't cast)")
	}
}

// TestHeadSQLiteDirtyTypesPreservesCell: the "abc" must come through
// verbatim — that's the no-data-loss promise.
func TestHeadSQLiteDirtyTypesPreservesCell(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(dirtyDB); err != nil {
		t.Skipf("dirty_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	res, err := Head(dirtyDB, "demo", 0)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if v, _ := res.Rows[1]["age"].(string); v != "abc" {
		t.Errorf("row[1].age = %v, want \"abc\" preserved verbatim", res.Rows[1]["age"])
	}
}

// TestSummarizeSQLitePromotedNumeric: after promotion, SUMMARIZE produces
// real numeric stats (avg, std, quartiles) on the BIGINT column.
func TestSummarizeSQLitePromotedNumeric(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(mixedDB); err != nil {
		t.Skipf("mixed_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	res, err := Summarize(mixedDB, "demo")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	var age SummarizeColumn
	for _, c := range res.Columns {
		if c.Name == "age" {
			age = c
		}
	}
	if age.Avg == "" {
		t.Errorf("age.Avg is empty; want a numeric average post-promotion")
	}
	if age.Type != "BIGINT" {
		t.Errorf("age.Type = %q, want BIGINT", age.Type)
	}
}

// TestColsCSVStillTwoColumnBox: non-sqlite sources continue to show the
// simple 2-column box; declared/note fields are empty.
func TestColsCSVStillTwoColumnBox(t *testing.T) {
	requireDuck(t)
	res, err := Cols(tinyCSV, "")
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	for _, c := range res.Columns {
		if c.Declared != "" {
			t.Errorf("col %q: Declared = %q, want empty for csv source", c.Name, c.Declared)
		}
	}
}

func TestHeadDefault(t *testing.T) {
	requireDuck(t)
	res, err := Head(tinyCSV, "", 0) // 0 → default 10; tiny.csv only has 3 rows
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Errorf("got %d rows, want 3 (file has only 3)", len(res.Rows))
	}
	if len(res.Columns) != 3 {
		t.Errorf("got %d columns, want 3", len(res.Columns))
	}
	if res.Rows[0]["name"] != "alice" {
		t.Errorf("row[0].name = %v, want alice", res.Rows[0]["name"])
	}
}

func TestHeadLimitN(t *testing.T) {
	requireDuck(t)
	res, err := Head(tinyCSV, "", 2)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Errorf("got %d rows, want 2", len(res.Rows))
	}
}

func TestTail(t *testing.T) {
	requireDuck(t)
	res, err := Tail(tinyCSV, "", 2)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Errorf("got %d rows, want 2", len(res.Rows))
	}
	if res.Rows[len(res.Rows)-1]["name"] != "carol" {
		t.Errorf("last row name = %v, want carol", res.Rows[len(res.Rows)-1]["name"])
	}
}

func TestGlimpse(t *testing.T) {
	requireDuck(t)
	res, err := Glimpse(tinyCSV, "", 5)
	if err != nil {
		t.Fatalf("Glimpse: %v", err)
	}
	if len(res.Columns) != 3 {
		t.Fatalf("got %d glimpse columns, want 3", len(res.Columns))
	}
	for _, c := range res.Columns {
		if c.Name == "" || c.Type == "" {
			t.Errorf("glimpse column missing name/type: %+v", c)
		}
		if len(c.Samples) == 0 {
			t.Errorf("glimpse column %q has no samples", c.Name)
		}
	}
}

func TestShape(t *testing.T) {
	requireDuck(t)
	res, err := Shape(tinyCSV, "")
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if res.Rows != 3 || res.Columns != 3 {
		t.Errorf("shape = (%d, %d), want (3, 3)", res.Rows, res.Columns)
	}
}

func TestShapeMultiTableSQLite(t *testing.T) {
	requireDuck(t)
	res, err := Shape(tinyDB, "extras")
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if res.Rows != 2 || res.Columns != 2 {
		t.Errorf("shape = (%d, %d), want (2, 2)", res.Rows, res.Columns)
	}
}

func TestSummarize(t *testing.T) {
	requireDuck(t)
	res, err := Summarize(tinyCSV, "")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(res.Columns) != 3 {
		t.Fatalf("got %d summarized columns, want 3", len(res.Columns))
	}
	for _, c := range res.Columns {
		if c.Name == "" {
			t.Errorf("summarize column missing name: %+v", c)
		}
	}
}

func TestQueryReadOnly(t *testing.T) {
	requireDuck(t)
	res, err := Query(tinyCSV, "SELECT name FROM src WHERE id = 2")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Rows))
	}
	if res.Rows[0]["name"] != "bob" {
		t.Errorf("name = %v, want bob", res.Rows[0]["name"])
	}
}

func TestQueryDuckDBRealTableNames(t *testing.T) {
	requireDuck(t)
	// tiny.duckdb has 3 tables (people, extras); the user references one
	// by its real name rather than `src`. This must not demand --table.
	res, err := Query(tinyDuck, "SELECT name FROM people WHERE id = 2")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Rows))
	}
	if res.Rows[0]["name"] != "bob" {
		t.Errorf("name = %v, want bob", res.Rows[0]["name"])
	}

	// A different table in the same file is just as reachable.
	res2, err := Query(tinyDuck, "SELECT k, v FROM extras ORDER BY k")
	if err != nil {
		t.Fatalf("Query extras: %v", err)
	}
	if len(res2.Rows) != 2 {
		t.Fatalf("got %d rows from extras, want 2", len(res2.Rows))
	}
}

func TestQuerySQLiteRealTableNames(t *testing.T) {
	requireDuck(t)
	// Multi-table SQLite (people, extras) — same contract as DuckDB.
	res, err := Query(tinyDB, "SELECT name FROM people WHERE id = 3")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Rows))
	}
	if res.Rows[0]["name"] != "carol" {
		t.Errorf("name = %v, want carol", res.Rows[0]["name"])
	}
}

// TestQuerySQLiteDirtyTypesFallback: SQLite's dynamic typing lets a value
// violate its column's declared type. Native typing would error with a raw
// duckdb "Mismatch Type Error"; Query must instead retry with everything as
// VARCHAR so the query succeeds and the offending cell survives as text.
func TestQuerySQLiteDirtyTypesFallback(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(dirtyDB); err != nil {
		t.Skipf("dirty_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	// age is declared INTEGER but row 2 holds "abc" — a hard cast failure.
	res, err := Query(dirtyDB, "SELECT id, name, age FROM demo ORDER BY id")
	if err != nil {
		t.Fatalf("Query should gracefully fall back, got: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(res.Rows))
	}
	if v, _ := res.Rows[1]["age"].(string); v != "abc" {
		t.Errorf("age[1] = %v, want \"abc\" (preserved as text)", res.Rows[1]["age"])
	}
}

// TestQuerySQLiteEmptyStringFallback: the common real-world case — an empty
// string "" placeholder in a numeric column (e.g. missing ages). Must not
// error.
func TestQuerySQLiteEmptyStringFallback(t *testing.T) {
	requireDuck(t)
	if _, err := os.Stat(mixedDB); err != nil {
		t.Skipf("mixed_types.db fixture not generated (sqlite3 missing?): %v", err)
	}
	res, err := Query(mixedDB, "SELECT id, age FROM demo ORDER BY id")
	if err != nil {
		t.Fatalf("Query should gracefully fall back, got: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(res.Rows))
	}
	if v, _ := res.Rows[1]["age"].(string); v != "" {
		t.Errorf("age[1] = %v, want \"\" (preserved)", res.Rows[1]["age"])
	}
}

func TestConvertCSVToParquet(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.parquet")
	res, err := Convert(tinyCSV, "", out, "")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Output != out {
		t.Errorf("Output = %q, want %q", res.Output, out)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}

	// Round-trip: read it back and confirm shape.
	shape, err := Shape(out, "")
	if err != nil {
		t.Fatalf("Shape on output: %v", err)
	}
	if shape.Rows != 3 || shape.Columns != 3 {
		t.Errorf("round-tripped shape = (%d, %d), want (3, 3)", shape.Rows, shape.Columns)
	}
}

func TestConvertCSVToJSON(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	if _, err := Convert(tinyCSV, "", out, ""); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alice") {
		t.Errorf("output JSON missing alice: %q", string(body))
	}
}

func TestConvertUnsupportedTarget(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	if _, err := Convert(tinyCSV, "", filepath.Join(dir, "out.bogus"), ""); err == nil {
		t.Error("expected error for unsupported output extension")
	}
}

func TestConvertCSVToSQLite(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.db")
	res, err := Convert(tinyCSV, "", out, "")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Rows != 3 {
		t.Errorf("rows = %d, want 3", res.Rows)
	}
	// Destination table defaults to source basename → "tiny".
	cols, err := Cols(out, "tiny")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(cols.Columns) != 3 {
		t.Errorf("columns = %d, want 3", len(cols.Columns))
	}
}

func TestConvertCSVToDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.duckdb")
	res, err := Convert(tinyCSV, "", out, "")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Rows != 3 {
		t.Errorf("rows = %d, want 3", res.Rows)
	}
	shape, err := Shape(out, "tiny")
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if shape.Rows != 3 {
		t.Errorf("rows = %d, want 3", shape.Rows)
	}
}

func TestConvertDestTableOverride(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.db")
	if _, err := Convert(tinyCSV, "", out, "researchers"); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	cols, err := Cols(out, "researchers")
	if err != nil {
		t.Fatalf("read back named table: %v", err)
	}
	if len(cols.Columns) != 3 {
		t.Errorf("columns = %d, want 3", len(cols.Columns))
	}
}

func TestConvertSQLiteToCSV(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.csv")
	res, err := Convert(tinyDB, "people", out, "")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Rows != 3 {
		t.Errorf("rows = %d, want 3", res.Rows)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alice") {
		t.Errorf("output csv missing alice: %q", string(body))
	}
}

func TestConvertSQLiteToDuckDB(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "cross.duckdb")
	if _, err := Convert(tinyDB, "extras", out, "ported"); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	shape, err := Shape(out, "ported")
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if shape.Rows != 2 {
		t.Errorf("rows = %d, want 2 (extras has 2 rows)", shape.Rows)
	}
}

// TestConvertDestTableDefaultsToSrcTable pins the cross-DB UX: when
// converting between databases without --as, the destination table
// keeps the source table's name (not the source file's basename).
func TestConvertDestTableDefaultsToSrcTable(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "cross.duckdb")
	if _, err := Convert(tinyDB, "people", out, ""); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if _, err := Cols(out, "people"); err != nil {
		t.Errorf("expected destination table named %q (source table name preserved): %v", "people", err)
	}
}

func TestConvertMultiTableSourceWithoutTableErrors(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	if _, err := Convert(tinyDB, "", filepath.Join(dir, "out.csv"), ""); err == nil {
		t.Error("expected error when converting multi-table sqlite without --table")
	}
}

func TestConvertUnsafeDestTableRejected(t *testing.T) {
	requireDuck(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.db")
	if _, err := Convert(tinyCSV, "", out, "bad; DROP TABLE x"); err == nil {
		t.Error("expected error for unsafe --as identifier")
	}
}
