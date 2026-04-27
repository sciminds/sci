package data

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

// ---------- Type inference ----------

func TestInferType_Integer(t *testing.T) {
	vals := []string{"1", "42", "-7", "0", "1000000"}
	got := inferColumnType(vals)
	if got != "INTEGER" {
		t.Errorf("inferColumnType(%v) = %q, want INTEGER", vals, got)
	}
}

func TestInferType_Real(t *testing.T) {
	vals := []string{"1.5", "3.14", "-0.001", "42.0"}
	got := inferColumnType(vals)
	if got != "REAL" {
		t.Errorf("inferColumnType(%v) = %q, want REAL", vals, got)
	}
}

func TestInferType_MixedIntAndReal(t *testing.T) {
	// If a column has both ints and reals, it should widen to REAL.
	vals := []string{"1", "2.5", "3", "4.0"}
	got := inferColumnType(vals)
	if got != "REAL" {
		t.Errorf("inferColumnType(%v) = %q, want REAL", vals, got)
	}
}

func TestInferType_Text(t *testing.T) {
	vals := []string{"hello", "world", "foo"}
	got := inferColumnType(vals)
	if got != "TEXT" {
		t.Errorf("inferColumnType(%v) = %q, want TEXT", vals, got)
	}
}

func TestInferType_EmptyStringsIgnored(t *testing.T) {
	// Empty strings (NULLs) should not influence type. Remaining values are ints.
	vals := []string{"1", "", "3", ""}
	got := inferColumnType(vals)
	if got != "INTEGER" {
		t.Errorf("inferColumnType(%v) = %q, want INTEGER", vals, got)
	}
}

func TestInferType_AllEmpty(t *testing.T) {
	// If every value is empty, default to TEXT.
	vals := []string{"", "", ""}
	got := inferColumnType(vals)
	if got != "TEXT" {
		t.Errorf("inferColumnType(%v) = %q, want TEXT", vals, got)
	}
}

func TestInferType_BooleanAsInteger(t *testing.T) {
	// "true"/"false" should not be treated as integers — they're TEXT.
	vals := []string{"true", "false", "true"}
	got := inferColumnType(vals)
	if got != "TEXT" {
		t.Errorf("inferColumnType(%v) = %q, want TEXT", vals, got)
	}
}

// ---------- CSV import into SQLite ----------

func TestImportCSV_Basic(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age,score\nAlice,30,9.5\nBob,25,8.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	// Table should exist.
	names, err := store.TableNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "people" {
		t.Fatalf("expected [people], got %v", names)
	}

	// Check columns and types.
	cols, err := store.TableColumns("people")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
	wantCols := []struct {
		name string
		typ  string
	}{
		{"name", "TEXT"},
		{"age", "INTEGER"},
		{"score", "REAL"},
	}
	for i, want := range wantCols {
		if cols[i].Name != want.name {
			t.Errorf("col[%d].Name = %q, want %q", i, cols[i].Name, want.name)
		}
		if cols[i].Type != want.typ {
			t.Errorf("col[%d].Type = %q, want %q", i, cols[i].Type, want.typ)
		}
	}

	// Check row count.
	count, err := store.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestImportCSV_BOMHeader(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "bom.csv")
	// UTF-8 BOM + "Bad,Good\n1,2\n" — the exact shape from issue #1.
	content := "\ufeffBad,Good\n1,2\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(csvPath, "bom"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	cols, err := store.TableColumns("bom")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 || cols[0].Name != "Bad" || cols[1].Name != "Good" {
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		t.Errorf("columns = %q, want [Bad Good]", names)
	}
}

