package data

// file_store.go — [FileViewStore] wraps [SQLiteStore] to view flat files
// (CSV, TSV, JSON, JSONL) as read-only virtual tables in the TUI.

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

// FileViewStore implements DataStore for viewing and editing flat files
// (CSV, TSV, JSON, JSONL) by importing them into an in-memory SQLite
// database. Mutations are written back to the original file on Close.
type FileViewStore struct {
	filePath  string       // absolute path to the original data file
	tableName string       // derived from filename
	inner     *SQLiteStore // backed by in-memory SQLite
	dirty     bool         // true if any mutation has been performed
}

// OpenFileStore opens a flat file for viewing and editing through SQLite.
func OpenFileStore(path string) (*FileViewStore, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if !viewableExtensions[ext] {
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	inner, err := OpenMemoryStore()
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

	return &FileViewStore{filePath: absPath, tableName: name, inner: inner}, nil
}

func importCSVFile(store *SQLiteStore, path, tableName string, delimiter rune) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.Comma = delimiter
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 1 {
		return fmt.Errorf("file is empty")
	}

	header := records[0]
	quotedCols := lo.Map(header, func(col string, _ int) string {
		return fmt.Sprintf(`"%s" TEXT`, col)
	})
	createSQL := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, tableName, strings.Join(quotedCols, ", "))
	if _, err := store.db.NewQuery(createSQL).Execute(); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	if len(records) > 1 {
		return store.InsertRows(tableName, header, records[1:])
	}
	return nil
}

func importJSONFile(store *SQLiteStore, path, tableName string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	if err := json.NewDecoder(f).Decode(&records); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("json array is empty")
	}
	return importJSONRecords(store, records, tableName)
}

func importJSONLFile(store *SQLiteStore, path, tableName string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var records []map[string]any
	scanner := bufio.NewScanner(f)
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
	return importJSONRecords(store, records, tableName)
}

func importJSONRecords(store *SQLiteStore, records []map[string]any, tableName string) error {
	keySet := make(map[string]bool)
	for _, rec := range records {
		for k := range rec {
			keySet[k] = true
		}
	}
	keys := slices.Sorted(maps.Keys(keySet))

	quotedCols := lo.Map(keys, func(col string, _ int) string {
		return fmt.Sprintf(`"%s" TEXT`, col)
	})
	createSQL := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, tableName, strings.Join(quotedCols, ", "))
	if _, err := store.db.NewQuery(createSQL).Execute(); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	rows := make([][]string, len(records))
	for i, rec := range records {
		row := make([]string, len(keys))
		for j, k := range keys {
			if v, ok := rec[k]; ok && v != nil {
				row[j] = fmt.Sprintf("%v", v)
			}
		}
		rows[i] = row
	}
	return store.InsertRows(tableName, keys, rows)
}

func (s *FileViewStore) Close() error {
	if s.dirty {
		if err := s.inner.ExportCSV(s.tableName, s.filePath); err != nil {
			_ = s.inner.Close()
			return fmt.Errorf("write back to %s: %w", filepath.Base(s.filePath), err)
		}
	}
	return s.inner.Close()
}

func (s *FileViewStore) TableNames() ([]string, error) { return s.inner.TableNames() }
func (s *FileViewStore) TableColumns(t string) ([]PragmaColumn, error) {
	return s.inner.TableColumns(t)
}
func (s *FileViewStore) TableRowCount(t string) (int, error) { return s.inner.TableRowCount(t) }

func (s *FileViewStore) QueryTable(t string) ([]string, [][]string, [][]bool, []int64, error) {
	return s.inner.QueryTable(t)
}

func (s *FileViewStore) ReadOnlyQuery(q string) ([]string, [][]string, error) {
	return s.inner.ReadOnlyQuery(q)
}

func (s *FileViewStore) TableSummaries() ([]TableSummary, error) { return s.inner.TableSummaries() }
func (s *FileViewStore) ExportCSV(t, p string) error             { return s.inner.ExportCSV(t, p) }

func (s *FileViewStore) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	if err := s.inner.UpdateCell(table, column, rowID, pkValues, value); err != nil {
		return err
	}
	s.dirty = true
	return nil
}

func (s *FileViewStore) DeleteRows(table string, ids []RowIdentifier) (int64, error) {
	n, err := s.inner.DeleteRows(table, ids)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.dirty = true
	}
	return n, nil
}

func (s *FileViewStore) InsertRows(table string, columns []string, rows [][]string) error {
	if err := s.inner.InsertRows(table, columns, rows); err != nil {
		return err
	}
	s.dirty = true
	return nil
}

func (s *FileViewStore) RenameTable(_, _ string) error {
	return fmt.Errorf("rename not supported for file view")
}
func (s *FileViewStore) DropTable(_ string) error {
	return fmt.Errorf("drop not supported for file view")
}
func (s *FileViewStore) ImportCSV(_, _ string) error {
	return fmt.Errorf("import not supported for file view")
}
func (s *FileViewStore) ImportFile(_, _ string) error {
	return fmt.Errorf("import not supported for file view")
}
func (s *FileViewStore) CreateEmptyTable(_ string) error {
	return fmt.Errorf("create table not supported for file view")
}
