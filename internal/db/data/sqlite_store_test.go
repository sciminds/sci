package data

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// Compile-time interface assertions.
var (
	_ DataStore = (*SQLiteStore)(nil)
	_ DataStore = (*FileViewStore)(nil)
)

func TestSQLiteStoreOpenClose(t *testing.T) {
	t.Parallel()
	store, err := OpenMemoryStore()
	if err != nil {
		t.Fatalf("OpenMemoryStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSQLiteStoreSetup(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	if store.db == nil {
		t.Fatal("db is nil")
	}
}

func TestTableNames(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	want := []string{"example", "penguins"}
	if !slices.Equal(names, want) {
		t.Errorf("TableNames = %v, want %v", names, want)
	}
}

func TestTableColumns(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)

	t.Run("penguins", func(t *testing.T) {
		cols, err := store.TableColumns("penguins")
		if err != nil {
			t.Fatalf("TableColumns: %v", err)
		}
		if len(cols) != 4 {
			t.Fatalf("got %d columns, want 4", len(cols))
		}
		if cols[0].Name != "species" || cols[0].PK != 0 {
			t.Errorf("cols[0] = %+v, want species non-PK", cols[0])
		}
	})

	t.Run("example_pk", func(t *testing.T) {
		cols, err := store.TableColumns("example")
		if err != nil {
			t.Fatalf("TableColumns: %v", err)
		}
		if len(cols) != 3 {
			t.Fatalf("got %d columns, want 3", len(cols))
		}
		if cols[0].Name != "id" || cols[0].PK == 0 {
			t.Errorf("cols[0] = %+v, want id with PK", cols[0])
		}
		if !cols[1].NotNull {
			t.Errorf("cols[1].NotNull = false, want true (name is NOT NULL)")
		}
	})

	t.Run("spaced_column_name", func(t *testing.T) {
		if _, err := store.db.NewQuery(`CREATE TABLE signals ("BOLD signal" REAL, trial INTEGER)`).Execute(); err != nil {
			t.Fatalf("create table: %v", err)
		}
		cols, err := store.TableColumns("signals")
		if err != nil {
			t.Fatalf("TableColumns: %v", err)
		}
		if cols[0].Name != "BOLD signal" {
			t.Errorf("cols[0].Name = %q, want %q", cols[0].Name, "BOLD signal")
		}
	})
}

func TestTableRowCount(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	got, err := store.TableRowCount("penguins")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if got != 6 {
		t.Errorf("penguins row count = %d, want 6", got)
	}
	got, err = store.TableRowCount("example")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if got != 3 {
		t.Errorf("example row count = %d, want 3", got)
	}
}

func TestTableSummaries(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	summaries, err := store.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	if summaries[0].Name != "example" || summaries[0].Columns != 3 {
		t.Errorf("summaries[0] = %+v, want example with 3 columns", summaries[0])
	}
	if summaries[1].Name != "penguins" || summaries[1].Columns != 4 {
		t.Errorf("summaries[1] = %+v, want penguins with 4 columns", summaries[1])
	}
}

func TestQueryTable(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	colNames, rows, nullFlags, rowIDs, err := store.QueryTable("penguins")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(colNames) != 4 {
		t.Fatalf("got %d columns, want 4", len(colNames))
	}
	for _, c := range colNames {
		if c == "rowid" {
			t.Error("rowid should not be in column names")
		}
	}
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6", len(rows))
	}
	if len(rowIDs) != 6 {
		t.Fatalf("got %d rowIDs, want 6", len(rowIDs))
	}
	seen := make(map[int64]bool)
	for _, id := range rowIDs {
		if seen[id] {
			t.Errorf("duplicate rowID: %d", id)
		}
		seen[id] = true
	}
	if !nullFlags[2][2] {
		t.Errorf("expected NULL flag for penguins row 2, col 2 (bill)")
	}
	if !nullFlags[4][3] {
		t.Errorf("expected NULL flag for penguins row 4, col 3 (year)")
	}
}