func TestImportCSV_HeaderSanitation(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "messy.csv")
	// Padded header, empty header, duplicate header, punctuation, Unicode.
	content := "  Name  ,,Date (UTC),temp_°C,Name\nAlice,x,2024-01-01,21,A\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(csvPath, "messy"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	cols, err := store.TableColumns("messy")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(cols))
	for i, c := range cols {
		got[i] = c.Name
	}
	want := []string{"Name", "column_2", "Date (UTC)", "temp_°C", "Name_1"}
	if len(got) != len(want) {
		t.Fatalf("columns = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("col[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Data should still land in the right column.
	count, err := store.TableRowCount("messy")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}

func TestImportCSV_WithNulls(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("id,value\n1,hello\n2,\n3,world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(csvPath, "stuff"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	// Row 2 should have NULL for value column.
	colNames, rows, nullFlags, _, err := store.QueryTable("stuff")
	if err != nil {
		t.Fatal(err)
	}
	_ = colNames
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if !nullFlags[1][1] {
		t.Error("expected row 2, col 'value' to be NULL")
	}
}

func TestImportTSV(t *testing.T) {
	dir := t.TempDir()
	tsvPath := filepath.Join(dir, "data.tsv")
	if err := os.WriteFile(tsvPath, []byte("name\tage\nAlice\t30\nBob\t25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(tsvPath, "people"); err != nil {
		t.Fatalf("ImportFile: %v", err)
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
	if cols[1].Type != "INTEGER" {
		t.Errorf("age type = %q, want INTEGER", cols[1].Type)
	}
}

func TestImportJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "data.json")
	content := `[{"name":"Alice","age":30,"score":9.5},{"name":"Bob","age":25,"score":8.0}]`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(jsonPath, "people"); err != nil {
		t.Fatalf("ImportFile: %v", err)
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
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
}

func TestImportJSONL(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "data.jsonl")
	content := "{\"name\":\"Alice\",\"age\":30}\n{\"name\":\"Bob\",\"age\":25}\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportFile(jsonlPath, "people"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	count, err := store.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

// ---------- Export selected rows ----------

func TestWriteRowsCSV(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	header := []string{"name", "age", "score"}
	rows := [][]string{
		{"Alice", "30", "9.5"},
		{"Bob", "25", "8.0"},
	}

	if err := WriteRowsCSV(outPath, header, rows); err != nil {
		t.Fatalf("WriteRowsCSV: %v", err)
	}

	// Read back and verify.
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 3 { // header + 2 rows
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0][0] != "name" || records[0][1] != "age" || records[0][2] != "score" {
		t.Errorf("header = %v, want [name age score]", records[0])
	}
	if records[1][0] != "Alice" || records[1][1] != "30" {
		t.Errorf("row 1 = %v", records[1])
	}
}

func TestWriteRowsCSV_Empty(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	// Header only, no rows — should still write header.
	if err := WriteRowsCSV(outPath, []string{"a", "b"}, nil); err != nil {
		t.Fatalf("WriteRowsCSV: %v", err)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (header only), got %d", len(records))
	}
}

// ---------- Column operations ----------

func TestRenameColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	// Create a table and import data.
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatal(err)
	}

	// Rename "age" → "years".
	if err := store.RenameColumn("people", "age", "years"); err != nil {
		t.Fatalf("RenameColumn: %v", err)
	}

	cols, err := store.TableColumns("people")
	if err != nil {
		t.Fatal(err)
	}
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
	}
	if colNames[0] != "name" || colNames[1] != "years" {
		t.Errorf("columns after rename = %v, want [name years]", colNames)
	}

	// Data should be preserved.
	count, err := store.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestDropColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age,score\nAlice,30,9.5\nBob,25,8.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatal(err)
	}

	// Drop "score" column.
	if err := store.DropColumn("people", "score"); err != nil {
		t.Fatalf("DropColumn: %v", err)
	}

	cols, err := store.TableColumns("people")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns after drop, got %d", len(cols))
	}
	if cols[0].Name != "name" || cols[1].Name != "age" {
		t.Errorf("columns after drop = [%s %s], want [name age]", cols[0].Name, cols[1].Name)
	}

	// Data should be preserved.
	count, err := store.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestDropColumn_LastColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("only\n1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "single"); err != nil {
		t.Fatal(err)
	}

	// Dropping the last column should error.
	err = store.DropColumn("single", "only")
	if err == nil {
		t.Fatal("expected error when dropping last column, got nil")
	}
}

// ---------- Dedup ----------

func TestDeduplicateTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	csvPath := filepath.Join(dir, "data.csv")
	// Rows 1 and 3 are duplicates.
	if err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\nAlice,30\nCarol,28\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatal(err)
	}

	removed, err := store.DeduplicateTable("people")
	if err != nil {
		t.Fatalf("DeduplicateTable: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	count, err := store.TableRowCount("people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("row count after dedup = %d, want 3", count)
	}
}

func TestDeduplicateTable_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatal(err)
	}

	removed, err := store.DeduplicateTable("people")
	if err != nil {
		t.Fatalf("DeduplicateTable: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}

// ---------- Derived tables and views ----------

func TestCreateTableAs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age,score\nAlice,30,9.5\nBob,25,8.0\nCarol,28,7.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatal(err)
	}

	// Create a derived table with a filter.
	query := "SELECT name, age FROM people WHERE age >= 28"
	if err := store.CreateTableAs("older_people", query); err != nil {
		t.Fatalf("CreateTableAs: %v", err)
	}

	// Check the derived table.
	count, err := store.TableRowCount("older_people")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("derived table row count = %d, want 2", count)
	}

	cols, err := store.TableColumns("older_people")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
}

func TestCreateViewAs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportFile(csvPath, "people"); err != nil {
		t.Fatal(err)
	}

	query := "SELECT name FROM people WHERE age > 26"
	if err := store.CreateViewAs("older_names", query); err != nil {
		t.Fatalf("CreateViewAs: %v", err)
	}

	// View should appear in table names.
	names, err := store.TableNames()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n == "older_names" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("view 'older_names' not in table names: %v", names)
	}

	// Should be marked as a view.
	if !store.IsView("older_names") {
		t.Error("expected 'older_names' to be a view")
	}

	// Query the view.
	_, rows, err := store.ReadOnlyQuery("SELECT * FROM older_names")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("view row count = %d, want 1", len(rows))
	}
	if rows[0][0] != "Alice" {
		t.Errorf("view row[0] = %q, want Alice", rows[0][0])
	}
}

func TestCreateTableAs_InvalidQuery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	// Non-SELECT query should be rejected.
	err = store.CreateTableAs("bad", "DROP TABLE foo")
	if err == nil {
		t.Fatal("expected error for non-SELECT query, got nil")
	}
}

func TestImportCSV_DuplicateTable(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	// First import should succeed.
	if err := store.ImportFile(csvPath, "data"); err != nil {
		t.Fatalf("first ImportFile: %v", err)
	}

	// Second import with same table name should fail.
	err = store.ImportFile(csvPath, "data")
	if err == nil {
		t.Fatal("expected error on duplicate table import, got nil")
	}
}

func TestImportCSV_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "empty.csv")
	// Header only, no data rows.
	if err := os.WriteFile(csvPath, []byte("a,b,c\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	// Should create the table with columns but zero rows.
	if err := store.ImportFile(csvPath, "empty"); err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	count, err := store.TableRowCount("empty")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("row count = %d, want 0", count)
	}

	cols, err := store.TableColumns("empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Errorf("expected 3 columns, got %d", len(cols))
	}
}
