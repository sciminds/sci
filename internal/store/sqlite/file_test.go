package sqlite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/store"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

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

func TestFileViewCSV(t *testing.T) {
	t.Parallel()
	testFileView(t, filepath.Join(createFixtureFiles(t), "sample.csv"))
}

func TestFileViewTSV(t *testing.T) {
	t.Parallel()
	testFileView(t, filepath.Join(createFixtureFiles(t), "sample.tsv"))
}

func TestFileViewJSON(t *testing.T) {
	t.Parallel()
	testFileView(t, filepath.Join(createFixtureFiles(t), "sample.json"))
}

func TestFileViewJSONL(t *testing.T) {
	t.Parallel()
	testFileView(t, filepath.Join(createFixtureFiles(t), "sample.jsonl"))
}

func testFileView(t *testing.T, path string) {
	t.Helper()
	s, err := OpenFileView(path)
	if err != nil {
		t.Fatalf("OpenFileView(%s): %v", filepath.Base(path), err)
	}
	defer func() { _ = s.Close() }()

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 1 || names[0] != "sample" {
		t.Fatalf("TableNames = %v, want [sample]", names)
	}

	cols, err := s.TableColumns("sample")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(cols))
	}

	count, err := s.TableRowCount("sample")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}

	qCols, rows, nullFlags, rowIDs, err := s.QueryTable("sample")
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

	summaries, err := s.TableSummaries()
	if err != nil {
		t.Fatalf("TableSummaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Rows != 3 || summaries[0].Columns != 4 {
		t.Errorf("summary = %+v, want {Rows:3, Columns:4}", summaries)
	}
}

func TestFileViewUnsupportedOps(t *testing.T) {
	t.Parallel()
	s, err := OpenFileView(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.RenameTable("sample", "new"); err == nil {
		t.Error("RenameTable should return error")
	}
	if err := s.DropTable("sample"); err == nil {
		t.Error("DropTable should return error")
	}
	if err := s.ImportCSV("x.csv", "t"); err == nil {
		t.Error("ImportCSV should return error")
	}
}

func TestFileViewUpdateCell(t *testing.T) {
	t.Parallel()
	s, err := OpenFileView(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, _, rowIDs, _ := s.QueryTable("sample")
	newVal := "Alicia"
	if err := s.UpdateCell("sample", "name", rowIDs[0], nil, &newVal); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}

	colNames, rows, _, _, _ := s.QueryTable("sample")
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

func TestFileViewDeleteRows(t *testing.T) {
	t.Parallel()
	s, err := OpenFileView(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, _, _, rowIDs, _ := s.QueryTable("sample")
	n, err := s.DeleteRows("sample", []store.RowIdentifier{{RowID: rowIDs[0]}})
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteRows returned %d, want 1", n)
	}
	count, _ := s.TableRowCount("sample")
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

func TestFileViewInsertRows(t *testing.T) {
	t.Parallel()
	s, err := OpenFileView(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.InsertRows("sample", []string{"id", "name", "age", "height"},
		[][]string{{"4", "Dave", "28", "1.70"}})
	if err != nil {
		t.Fatalf("InsertRows: %v", err)
	}
	count, _ := s.TableRowCount("sample")
	if count != 4 {
		t.Errorf("row count = %d, want 4", count)
	}
}

func TestFileViewWriteBack(t *testing.T) {
	t.Parallel()
	dir := createFixtureFiles(t)
	csvPath := filepath.Join(dir, "sample.csv")
	s, err := OpenFileView(csvPath)
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}

	_, _, _, rowIDs, _ := s.QueryTable("sample")
	newVal := "Alicia"
	if err := s.UpdateCell("sample", "name", rowIDs[0], nil, &newVal); err != nil {
		t.Fatalf("UpdateCell: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	content, _ := os.ReadFile(csvPath)
	if !strings.Contains(string(content), "Alicia") {
		t.Error("CSV should contain 'Alicia' after write-back")
	}
}

func TestFileViewNoWriteBackWhenClean(t *testing.T) {
	t.Parallel()
	dir := createFixtureFiles(t)
	csvPath := filepath.Join(dir, "sample.csv")
	original, _ := os.ReadFile(csvPath)

	s, err := OpenFileView(csvPath)
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	_ = s.Close()

	after, _ := os.ReadFile(csvPath)
	if string(original) != string(after) {
		t.Error("CSV should not be modified when no mutations were made")
	}
}

func TestFileViewReadOnlyQuery(t *testing.T) {
	t.Parallel()
	s, err := OpenFileView(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows, err := s.ReadOnlyQuery("SELECT name FROM sample WHERE id = 1")
	if err != nil {
		t.Fatalf("ReadOnlyQuery: %v", err)
	}
	if len(cols) != 1 || len(rows) != 1 || rows[0][0] != "Alice" {
		t.Errorf("got cols=%v rows=%v, want [name] [[Alice]]", cols, rows)
	}
}

func TestFileViewExportCSV(t *testing.T) {
	t.Parallel()
	s, err := OpenFileView(filepath.Join(createFixtureFiles(t), "sample.csv"))
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	outPath := filepath.Join(t.TempDir(), "exported.csv")
	if err := s.ExportCSV("sample", outPath); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, _ := os.ReadFile(outPath)
	if !strings.Contains(string(data), "Alice") || !strings.Contains(string(data), "Bob") {
		t.Error("exported CSV should contain Alice and Bob")
	}
}

func TestFileViewCSV_FilenameWithDashes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "pairs-bom.csv")
	writeFile(t, csvPath, "Bad,Good\n1,2\n")

	s, err := OpenFileView(csvPath)
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	if len(names) != 1 || names[0] != "pairs_bom" {
		t.Errorf("TableNames = %v, want [pairs_bom]", names)
	}
}

func TestFileViewCSV_BOMHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "bom.csv")
	writeFile(t, csvPath, "\ufeffBad,Good\n1,2\n")

	s, err := OpenFileView(csvPath)
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, err := s.TableColumns("bom")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(cols) != 2 || cols[0].Name != "Bad" || cols[1].Name != "Good" {
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		t.Errorf("columns = %q, want [Bad Good]", names)
	}

	count, err := s.TableRowCount("bom")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}

func TestFileViewCSV_HeaderSanitation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "messy.csv")
	writeFile(t, csvPath, "  Name  ,,Date (UTC),temp_°C,Name\nAlice,x,2024-01-01,21,A\n")

	s, err := OpenFileView(csvPath)
	if err != nil {
		t.Fatalf("OpenFileView: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, err := s.TableColumns("messy")
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
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
}

func TestFileViewUnsupportedExt(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "data.xlsx")
	_ = os.WriteFile(tmp, []byte("fake"), 0o644)
	_, err := OpenFileView(tmp)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got %v", err)
	}
}

func TestFileViewNonexistentFile(t *testing.T) {
	t.Parallel()
	_, err := OpenFileView("/tmp/does-not-exist-ever.csv")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestIsViewableFile(t *testing.T) {
	t.Parallel()
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

// BenchmarkImportCSVFileView measures CSV file import into an in-memory SQLite store.
func BenchmarkImportCSVFileView(b *testing.B) {
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
		s, err := OpenFileView(csvPath)
		if err != nil {
			b.Fatal(err)
		}
		_ = s.Close()
	}
}

// BenchmarkImportJSONLFileView measures JSONL file import into an in-memory SQLite store.
func BenchmarkImportJSONLFileView(b *testing.B) {
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
		s, err := OpenFileView(jsonlPath)
		if err != nil {
			b.Fatal(err)
		}
		_ = s.Close()
	}
}