func TestReadOnlyQuery(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)

	t.Run("basic", func(t *testing.T) {
		cols, rows, err := store.ReadOnlyQuery("SELECT species, year FROM penguins WHERE year = 2007")
		if err != nil {
			t.Fatalf("ReadOnlyQuery: %v", err)
		}
		if !slices.Equal(cols, []string{"species", "year"}) {
			t.Errorf("columns = %v, want [species year]", cols)
		}
		if len(rows) != 3 {
			t.Errorf("got %d rows, want 3", len(rows))
		}
	})

	t.Run("max_rows", func(t *testing.T) {
		if _, err := store.db.NewQuery(`
			CREATE TABLE big AS
			WITH RECURSIVE cnt(n) AS (
				SELECT 0 UNION ALL SELECT n+1 FROM cnt WHERE n < 249
			) SELECT n FROM cnt
		`).Execute(); err != nil {
			t.Fatalf("create big table: %v", err)
		}
		_, rows, err := store.ReadOnlyQuery("SELECT * FROM big")
		if err != nil {
			t.Fatalf("ReadOnlyQuery: %v", err)
		}
		if len(rows) != 200 {
			t.Errorf("got %d rows, want 200 (max cap)", len(rows))
		}
	})

	t.Run("rejects_insert", func(t *testing.T) {
		_, _, err := store.ReadOnlyQuery("INSERT INTO penguins VALUES ('X','X',0,0)")
		if err == nil {
			t.Error("expected error for INSERT, got nil")
		}
	})

	t.Run("rejects_drop", func(t *testing.T) {
		_, _, err := store.ReadOnlyQuery("DROP TABLE penguins")
		if err == nil {
			t.Error("expected error for DROP, got nil")
		}
	})

	t.Run("column_order", func(t *testing.T) {
		cols, _, err := store.ReadOnlyQuery("SELECT year, species, bill FROM penguins LIMIT 1")
		if err != nil {
			t.Fatalf("ReadOnlyQuery: %v", err)
		}
		want := []string{"year", "species", "bill"}
		if !slices.Equal(cols, want) {
			t.Errorf("column order = %v, want %v", cols, want)
		}
	})
}

func TestUpdateCell(t *testing.T) {
	t.Parallel()
	t.Run("by_rowid", func(t *testing.T) {
		store := setupTestDB(t)
		_, _, _, rowIDs, err := store.QueryTable("penguins")
		if err != nil {
			t.Fatalf("QueryTable: %v", err)
		}
		newVal := "Modified"
		if err := store.UpdateCell("penguins", "species", rowIDs[0], nil, &newVal); err != nil {
			t.Fatalf("UpdateCell: %v", err)
		}
		_, rows, err := store.ReadOnlyQuery(fmt.Sprintf("SELECT species FROM penguins WHERE rowid = %d", rowIDs[0]))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if len(rows) != 1 || rows[0][0] != "Modified" {
			t.Errorf("got %v, want [[Modified]]", rows)
		}
	})

	t.Run("by_pk", func(t *testing.T) {
		store := setupTestDB(t)
		newVal := "99.9"
		pk := map[string]string{"id": "2"}
		if err := store.UpdateCell("example", "score", 0, pk, &newVal); err != nil {
			t.Fatalf("UpdateCell: %v", err)
		}
		_, rows, err := store.ReadOnlyQuery("SELECT score FROM example WHERE id = 2")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if len(rows) != 1 || rows[0][0] != "99.9" {
			t.Errorf("got %v, want [[99.9]]", rows)
		}
	})

	t.Run("set_null", func(t *testing.T) {
		store := setupTestDB(t)
		_, _, _, rowIDs, err := store.QueryTable("penguins")
		if err != nil {
			t.Fatalf("QueryTable: %v", err)
		}
		if err := store.UpdateCell("penguins", "island", rowIDs[0], nil, nil); err != nil {
			t.Fatalf("UpdateCell: %v", err)
		}
		_, rows, err := store.ReadOnlyQuery(fmt.Sprintf("SELECT island FROM penguins WHERE rowid = %d", rowIDs[0]))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if len(rows) != 1 || rows[0][0] != "" {
			t.Errorf("expected empty string for NULL, got %v", rows)
		}
	})
}

