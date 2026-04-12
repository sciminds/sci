package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteTablesAsCSV_HappyPath feeds a hand-rolled DoclingDocument JSON
// with two tables through the post-processor and verifies the CSV output
// byte-for-byte. Synthetic — no docling dependency.
func TestWriteTablesAsCSV_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "paper.json")
	const doc = `{
      "tables": [
        {
          "data": {
            "num_rows": 2,
            "num_cols": 3,
            "grid": [
              [
                {"text": "name", "column_header": true},
                {"text": "age",  "column_header": true},
                {"text": "city", "column_header": true}
              ],
              [
                {"text": "alice", "column_header": false},
                {"text": "30",    "column_header": false},
                {"text": "NYC",   "column_header": false}
              ]
            ]
          }
        },
        {
          "data": {
            "num_rows": 1,
            "num_cols": 2,
            "grid": [
              [
                {"text": "key",   "column_header": true},
                {"text": "value", "column_header": true}
              ]
            ]
          }
        }
      ]
    }`
	if err := os.WriteFile(jsonPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	csvDir := filepath.Join(dir, "tables")
	paths, err := writeTablesAsCSV(jsonPath, csvDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d table files, want 2", len(paths))
	}
	if !strings.HasSuffix(paths[0], "table-001.csv") {
		t.Errorf("first file = %s, want suffix table-001.csv", paths[0])
	}
	if !strings.HasSuffix(paths[1], "table-002.csv") {
		t.Errorf("second file = %s, want suffix table-002.csv", paths[1])
	}

	// Table 1: full 2x3 grid
	body1, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	const want1 = "name,age,city\nalice,30,NYC\n"
	if string(body1) != want1 {
		t.Errorf("table-001.csv = %q, want %q", body1, want1)
	}

	// Table 2: single row
	body2, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	const want2 = "key,value\n"
	if string(body2) != want2 {
		t.Errorf("table-002.csv = %q, want %q", body2, want2)
	}
}

// Commas and quotes in cell text must be CSV-escaped. encoding/csv does
// this for us but we lock it with a test so a future "optimization" to
// raw string joining gets caught.
func TestWriteTablesAsCSV_EscapesCommasAndQuotes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "weird.json")
	const doc = `{
      "tables": [
        {
          "data": {
            "num_rows": 1,
            "num_cols": 2,
            "grid": [
              [
                {"text": "a, b", "column_header": false},
                {"text": "she said \"hi\"", "column_header": false}
              ]
            ]
          }
        }
      ]
    }`
	if err := os.WriteFile(jsonPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := writeTablesAsCSV(jsonPath, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(paths[0])
	const want = "\"a, b\",\"she said \"\"hi\"\"\"\n"
	if string(body) != want {
		t.Errorf("escaping: got %q, want %q", body, want)
	}
}

func TestWriteTablesAsCSV_NoTables(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(jsonPath, []byte(`{"tables": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := writeTablesAsCSV(jsonPath, filepath.Join(dir, "tables"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("no-table doc produced %d files", len(paths))
	}
	// We should NOT create csvDir if there are no tables to write —
	// avoids leaving empty directories lying around.
	if _, err := os.Stat(filepath.Join(dir, "tables")); err == nil {
		t.Error("empty csvDir should not have been created")
	}
}

func TestWriteTablesAsCSV_MissingJSON(t *testing.T) {
	t.Parallel()
	_, err := writeTablesAsCSV(filepath.Join(t.TempDir(), "nope.json"), t.TempDir())
	if err == nil {
		t.Error("expected error for missing JSON file")
	}
}

func TestWriteTablesAsCSV_MalformedJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(jsonPath, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := writeTablesAsCSV(jsonPath, filepath.Join(dir, "out"))
	if err == nil {
		t.Error("expected parse error")
	}
}
