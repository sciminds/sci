package sqlite

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// viewableExtensions lists file extensions that can be viewed directly.
var viewableExtensions = map[string]bool{
	".csv":    true,
	".tsv":    true,
	".json":   true,
	".jsonl":  true,
	".ndjson": true,
}

// IsViewableFile returns true if the file extension is supported for direct viewing.
func IsViewableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return viewableExtensions[ext]
}

// FileView implements [store.DataStore] for viewing and editing flat files
// (CSV, TSV, JSON, JSONL) by importing them into an in-memory SQLite
// database. Mutations are written back to the original file on Close.
type FileView struct {
	filePath  string // absolute path to the original data file
	tableName string // derived from filename
	inner     *Store // backed by in-memory SQLite
	dirty     bool   // true if any mutation has been performed
}

// OpenFileView opens a flat file for viewing and editing through SQLite.
func OpenFileView(path string) (*FileView, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if !viewableExtensions[ext] {
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	name := store.TableNameFromFile(path)

	inner, err := OpenMemory()
	if err != nil {
		return nil, fmt.Errorf("open in-memory db: %w", err)
	}

	var importErr error
	switch ext {
	case ".csv":
		importErr = importCSVFile(inner, absPath, name, ',')
	case ".tsv":
		importErr = importCSVFile(inner, absPath, name, '\t')
	case ".json":
		importErr = importJSONFile(inner, absPath, name)
	case ".jsonl", ".ndjson":
		importErr = importJSONLFile(inner, absPath, name)
	}
	if importErr != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("import %s: %w", filepath.Base(path), importErr)
	}

	return &FileView{filePath: absPath, tableName: name, inner: inner}, nil
}

func importCSVFile(s *Store, path, tableName string, delimiter rune) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(store.DecodeReader(f))
	r.Comma = delimiter
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 1 {
		return fmt.Errorf("file is empty")
	}

	header := store.SanitizeImportHeaders(records[0])
	quotedCols := lo.Map(header, func(col string, _ int) string {
		return fmt.Sprintf(`"%s" TEXT`, col)
	})
	createSQL := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, tableName, strings.Join(quotedCols, ", "))
	if _, err := s.db.Exec(createSQL); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	if len(records) > 1 {
		return s.InsertRows(tableName, header, records[1:])
	}
	return nil
}

func importJSONFile(s *Store, path, tableName string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	if err := json.NewDecoder(store.DecodeReader(f)).Decode(&records); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("json array is empty")
	}
	return importJSONRecords(s, records, tableName)
}

func importJSONLFile(s *Store, path, tableName string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	scanner := bufio.NewScanner(store.DecodeReader(f))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // up to 10 MB per line
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			return fmt.Errorf("parse jsonl: %w", err)
		}
		records = append(records, obj)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read jsonl: %w", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("jsonl file is empty")
	}
	return importJSONRecords(s, records, tableName)
}

func importJSONRecords(s *Store, records []map[string]any, tableName string) error {
	keySet := make(map[string]bool)
	for _, rec := range records {
		for k := range rec {
			keySet[k] = true
		}
	}
	rawKeys := slices.Sorted(maps.Keys(keySet))
	cols := store.SanitizeImportHeaders(rawKeys)

	quotedCols := lo.Map(cols, func(col string, _ int) string {
		return fmt.Sprintf(`"%s" TEXT`, col)
	})
	createSQL := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, tableName, strings.Join(quotedCols, ", "))
	if _, err := s.db.Exec(createSQL); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	rows := make([][]string, len(records))
	for i, rec := range records {
		row := make([]string, len(cols))
		for j, k := range rawKeys {
			if v, ok := rec[k]; ok && v != nil {
				row[j] = fmt.Sprintf("%v", v)
			}
		}
		rows[i] = row
	}
	return s.InsertRows(tableName, cols, rows)
}

// Close implements DataStore. Flushes the in-memory table back to the
// original file if any mutation occurred.
func (s *FileView) Close() error {
	if s.dirty {
		if err := s.inner.ExportCSV(s.tableName, s.filePath); err != nil {
			_ = s.inner.Close()
			return fmt.Errorf("write back to %s: %w", filepath.Base(s.filePath), err)
		}
	}
	return s.inner.Close()
}

// TableNames implements DataStore.
func (s *FileView) TableNames() ([]string, error) { return s.inner.TableNames() }

// TableColumns implements DataStore.
func (s *FileView) TableColumns(t string) ([]store.PragmaColumn, error) {
	return s.inner.TableColumns(t)
}

// TableRowCount implements DataStore.
func (s *FileView) TableRowCount(t string) (int, error) { return s.inner.TableRowCount(t) }

// QueryTable implements DataStore.
func (s *FileView) QueryTable(t string) ([]string, [][]string, [][]bool, []int64, error) {
	return s.inner.QueryTable(t)
}

// ReadOnlyQuery implements DataStore.
func (s *FileView) ReadOnlyQuery(q string) ([]string, [][]string, error) {
	return s.inner.ReadOnlyQuery(q)
}

// TableSummaries implements DataStore.
func (s *FileView) TableSummaries() ([]store.TableSummary, error) {
	return s.inner.TableSummaries()
}

// ExportCSV implements DataStore.
func (s *FileView) ExportCSV(t, p string) error { return s.inner.ExportCSV(t, p) }

// UpdateCell implements DataStore.
func (s *FileView) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	if err := s.inner.UpdateCell(table, column, rowID, pkValues, value); err != nil {
		return err
	}
	s.dirty = true
	return nil
}

// DeleteRows implements DataStore.
func (s *FileView) DeleteRows(table string, ids []store.RowIdentifier) (int64, error) {
	n, err := s.inner.DeleteRows(table, ids)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.dirty = true
	}
	return n, nil
}

// InsertRows implements DataStore.
func (s *FileView) InsertRows(table string, columns []string, rows [][]string) error {
	if err := s.inner.InsertRows(table, columns, rows); err != nil {
		return err
	}
	s.dirty = true
	return nil
}

// RenameTable implements DataStore (not supported for file views).
func (s *FileView) RenameTable(_, _ string) error {
	return fmt.Errorf("rename not supported for file view")
}

// DropTable implements DataStore (not supported for file views).
func (s *FileView) DropTable(_ string) error {
	return fmt.Errorf("drop not supported for file view")
}

// ImportCSV implements DataStore (not supported for file views).
func (s *FileView) ImportCSV(_, _ string) error {
	return fmt.Errorf("import not supported for file view")
}

// AppendCSV implements DataStore (not supported for file views).
func (s *FileView) AppendCSV(_, _ string) error {
	return fmt.Errorf("append not supported for file view")
}

// ImportFile implements DataStore (not supported for file views).
func (s *FileView) ImportFile(_, _ string) error {
	return fmt.Errorf("import not supported for file view")
}

// CreateEmptyTable implements DataStore (not supported for file views).
func (s *FileView) CreateEmptyTable(_ string) error {
	return fmt.Errorf("create table not supported for file view")
}
