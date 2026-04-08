package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createFixtureFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "sample.csv"),
		"id,name,age,height\n1,Alice,30,1.75\n2,Bob,25,1.80\n3,Carol,35,1.65\n")
	writeFile(t, filepath.Join(dir, "sample.tsv"),
		"id\tname\tage\theight\n1\tAlice\t30\t1.75\n2\tBob\t25\t1.80\n3\tCarol\t35\t1.65\n")

	records := []map[string]any{
		{"id": 1, "name": "Alice", "age": 30, "height": 1.75},
		{"id": 2, "name": "Bob", "age": 25, "height": 1.80},
		{"id": 3, "name": "Carol", "age": 35, "height": 1.65},
	}
	jsonData, _ := json.Marshal(records)
	writeFile(t, filepath.Join(dir, "sample.json"), string(jsonData))

	var jsonlLines []string
	for _, rec := range records {
		line, _ := json.Marshal(rec)
		jsonlLines = append(jsonlLines, string(line))
	}
	writeFile(t, filepath.Join(dir, "sample.jsonl"), strings.Join(jsonlLines, "\n")+"\n")

	return dir
}

func TestFileViewStoreCSV(t *testing.T) {
	testFileStore(t, filepath.Join(createFixtureFiles(t), "sample.csv"))
}

func TestFileViewStoreTSV(t *testing.T) {
	testFileStore(t, filepath.Join(createFixtureFiles(t), "sample.tsv"))
}

func TestFileViewStoreJSON(t *testing.T) {
	testFileStore(t, filepath.Join(createFixtureFiles(t), "sample.json"))
}

func TestFileViewStoreJSONL(t *testing.T) {
	testFileStore(t, filepath.Join(createFixtureFiles(t), "sample.jsonl"))
}

func testFileStore(t *testing.T, path string) {
	t.Helper()
	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("OpenFileStore(%s): %v", filepath.Base(path), err)
	}
	defer func() { _ = store.Close() }()

	names, err := store.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 1 || names[0] != "sample" {
		t.Fatalf("TableNames = %v, want [sample]", names)
	}

	cols, err := store.TableColumns("sample")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(cols))
	}

	count, err := store.TableRowCount("sample")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}

	qCols, rows, nullFlags, rowIDs, err := store.QueryTable("sample")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(qCols) != 4 || len(rows) != 3 || len(nullFlags) != 3 || len(rowIDs) != 3 {
		t.Errorf("QueryTable: cols=%d rows=%d nullFlags=%d rowIDs=%d", len(qCols), len(rows), len(nullFlags), len(rowIDs))
	}

	nameIdx := -1
	for i, c := range qCols {
		if strings.EqualFold(c, "name") {
			nameIdx = i
			break
		}
	}
	if nameIdx >= 0 && len(rows) > 0 && rows[0][nameIdx] != "Alice" {
		t.Errorf("first row name = %q, want Alice", rows[0][nameIdx])
	}

	summaries, err := store.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Rows != 3 || summaries[0].Columns != 4 {
		t.Errorf("summary = %+v, want {Rows:3, Columns:4}", summaries)
	}
}

func TestFileViewStoreUnsupportedOps(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.RenameTable("sample", "new"); err == nil {
		t.Error("RenameTable should return error")
	}
	if err := store.DropTable("sample"); err == nil {
		t.Error("DropTable should return error")
	}
	if err := store.ImportCSV("x.csv", "t"); err == nil {
		t.Error("ImportCSV should return error")
	}
}

func TestFileViewStoreUpdateCell(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, _, rowIDs, _ := store.QueryTable("sample")
	newVal := "Alicia"
	if err := store.UpdateCell("sample", "name", rowIDs[0], nil, &newVal); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}

	colNames, rows, _, _, _ := store.QueryTable("sample")
	nameIdx := -1
	for i, c := range colNames {
		if strings.EqualFold(c, "name") {
			nameIdx = i
			break
		}
	}
	if nameIdx >= 0 && rows[0][nameIdx] != "Alicia" {
		t.Errorf("row 0 name = %q, want Alicia", rows[0][nameIdx])
	}
}

