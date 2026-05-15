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
