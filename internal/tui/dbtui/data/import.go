package data

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

// WriteRowsCSV writes a header and rows to a CSV file.
func WriteRowsCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// RenameColumn renames a column in a SQLite table.
func (s *Store) RenameColumn(table, oldName, newName string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !IsSafeColumnName(oldName) {
		return fmt.Errorf("invalid column name: %q", oldName)
	}
	if !IsSafeColumnName(newName) {
		return fmt.Errorf("invalid column name: %q", newName)
	}
	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %q RENAME COLUMN %q TO %q", table, oldName, newName))
	return err
}

// DropColumn drops a column from a SQLite table.
// Returns an error if it would remove the last column.
func (s *Store) DropColumn(table, column string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !IsSafeColumnName(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}
	cols, err := s.TableColumns(table)
	if err != nil {
		return err
	}
	if len(cols) <= 1 {
		return fmt.Errorf("cannot drop the last column of %q", table)
	}
	_, err = s.db.Exec(fmt.Sprintf("ALTER TABLE %q DROP COLUMN %q", table, column))
	return err
}

// DeduplicateTable removes duplicate rows from a table, keeping the row
// with the lowest rowid. Returns the number of rows removed.
func (s *Store) DeduplicateTable(table string) (int64, error) {
	if !IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}

	cols, err := s.TableColumns(table)
	if err != nil {
		return 0, err
	}

	// Build column list for grouping.
	colNames := lo.Map(cols, func(c PragmaColumn, _ int) string {
		return fmt.Sprintf("%q", c.Name)
	})
	colList := strings.Join(colNames, ", ")

	// Delete rows where rowid is not the minimum for each unique combination.
	query := fmt.Sprintf(
		"DELETE FROM %q WHERE rowid NOT IN (SELECT MIN(rowid) FROM %q GROUP BY %s)",
		table, table, colList)

	result, err := s.db.Exec(query)
	if err != nil {
		return 0, fmt.Errorf("dedup %q: %w", table, err)
	}
	return result.RowsAffected()
}

// CreateTableAs creates a new table from a SELECT query.
func (s *Store) CreateTableAs(tableName, query string) error {
	if !IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}
	validated, err := ValidateReadOnlySQL(query)
	if err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf("CREATE TABLE %q AS %s", tableName, validated))
	return err
}

// CreateViewAs creates a new view from a SELECT query.
func (s *Store) CreateViewAs(viewName, query string) error {
	if !IsSafeIdentifier(viewName) {
		return fmt.Errorf("invalid view name: %q", viewName)
	}
	validated, err := ValidateReadOnlySQL(query)
	if err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf("CREATE VIEW %q AS %s", viewName, validated))
	return err
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
// Strips a leading BOM (UTF-8 or UTF-16 — see [DecodeReader]) and sanitizes
// the header so real-world Excel exports import without manual cleanup.
func readCSV(path string, delimiter rune) (header []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(DecodeReader(f))
	r.Comma = delimiter
	r.LazyQuotes = true

	header, err = r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	header = SanitizeImportHeaders(header)

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
// Key order is taken from the first object; subsequent objects may have
// additional keys appended.
func readJSON(path string) (header []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	if err := json.NewDecoder(DecodeReader(f)).Decode(&records); err != nil {
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
	dec := json.NewDecoder(DecodeReader(f))
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

	// Collect keys in insertion order from first record, then add any new keys.
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
	header = SanitizeImportHeaders(header)
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

// importTabular creates a typed SQLite table from header + rows.
func (s *Store) importTabular(tableName string, header []string, rows [][]string) error {
	if !IsSafeIdentifier(tableName) {
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

	// Build CREATE TABLE.
	colDefs := lo.Map(header, func(name string, i int) string {
		return fmt.Sprintf("%q %s", name, types[i])
	})
	createSQL := fmt.Sprintf("CREATE TABLE %q (%s)", tableName, strings.Join(colDefs, ", "))
	if _, err := s.db.Exec(createSQL); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	// Insert rows in a transaction.
	if len(rows) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	placeholders := make([]string, len(header))
	for i := range header {
		placeholders[i] = "?"
	}
	quotedCols := lo.Map(header, func(name string, _ int) string {
		return fmt.Sprintf("%q", name)
	})
	insertSQL := fmt.Sprintf("INSERT INTO %q (%s) VALUES (%s)",
		tableName, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, row := range rows {
		args := make([]any, len(header))
		for i := range header {
			if i < len(row) && row[i] != "" {
				args[i] = row[i]
			} else {
				args[i] = nil
			}
		}
		if _, err := stmt.Exec(args...); err != nil {
			return fmt.Errorf("insert row: %w", err)
		}
	}

	return tx.Commit()
}