func TestFileViewStoreDeleteRows(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, _, _, rowIDs, _ := store.QueryTable("sample")
	n, err := store.DeleteRows("sample", []RowIdentifier{{RowID: rowIDs[0]}})
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteRows returned %d, want 1", n)
	}
	count, _ := store.TableRowCount("sample")
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestFileViewStoreInsertRows(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	err = store.InsertRows("sample", []string{"id", "name", "age", "height"},
		[][]string{{"4", "Dave", "28", "1.70"}})
	if err != nil {
		t.Fatalf("InsertRows: %v", err)
	}
	count, _ := store.TableRowCount("sample")
	if count != 4 {
		t.Errorf("row count = %d, want 4", count)
	}
}

func TestFileViewStoreWriteBack(t *testing.T) {
	dir := createFixtureFiles(t)
	csvPath := filepath.Join(dir, "sample.csv")
	store, err := OpenFileStore(csvPath)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	_, _, _, rowIDs, _ := store.QueryTable("sample")
	newVal := "Alicia"
	if err := store.UpdateCell("sample", "name", rowIDs[0], nil, &newVal); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	content, _ := os.ReadFile(csvPath)
	if !strings.Contains(string(content), "Alicia") {
		t.Error("CSV should contain 'Alicia' after write-back")
	}
}

func TestFileViewStoreNoWriteBackWhenClean(t *testing.T) {
	dir := createFixtureFiles(t)
	csvPath := filepath.Join(dir, "sample.csv")
	original, _ := os.ReadFile(csvPath)

	store, err := OpenFileStore(csvPath)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	_ = store.Close()

	after, _ := os.ReadFile(csvPath)
	if string(original) != string(after) {
		t.Error("CSV should not be modified when no mutations were made")
	}
}

func TestFileViewStoreReadOnlyQuery(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	cols, rows, err := store.ReadOnlyQuery("SELECT name FROM sample WHERE id = 1")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(cols) != 1 || len(rows) != 1 || rows[0][0] != "Alice" {
		t.Errorf("got cols=%v rows=%v, want [name] [[Alice]]", cols, rows)
	}
}

func TestFileViewStoreExportCSV(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	outPath := filepath.Join(t.TempDir(), "exported.csv")
	if err := store.ExportCSV("sample", outPath); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, _ := os.ReadFile(outPath)
	if !strings.Contains(string(data), "Alice") || !strings.Contains(string(data), "Bob") {
		t.Error("exported CSV should contain Alice and Bob")
	}
}

func TestFileViewStoreUnsupportedExt(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "data.xlsx")
	_ = os.WriteFile(tmp, []byte("fake"), 0o644)
	_, err := OpenFileStore(tmp)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got %v", err)
	}
}

func TestFileViewStoreNonexistentFile(t *testing.T) {
	_, err := OpenFileStore("/tmp/does-not-exist-ever.csv")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestIsViewableFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"data.csv", true}, {"data.CSV", true}, {"data.tsv", true},
		{"data.json", true}, {"data.jsonl", true}, {"data.ndjson", true},
		{"data.parquet", false}, {"data.pq", false},
		{"data.duckdb", false}, {"data.db", false},
		{"data.xlsx", false}, {"data.txt", false}, {"data", false},
	}
	for _, tt := range tests {
		if got := IsViewableFile(tt.path); got != tt.want {
			t.Errorf("IsViewableFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// BenchmarkImportCSV measures CSV file import into an in-memory SQLite store.
func BenchmarkImportCSV(b *testing.B) {
	dir := b.TempDir()
	csvPath := filepath.Join(dir, "bench.csv")
	var sb strings.Builder
	sb.WriteString("id,name,value\n")
	for i := range 1000 {
		fmt.Fprintf(&sb, "%d,item_%d,%d.%02d\n", i, i, i*10, i%100)
	}
	if err := os.WriteFile(csvPath, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		store, err := OpenFileStore(csvPath)
		if err != nil {
			b.Fatal(err)
		}
		_ = store.Close()
	}
}

// BenchmarkImportJSONL measures JSONL file import into an in-memory SQLite store.
func BenchmarkImportJSONL(b *testing.B) {
	dir := b.TempDir()
	jsonlPath := filepath.Join(dir, "bench.jsonl")
	var sb strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&sb, `{"id":%d,"name":"item_%d","value":%d.%02d}`+"\n", i, i, i*10, i%100)
	}
	if err := os.WriteFile(jsonlPath, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		store, err := OpenFileStore(jsonlPath)
		if err != nil {
			b.Fatal(err)
		}
		_ = store.Close()
	}
}