func TestDeleteRows(t *testing.T) {
	t.Parallel()
	t.Run("by_rowid", func(t *testing.T) {
		store := setupTestDB(t)
		_, _, _, rowIDs, err := store.QueryTable("penguins")
		if err != nil {
			t.Fatalf("QueryTable: %v", err)
		}
		n, err := store.DeleteRows("penguins", []RowIdentifier{{RowID: rowIDs[0]}})
		if err != nil {
			t.Fatalf("DeleteRows: %v", err)
		}
		if n != 1 {
			t.Errorf("deleted %d, want 1", n)
		}
		count, _ := store.TableRowCount("penguins")
		if count != 5 {
			t.Errorf("row count = %d, want 5", count)
		}
	})

	t.Run("by_pk", func(t *testing.T) {
		store := setupTestDB(t)
		n, err := store.DeleteRows("example", []RowIdentifier{
			{PKValues: map[string]string{"id": "1"}},
			{PKValues: map[string]string{"id": "3"}},
		})
		if err != nil {
			t.Fatalf("DeleteRows: %v", err)
		}
		if n != 2 {
			t.Errorf("deleted %d, want 2", n)
		}
		count, _ := store.TableRowCount("example")
		if count != 1 {
			t.Errorf("row count = %d, want 1", count)
		}
	})

	t.Run("empty", func(t *testing.T) {
		store := setupTestDB(t)
		n, err := store.DeleteRows("penguins", nil)
		if err != nil {
			t.Fatalf("DeleteRows: %v", err)
		}
		if n != 0 {
			t.Errorf("deleted %d, want 0", n)
		}
	})
}

func TestInsertRows(t *testing.T) {
	t.Parallel()
	t.Run("basic", func(t *testing.T) {
		store := setupTestDB(t)
		err := store.InsertRows("penguins",
			[]string{"species", "island", "bill", "year"},
			[][]string{{"Emperor", "Ross", "55.0", "2010"}, {"King", "Macquarie", "60.1", "2011"}})
		if err != nil {
			t.Fatalf("InsertRows: %v", err)
		}
		count, _ := store.TableRowCount("penguins")
		if count != 8 {
			t.Errorf("row count = %d, want 8", count)
		}
	})

	t.Run("null_handling", func(t *testing.T) {
		store := setupTestDB(t)
		err := store.InsertRows("penguins",
			[]string{"species", "island", "bill", "year"},
			[][]string{{"Emperor", "", "", ""}})
		if err != nil {
			t.Fatalf("InsertRows: %v", err)
		}
		_, rows, err := store.ReadOnlyQuery("SELECT species, island FROM penguins WHERE species = 'Emperor'")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if len(rows) != 1 || rows[0][1] != "" {
			t.Errorf("expected NULL (empty string), got %v", rows)
		}
	})

	t.Run("empty", func(t *testing.T) {
		store := setupTestDB(t)
		err := store.InsertRows("penguins", []string{"species"}, nil)
		if err != nil {
			t.Fatalf("InsertRows: %v", err)
		}
	})

	t.Run("large_batch", func(t *testing.T) {
		store := setupTestDB(t)
		// Insert 1500 rows — exceeds the 999-param limit, so tests multi-statement batching.
		rows := make([][]string, 1500)
		for i := range rows {
			rows[i] = []string{fmt.Sprintf("Species_%d", i), "Island", fmt.Sprintf("%d.0", i), "2020"}
		}
		if err := store.InsertRows("penguins", []string{"species", "island", "bill", "year"}, rows); err != nil {
			t.Fatalf("InsertRows: %v", err)
		}
		count, err := store.TableRowCount("penguins")
		if err != nil {
			t.Fatalf("TableRowCount: %v", err)
		}
		// 6 original + 1500 new
		if count != 1506 {
			t.Errorf("row count = %d, want 1506", count)
		}
	})
}

