package data

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"time"

	"github.com/samber/lo"

	_ "modernc.org/sqlite"
)

// DateLayout is the Go time layout for ISO date formatting.
const DateLayout = "2006-01-02"

// Store wraps a raw database/sql connection to any SQLite file.
type Store struct {
	db       *sql.DB
	views    map[string]bool // populated by TableNames
	virtuals map[string]bool // populated by TableNames
	shadows  map[string]bool // populated by TableNames (FTS5 internal tables)
}

// Open opens a SQLite database at the given path with WAL mode.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Exec executes a SQL statement that does not return rows.
func (s *Store) Exec(query string) (sql.Result, error) {
	return s.db.Exec(query)
}

// TableSummaries returns all table names with row counts and column counts.
// Row counts are fetched in a single UNION ALL query to minimize round trips.
func (s *Store) TableSummaries() ([]TableSummary, error) {
	names, err := s.TableNames()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}

	// Build a single query for all row counts.
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString(" UNION ALL ")
		}
		fmt.Fprintf(&b, "SELECT %q AS name, COUNT(*) AS cnt FROM %q", name, name)
	}
	rows, err := s.db.Query(b.String())
	if err != nil {
		return nil, fmt.Errorf("table summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	countMap := make(map[string]int, len(names))
	for rows.Next() {
		var name string
		var cnt int
		if err := rows.Scan(&name, &cnt); err != nil {
			return nil, err
		}
		countMap[name] = cnt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Column counts still require one PRAGMA per table (unavoidable in SQLite).
	summaries := make([]TableSummary, 0, len(names))
	for _, name := range names {
		cols, _ := s.TableColumns(name)
		summaries = append(summaries, TableSummary{
			Name:    name,
			Rows:    countMap[name],
			Columns: len(cols),
		})
	}
	return summaries, nil
}

// TableNames returns the names of all non-internal tables and views in the database.
func (s *Store) TableNames() ([]string, error) {
	rows, err := s.db.Query(
		"SELECT name, type, sql FROM sqlite_master WHERE type IN ('table', 'view') " +
			"AND name NOT LIKE 'sqlite_%' ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	s.views = make(map[string]bool)
	s.virtuals = make(map[string]bool)
	s.shadows = make(map[string]bool)
	var names []string
	for rows.Next() {
		var name, typ string
		var ddl sql.NullString
		if err := rows.Scan(&name, &typ, &ddl); err != nil {
			return nil, err
		}
		names = append(names, name)
		switch {
		case typ == "view":
			s.views[name] = true
		case ddl.Valid && strings.HasPrefix(strings.ToUpper(ddl.String), "CREATE VIRTUAL TABLE"):
			s.virtuals[name] = true
		case !ddl.Valid:
			// Shadow tables (e.g. FTS5 _config, _data) have NULL sql.
			s.shadows[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Mark shadow tables: any table whose name starts with a known virtual
	// table name + "_" is a shadow table (e.g. fts5: X_config, X_data, …).
	for _, name := range names {
		if s.views[name] || s.virtuals[name] || s.shadows[name] {
			continue
		}
		for vt := range s.virtuals {
			if strings.HasPrefix(name, vt+"_") {
				s.shadows[name] = true
				break
			}
		}
	}
	// Filter out shadow tables — they contain binary blobs (FTS index data)
	// and are not useful to view or edit.
	return lo.Reject(names, func(name string, _ int) bool {
		return s.shadows[name]
	}), nil
}

// IsView reports whether name is a SQL view (not a table).
func (s *Store) IsView(name string) bool {
	return s.views[name]
}

// IsVirtual reports whether name is a virtual table (e.g. FTS5, WITHOUT ROWID shadow tables).
func (s *Store) IsVirtual(name string) bool {
	return s.virtuals[name]
}

// TableColumns returns column metadata for the named table via PRAGMA.
func (s *Store) TableColumns(table string) ([]PragmaColumn, error) {
	if !IsSafeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []PragmaColumn
	for rows.Next() {
		var c PragmaColumn
		if err := rows.Scan(&c.CID, &c.Name, &c.Type, &c.NotNull, &c.DfltValue, &c.PK); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// TableRowCount returns the number of rows in the named table.
func (s *Store) TableRowCount(table string) (int, error) {
	if !IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}
	var count int
	err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", table)).Scan(&count)
	return count, err
}

// QueryTable returns all rows from the named table as string slices.
// Columns are returned in PRAGMA table_info order. NULL values are
// represented by the null sentinel. Each row's SQLite rowid is returned
// in the rowIDs slice for use in UPDATE/DELETE.
func (s *Store) QueryTable(table string) (colNames []string, rows [][]string, nullFlags [][]bool, rowIDs []int64, err error) {
	if !IsSafeIdentifier(table) {
		return nil, nil, nil, nil, fmt.Errorf("invalid table name: %q", table)
	}

	// Views and virtual tables don't have rowid — use a synthetic counter instead.
	if s.IsView(table) || s.IsVirtual(table) {
		return s.queryView(table)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sqlRows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT rowid, * FROM %q LIMIT %d", table, MaxTableRows))
	if err != nil {
		// Virtual tables (FTS shadow tables, WITHOUT ROWID tables, etc.)
		// lack a rowid column. Fall back to the view path with synthetic IDs.
		cancel()
		return s.queryView(table)
	}
	defer func() { _ = sqlRows.Close() }()

	allCols, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("columns %q: %w", table, err)
	}
	// First column is rowid; the rest are user columns.
	colNames = allCols[1:]

	for sqlRows.Next() {
		var rowID int64
		values := make([]any, len(colNames))
		ptrs := make([]any, len(colNames)+1)
		ptrs[0] = &rowID
		for i := range values {
			ptrs[i+1] = &values[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("scan %q: %w", table, err)
		}
		row := make([]string, len(colNames))
		nf := make([]bool, len(colNames))
		for i, v := range values {
			if v == nil {
				nf[i] = true
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		rows = append(rows, row)
		nullFlags = append(nullFlags, nf)
		rowIDs = append(rowIDs, rowID)
	}
	return colNames, rows, nullFlags, rowIDs, sqlRows.Err()
}

// queryView queries a SQL view without rowid, using synthetic row IDs.
func (s *Store) queryView(view string) (colNames []string, rows [][]string, nullFlags [][]bool, rowIDs []int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sqlRows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %q LIMIT %d", view, MaxTableRows))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("query view %q: %w", view, err)
	}
	defer func() { _ = sqlRows.Close() }()

	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("columns %q: %w", view, err)
	}

	var counter int64
	for sqlRows.Next() {
		counter++
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("scan view %q: %w", view, err)
		}
		row := make([]string, len(cols))
		nf := make([]bool, len(cols))
		for i, v := range values {
			if v == nil {
				nf[i] = true
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		rows = append(rows, row)
		nullFlags = append(nullFlags, nf)
		rowIDs = append(rowIDs, counter)
	}
	return cols, rows, nullFlags, rowIDs, sqlRows.Err()
}

// ReadOnlyQuery executes a validated SELECT query and returns results.
func (s *Store) ReadOnlyQuery(query string) (columns []string, rows [][]string, err error) {
	trimmed, err := ValidateReadOnlySQL(query)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sqlRows, err := s.db.QueryContext(ctx, trimmed)
	if err != nil {
		return nil, nil, fmt.Errorf("execute query: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	columns, err = sqlRows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("get columns: %w", err)
	}

	const maxRows = 200
	for sqlRows.Next() {
		if len(rows) >= maxRows {
			break
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, fmt.Errorf("scan row: %w", err)
		}
		row := make([]string, len(columns))
		for i, v := range values {
			if v == nil {
				row[i] = ""
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		rows = append(rows, row)
	}
	return columns, rows, sqlRows.Err()
}

// UpdateCell updates a single cell value by rowid.
func (s *Store) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !IsSafeIdentifier(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}
	query := fmt.Sprintf("UPDATE %q SET %q = ? WHERE rowid = ?", table, column)
	var arg any
	if value == nil {
		arg = nil
	} else {
		arg = *value
	}
	result, err := s.db.Exec(query, arg, rowID)
	if err != nil {
		return fmt.Errorf("update %q.%q: %w", table, column, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("no row with rowid %d in %q", rowID, table)
	}
	return nil
}

// DeleteRows removes rows by rowid and returns the count of deleted rows.
func (s *Store) DeleteRows(table string, ids []RowIdentifier) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if !IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id.RowID
	}
	query := fmt.Sprintf("DELETE FROM %q WHERE rowid IN (%s)", table, strings.Join(placeholders, ","))
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete from %q: %w", table, err)
	}
	return result.RowsAffected()
}

// InsertRows inserts multiple rows. Empty string values are inserted as NULL.
func (s *Store) InsertRows(table string, columns []string, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	for _, col := range columns {
		if !IsSafeIdentifier(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
	}

	quotedCols := lo.Map(columns, func(c string, _ int) string {
		return fmt.Sprintf("%q", c)
	})
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = "?"
	}
	query := fmt.Sprintf("INSERT INTO %q (%s) VALUES (%s)",
		table, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(query)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, row := range rows {
		args := make([]any, len(columns))
		for i := range columns {
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

// ExportCSV exports a table to a CSV file.
func (s *Store) ExportCSV(table, csvPath string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}

	cols, err := s.TableColumns(table)
	if err != nil {
		return fmt.Errorf("columns for %q: %w", table, err)
	}

	sqlRows, err := s.db.Query(fmt.Sprintf("SELECT * FROM %q", table))
	if err != nil {
		return fmt.Errorf("query %q: %w", table, err)
	}
	defer func() { _ = sqlRows.Close() }()

	f, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("create %q: %w", csvPath, err)
	}
	defer func() { _ = f.Close() }()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header.
	header := lo.Map(cols, func(c PragmaColumn, _ int) string {
		return c.Name
	})
	if err := w.Write(header); err != nil {
		return err
	}

	// Write rows.
	for sqlRows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		row := make([]string, len(cols))
		for i, v := range values {
			if v != nil {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return sqlRows.Err()
}

// RenameTable renames a table in the SQLite database.
func (s *Store) RenameTable(oldName, newName string) error {
	if !IsSafeIdentifier(oldName) {
		return fmt.Errorf("invalid table name: %q", oldName)
	}
	if !IsSafeIdentifier(newName) {
		return fmt.Errorf("invalid table name: %q", newName)
	}
	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %q RENAME TO %q", oldName, newName))
	return err
}

// DropTable drops the named table from the SQLite database.
func (s *Store) DropTable(table string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	_, err := s.db.Exec(fmt.Sprintf("DROP TABLE %q", table))
	return err
}

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

// CreateEmptyTable is not supported for SQLite.
func (s *Store) CreateEmptyTable(tableName string) error {
	if !IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}
	_, err := s.db.Exec(fmt.Sprintf(
		"CREATE TABLE %q (id INTEGER PRIMARY KEY, name TEXT, value TEXT)", tableName))
	return err
}

// IsSafeIdentifier allows alphanumerics, underscores, and spaces
// (some backends like DuckDB allow spaces in table/column names).
func IsSafeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '_' && r != ' ' {
			return false
		}
	}
	return true
}
