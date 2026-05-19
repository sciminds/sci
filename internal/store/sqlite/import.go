package sqlite

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// ImportCSV imports a CSV file as a new typed SQLite table.
func (s *Store) ImportCSV(csvPath, tableName string) error {
	header, rows, err := readCSV(csvPath, ',')
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}
	return s.importTabular(tableName, header, rows)
}

// ImportFile imports a file as a new typed SQLite table, auto-detecting format.
func (s *Store) ImportFile(filePath, tableName string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".csv":
		return s.ImportCSV(filePath, tableName)
	case ".tsv":
		header, rows, err := readCSV(filePath, '\t')
		if err != nil {
			return fmt.Errorf("read TSV: %w", err)
		}
		return s.importTabular(tableName, header, rows)
	case ".json":
		header, rows, err := readJSON(filePath)
		if err != nil {
			return fmt.Errorf("read JSON: %w", err)
		}
		return s.importTabular(tableName, header, rows)
	case ".jsonl", ".ndjson":
		header, rows, err := readJSONL(filePath)
		if err != nil {
			return fmt.Errorf("read JSONL: %w", err)
		}
		return s.importTabular(tableName, header, rows)
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}
}

// AppendCSV appends the rows of csvPath into the existing tableName.
// Returns an error if the table does not exist. Column matching is by
// position via the CSV header; mismatched schemas surface as a SQLite
// error from the INSERT.
func (s *Store) AppendCSV(csvPath, tableName string) error {
	if !store.IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}
	exists, err := s.tableExists(tableName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("table %q does not exist", tableName)
	}
	header, rows, err := readCSV(csvPath, ',')
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}
	return s.insertRows(tableName, header, rows)
}

// importTabular creates a typed SQLite table from header + rows.
func (s *Store) importTabular(tableName string, header []string, rows [][]string) error {
	if !store.IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}

	// Infer types by sampling all values in each column.
	types := make([]string, len(header))
	for i := range header {
		colVals := make([]string, len(rows))
		for j, row := range rows {
			if i < len(row) {
				colVals[j] = row[i]
			}
		}
		types[i] = inferColumnType(colVals)
	}

	colDefs := lo.Map(header, func(name string, i int) string {
		return fmt.Sprintf("%q %s", name, types[i])
	})
	createSQL := fmt.Sprintf("CREATE TABLE %q (%s)", tableName, strings.Join(colDefs, ", "))
	if _, err := s.db.Exec(createSQL); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	return s.insertRows(tableName, header, rows)
}

// inferColumnType examines sample values and returns the narrowest SQLite type.
// Empty strings are treated as NULLs and ignored. If all values are empty,
// returns TEXT. Widening order: INTEGER → REAL → TEXT.
func inferColumnType(vals []string) string {
	hasInt := false
	hasReal := false
	hasText := false

	for _, v := range vals {
		if v == "" {
			continue
		}
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			hasInt = true
			continue
		}
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			hasReal = true
			continue
		}
		hasText = true
	}

	if hasText || (!hasInt && !hasReal) {
		return "TEXT"
	}
	if hasReal {
		return "REAL"
	}
	return "INTEGER"
}

// readCSV reads a CSV (or TSV) file and returns the header and all rows.
// Strips a leading BOM (UTF-8 or UTF-16 — see [store.DecodeReader]) and
// sanitizes the header so real-world Excel exports import without manual
// cleanup.
func readCSV(path string, delimiter rune) (header []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(store.DecodeReader(f))
	r.Comma = delimiter
	r.LazyQuotes = true

	header, err = r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	header = store.SanitizeImportHeaders(header)

	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read row: %w", err)
		}
		rows = append(rows, record)
	}
	return header, rows, nil
}

// readJSON reads a JSON array of objects and returns header + rows.
func readJSON(path string) (header []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	if err := json.NewDecoder(store.DecodeReader(f)).Decode(&records); err != nil {
		return nil, nil, fmt.Errorf("decode JSON array: %w", err)
	}

	return flattenRecords(records)
}

// readJSONL reads newline-delimited JSON objects.
func readJSONL(path string) (header []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	dec := json.NewDecoder(store.DecodeReader(f))
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err == io.EOF {
			break
		} else if err != nil {
			return nil, nil, fmt.Errorf("decode JSONL: %w", err)
		}
		records = append(records, obj)
	}

	return flattenRecords(records)
}

// flattenRecords converts a slice of JSON objects into a header + string rows.
func flattenRecords(records []map[string]any) (header []string, rows [][]string, err error) {
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("no records found")
	}

	colIndex := make(map[string]int)
	for _, rec := range records {
		for k := range rec {
			if _, ok := colIndex[k]; !ok {
				colIndex[k] = len(header)
				header = append(header, k)
			}
		}
	}

	for _, rec := range records {
		row := make([]string, len(header))
		for k, v := range rec {
			row[colIndex[k]] = jsonValueToString(v)
		}
		rows = append(rows, row)
	}
	header = store.SanitizeImportHeaders(header)
	return header, rows, nil
}

// jsonValueToString converts a JSON value to its string representation.
// Numbers that look like integers are formatted without decimal points.
func jsonValueToString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}