func TestRenameTable(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	if err := store.RenameTable("penguins", "birds"); err != nil {
		t.Fatalf("RenameTable: %v", err)
	}
	names, _ := store.TableNames()
	if !slices.Contains(names, "birds") {
		t.Errorf("table names = %v, want birds", names)
	}
	if slices.Contains(names, "penguins") {
		t.Error("penguins should be gone after rename")
	}
}

func TestDropTable(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	if err := store.DropTable("example"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}
	names, _ := store.TableNames()
	if slices.Contains(names, "example") {
		t.Error("example should be gone after drop")
	}
	if len(names) != 1 {
		t.Errorf("expected 1 table, got %d", len(names))
	}
}

func TestExportCSV(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/out.csv"
	if err := store.ExportCSV("example", csvPath); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, err := readFileLines(csvPath)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(data) != 4 {
		t.Errorf("csv has %d lines, want 4", len(data))
	}
	if !strings.Contains(data[0], "id") {
		t.Errorf("header = %q, expected to contain 'id'", data[0])
	}
}

func TestImportCSV(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/import.csv"
	writeFile(t, csvPath, "x,y\n1,a\n2,b\n3,c\n")
	if err := store.ImportCSV(csvPath, "imported"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	count, _ := store.TableRowCount("imported")
	if count != 3 {
		t.Errorf("row count = %d, want 3", count)
	}
}

func TestImportCSVStreaming(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/big.csv"

	// Write a CSV with 5000 rows to exercise streaming + batching.
	f, err := os.Create(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("id,name,value\n")
	for i := range 5000 {
		_, _ = fmt.Fprintf(f, "%d,item_%d,%d.5\n", i, i, i)
	}
	_ = f.Close()

	if err := store.ImportCSV(csvPath, "streamed"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}

	count, err := store.TableRowCount("streamed")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 5000 {
		t.Errorf("row count = %d, want 5000", count)
	}

	// Verify first and last rows.
	_, rows, err := store.ReadOnlyQuery("SELECT id, name FROM streamed WHERE id = 0")
	if err != nil {
		t.Fatalf("verify first: %v", err)
	}
	if len(rows) != 1 || rows[0][1] != "item_0" {
		t.Errorf("first row = %v, want [0 item_0]", rows)
	}

	_, rows, err = store.ReadOnlyQuery("SELECT id, name FROM streamed WHERE id = 4999")
	if err != nil {
		t.Fatalf("verify last: %v", err)
	}
	if len(rows) != 1 || rows[0][1] != "item_4999" {
		t.Errorf("last row = %v, want [4999 item_4999]", rows)
	}
}

func TestImportCSVEmpty(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/empty.csv"
	writeFile(t, csvPath, "x,y\n")
	if err := store.ImportCSV(csvPath, "empty_tbl"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	count, _ := store.TableRowCount("empty_tbl")
	if count != 0 {
		t.Errorf("row count = %d, want 0", count)
	}
}

func TestImportCSV_InconsistentFieldCount(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/ragged.csv"
	// Second row has 3 fields instead of 2.
	writeFile(t, csvPath, "x,y\n1,a\n2,b,extra\n3,c\n")

	err := store.ImportCSV(csvPath, "ragged")
	if err == nil {
		t.Fatal("expected error for CSV with inconsistent field count, got nil")
	}
}

func TestImportCSV_UnescapedQuotes(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/badquote.csv"
	// An unescaped quote mid-field is a parse error in Go's strict csv reader.
	writeFile(t, csvPath, "x,y\nhello \"world\",a\n")

	err := store.ImportCSV(csvPath, "badquote")
	if err == nil {
		t.Fatal("expected error for CSV with unescaped quotes, got nil")
	}
}

func TestImportCSV_BOMHeader(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/bom.csv"
	// UTF-8 BOM-prefixed CSV — the exact shape from issue #1.
	writeFile(t, csvPath, "\ufeffBad,Good\n1,2\n")

	if err := store.ImportCSV(csvPath, "bom"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
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

func TestImportCSV_PunctuationHeaders(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/punc.csv"
	writeFile(t, csvPath, "Date (UTC),% complete,temp_°C\n2024-01-01,90,21\n")

	if err := store.ImportCSV(csvPath, "punc"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}

	cols, err := store.TableColumns("punc")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(cols))
	for i, c := range cols {
		got[i] = c.Name
	}
	want := []string{"Date (UTC)", "% complete", "temp_°C"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Errorf("columns = %q, want %q", got, want)
			return
		}
	}
}

func TestImportCSV_UnicodeData(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/unicode.csv"
	writeFile(t, csvPath, "name,city\nМосква,🇷🇺\n日本語,東京\ncafé,Zürich\n")

	if err := store.ImportCSV(csvPath, "unicode"); err != nil {
		t.Fatalf("ImportCSV with unicode: %v", err)
	}

	count, err := store.TableRowCount("unicode")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("row count = %d, want 3", count)
	}

	// Verify round-trip of unicode data.
	_, rows, err := store.ReadOnlyQuery(`SELECT name, city FROM unicode WHERE name = 'café'`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0][1] != "Zürich" {
		t.Errorf("unicode round-trip failed: got %v", rows)
	}
}

func TestImportCSV_HeaderOnly(t *testing.T) {
	t.Parallel()
	// CSV with a header line (no trailing newline, no data rows).
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/headeronly.csv"
	writeFile(t, csvPath, "a,b,c")

	if err := store.ImportCSV(csvPath, "headeronly"); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}

	count, _ := store.TableRowCount("headeronly")
	if count != 0 {
		t.Errorf("row count = %d, want 0", count)
	}

	// Table should exist with the right columns.
	cols, err := store.TableColumns("headeronly")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	want := []string{"a", "b", "c"}
	if !slices.Equal(names, want) {
		t.Errorf("columns = %v, want %v", names, want)
	}
}

func TestImportCSV_InvalidTableName(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	csvPath := t.TempDir() + "/data.csv"
	writeFile(t, csvPath, "x\n1\n")

	err := store.ImportCSV(csvPath, "DROP TABLE; --")
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestImportCSV_NonexistentFile(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)

	err := store.ImportCSV("/nonexistent/path/data.csv", "tbl")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReadOnlyQueryEmptyQuery(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	_, _, err := store.ReadOnlyQuery("")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "empty query") {
		t.Errorf("error %q should mention 'empty query'", err.Error())
	}
}

func TestReadOnlyQuerySemicolon(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	_, _, err := store.ReadOnlyQuery("SELECT 1; DROP TABLE foo")
	if err == nil {
		t.Fatal("expected error for multi-statement query")
	}
}

func TestReadOnlyQueryRejectsWritableCTE(t *testing.T) {
	t.Parallel()
	store := setupTestDB(t)
	_, _, err := store.ReadOnlyQuery("WITH x AS (SELECT 1) INSERT INTO example(id,name) VALUES(99,'hack')")
	if err == nil {
		t.Fatal("expected error for writable CTE")
	}
}

func TestContainsWriteKeyword(t *testing.T) {
	t.Parallel()
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

func TestViewsIncludedInTableNames(t *testing.T) {
	t.Parallel()
	store := setupTestDBWithView(t)
	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if !slices.Contains(names, "penguin_summary") {
		t.Errorf("TableNames = %v, want penguin_summary view included", names)
	}
	// Tables should still be present.
	if !slices.Contains(names, "penguins") {
		t.Errorf("TableNames = %v, want penguins table included", names)
	}
}

func TestIsView(t *testing.T) {
	t.Parallel()
	store := setupTestDBWithView(t)
	// Populate views map via TableNames.
	if _, err := store.TableNames(); err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if !store.IsView("penguin_summary") {
		t.Error("IsView(penguin_summary) = false, want true")
	}
	if store.IsView("penguins") {
		t.Error("IsView(penguins) = true, want false")
	}
	if store.IsView("nonexistent") {
		t.Error("IsView(nonexistent) = true, want false")
	}
}

func TestQueryTableView(t *testing.T) {
	t.Parallel()
	store := setupTestDBWithView(t)
	// Populate views map.
	if _, err := store.TableNames(); err != nil {
		t.Fatalf("TableNames: %v", err)
	}

	colNames, rows, nullFlags, rowIDs, err := store.QueryTable("penguin_summary")
	if err != nil {
		t.Fatalf("QueryTable(view): %v", err)
	}
	if len(colNames) != 2 {
		t.Fatalf("got %d columns, want 2", len(colNames))
	}
	if colNames[0] != "species" || colNames[1] != "cnt" {
		t.Errorf("columns = %v, want [species cnt]", colNames)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if len(rowIDs) != 3 {
		t.Fatalf("got %d rowIDs, want 3", len(rowIDs))
	}
	// Synthetic rowIDs start at 1.
	if rowIDs[0] != 1 || rowIDs[1] != 2 || rowIDs[2] != 3 {
		t.Errorf("rowIDs = %v, want [1 2 3]", rowIDs)
	}
	if len(nullFlags) != 3 {
		t.Fatalf("got %d nullFlags, want 3", len(nullFlags))
	}
}

func TestTableColumnsView(t *testing.T) {
	t.Parallel()
	store := setupTestDBWithView(t)
	cols, err := store.TableColumns("penguin_summary")
	if err != nil {
		t.Fatalf("TableColumns(view): %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	if cols[0].Name != "species" {
		t.Errorf("cols[0].Name = %q, want species", cols[0].Name)
	}
}

func TestTableSummariesManyTables(t *testing.T) {
	t.Parallel()
	store, err := OpenMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Create 20 tables with varying row counts to exercise parallel paths.
	for i := range 20 {
		name := fmt.Sprintf("tbl_%02d", i)
		create := fmt.Sprintf(`CREATE TABLE %s (id INTEGER PRIMARY KEY, val TEXT)`, name)
		if _, err := store.db.NewQuery(create).Execute(); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		// Insert i rows into table i.
		for j := range i {
			ins := fmt.Sprintf(`INSERT INTO %s VALUES (%d, 'row_%d')`, name, j, j)
			if _, err := store.db.NewQuery(ins).Execute(); err != nil {
				t.Fatalf("insert into %s: %v", name, err)
			}
		}
	}

	summaries, err := store.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	if len(summaries) != 20 {
		t.Fatalf("got %d summaries, want 20", len(summaries))
	}

	// Verify sorted order and correct counts.
	for i, s := range summaries {
		wantName := fmt.Sprintf("tbl_%02d", i)
		if s.Name != wantName {
			t.Errorf("summaries[%d].Name = %q, want %q", i, s.Name, wantName)
		}
		if s.Rows != i {
			t.Errorf("summaries[%d].Rows = %d, want %d", i, s.Rows, i)
		}
		if s.Columns != 2 {
			t.Errorf("summaries[%d].Columns = %d, want 2", i, s.Columns)
		}
	}
}

func TestTableSummariesIncludesViews(t *testing.T) {
	t.Parallel()
	store := setupTestDBWithView(t)
	summaries, err := store.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	found := false
	for _, s := range summaries {
		if s.Name == "penguin_summary" {
			found = true
			if s.Rows != 3 {
				t.Errorf("penguin_summary rows = %d, want 3", s.Rows)
			}
			if s.Columns != 2 {
				t.Errorf("penguin_summary columns = %d, want 2", s.Columns)
			}
		}
	}
	if !found {
		t.Error("penguin_summary not in TableSummaries")
	}
}

func TestExportCSVView(t *testing.T) {
	t.Parallel()
	store := setupTestDBWithView(t)
	// Populate views map.
	if _, err := store.TableNames(); err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	csvPath := t.TempDir() + "/view.csv"
	if err := store.ExportCSV("penguin_summary", csvPath); err != nil {
		t.Fatalf("ExportCSV(view): %v", err)
	}
	data, err := readFileLines(csvPath)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	// Header + 3 data rows.
	if len(data) != 4 {
		t.Errorf("csv has %d lines, want 4", len(data))
	}
}

// setupTestDBWithView creates an in-memory DB with tables and a view.
func setupTestDBWithView(t *testing.T) *SQLiteStore {
	t.Helper()
	store := setupTestDB(t)
	viewSQL := `CREATE VIEW penguin_summary AS
		SELECT species, COUNT(*) AS cnt FROM penguins GROUP BY species`
	if _, err := store.db.NewQuery(viewSQL).Execute(); err != nil {
		t.Fatalf("create view: %v", err)
	}
	return store
}

func readFileLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n"), nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func BenchmarkQueryTable(b *testing.B) {
	store, err := OpenMemoryStore()
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	// Create a table with 10K rows and 8 columns.
	if _, err := store.db.NewQuery(`CREATE TABLE bench (
		id INTEGER PRIMARY KEY, a TEXT, b TEXT, c TEXT,
		d TEXT, e TEXT, f TEXT, g TEXT
	)`).Execute(); err != nil {
		b.Fatal(err)
	}
	if _, err := store.db.NewQuery(`INSERT INTO bench
		WITH RECURSIVE cnt(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM cnt WHERE n < 9999)
		SELECT n, 'a'||n, 'b'||n, 'c'||n, 'd'||n, 'e'||n, 'f'||n, 'g'||n FROM cnt`).Execute(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_, _, _, _, err := store.QueryTable("bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanDynamicRows(b *testing.B) {
	store, err := OpenMemoryStore()
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if _, err := store.db.NewQuery(`CREATE TABLE bench2 (
		id INTEGER PRIMARY KEY, a TEXT, b TEXT, c TEXT, d TEXT
	)`).Execute(); err != nil {
		b.Fatal(err)
	}
	if _, err := store.db.NewQuery(`INSERT INTO bench2
		WITH RECURSIVE cnt(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM cnt WHERE n < 4999)
		SELECT n, 'a'||n, 'b'||n, 'c'||n, 'd'||n FROM cnt`).Execute(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		sqlRows, err := store.db.NewQuery(`SELECT * FROM bench2`).Rows()
		if err != nil {
			b.Fatal(err)
		}
		_, _, _, err = scanDynamicRows(sqlRows)
		_ = sqlRows.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func setupTestDB(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenMemoryStore()
	if err != nil {
		t.Fatalf("OpenMemoryStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	schema := `
		CREATE TABLE penguins (species TEXT, island TEXT, bill REAL, year INTEGER);
		INSERT INTO penguins VALUES
			('Adelie','Torgersen',39.1,2007),('Adelie','Biscoe',37.8,2007),
			('Gentoo','Biscoe',NULL,2008),('Chinstrap','Dream',49.6,2009),
			('Gentoo','Biscoe',45.2,NULL),('Adelie','Dream',40.9,2007);
		CREATE TABLE example (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL);
		INSERT INTO example VALUES (1,'Alice',95.5),(2,'Bob',NULL),(3,'Carol',88.0);
	`
	if _, err := store.db.NewQuery(schema).Execute(); err != nil {
		t.Fatalf("setup schema: %v", err)
	}
	return store
}
