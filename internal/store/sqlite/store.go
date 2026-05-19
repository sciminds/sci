package sqlite

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"

	_ "modernc.org/sqlite" // registers "sqlite" driver
)

// memDBCounter gives each OpenMemory call a unique shared-cache name so
// multiple connections from the pool see the same in-memory database
// (plain ":memory:" gives each connection its own DB).
var memDBCounter atomic.Uint64

// formatSQLValue stringifies a value returned by a database/sql Scan into
// an `any`. BLOBs (returned as []byte by modernc.org/sqlite) are rendered
// as a compact `<BLOB N bytes>` placeholder rather than the default `%v`
// formatting, which prints []byte as a giant decimal-number spam string
// (e.g. a 16KB vector embedding becomes ~58KB of `[155 57 30 …]` text)
// that breaks table layout and looks like garbage.
func formatSQLValue(v any) string {
	if b, ok := v.([]byte); ok {
		return fmt.Sprintf("<BLOB %d bytes>", len(b))
	}
	return fmt.Sprintf("%v", v)
}

// Store wraps a raw database/sql connection to a SQLite file.
type Store struct {
	db       *sql.DB
	views    map[string]bool // populated by TableNames
	virtuals map[string]bool // populated by TableNames
	shadows  map[string]bool // populated by TableNames (FTS5 internal tables)
}

// Open opens a SQLite database at the given path with WAL mode, foreign
// keys, a 5-second busy timeout, and a 256 MB mmap window. The connection
// pool is sized for concurrent introspection reads (COUNT, PRAGMA …).
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	if err := configure(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// OpenMemory opens an in-memory SQLite database. Useful for tests and
// for the [FileView] flat-file viewer.
func OpenMemory() (*Store, error) {
	id := memDBCounter.Add(1)
	dsn := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", id)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open in-memory sqlite: %w", err)
	}
	if err := configure(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// configure sets pragmas and connection limits for a freshly-opened
// SQLite connection pool.
func configure(db *sql.DB) error {
	// WAL mode supports concurrent readers; allow up to 4 so introspection
	// queries (COUNT, PRAGMA table_info) can run in parallel.
	db.SetMaxOpenConns(4)
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA mmap_size = 268435456", // 256 MB — lets SQLite memory-map large files
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Exec executes a SQL statement that does not return rows.
func (s *Store) Exec(query string) (sql.Result, error) {
	return s.db.Exec(query)
}

// ---------- introspection ----------

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
func (s *Store) TableColumns(table string) ([]store.PragmaColumn, error) {
	if !store.IsSafeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []store.PragmaColumn
	for rows.Next() {
		var c store.PragmaColumn
		if err := rows.Scan(&c.CID, &c.Name, &c.Type, &c.NotNull, &c.DfltValue, &c.PK); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// TableRowCount returns the number of rows in the named table.
func (s *Store) TableRowCount(table string) (int, error) {
	if !store.IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}
	var count int
	err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", table)).Scan(&count)
	return count, err
}

// TableSummaries returns all table names with row counts and column counts.
// Row counts are fetched in a single UNION ALL query to minimize round trips;
// column counts still require one PRAGMA per table (unavoidable in SQLite).
func (s *Store) TableSummaries() ([]store.TableSummary, error) {
	names, err := s.TableNames()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}

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

	summaries := make([]store.TableSummary, 0, len(names))
	for _, name := range names {
		cols, _ := s.TableColumns(name)
		summaries = append(summaries, store.TableSummary{
			Name:    name,
			Rows:    countMap[name],
			Columns: len(cols),
		})
	}
	return summaries, nil
}

// ---------- queries ----------

// QueryTable returns all rows from the named table as string slices.
// Columns are returned in PRAGMA table_info order. Each row's SQLite
// rowid is returned for use in UPDATE/DELETE.
func (s *Store) QueryTable(table string) (colNames []string, rows [][]string, nullFlags [][]bool, rowIDs []int64, err error) {
	if !store.IsSafeIdentifier(table) {
		return nil, nil, nil, nil, fmt.Errorf("invalid table name: %q", table)
	}

	// Views and virtual tables don't have rowid — use a synthetic counter instead.
	if s.IsView(table) || s.IsVirtual(table) {
		return s.queryView(table)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sqlRows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT rowid, * FROM %q LIMIT %d", table, store.MaxTableRows))
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
				row[i] = formatSQLValue(v)
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

	sqlRows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %q LIMIT %d", view, store.MaxTableRows))
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
				row[i] = formatSQLValue(v)
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
	trimmed, err := store.ValidateReadOnlySQL(query)
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
				row[i] = formatSQLValue(v)
			}
		}
		rows = append(rows, row)
	}
	return columns, rows, sqlRows.Err()
}

// ---------- mutations ----------

// UpdateCell updates a single cell value by rowid.
func (s *Store) UpdateCell(table, column string, rowID int64, _ map[string]string, value *string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !store.IsSafeColumnName(column) {
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
func (s *Store) DeleteRows(table string, ids []store.RowIdentifier) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if !store.IsSafeIdentifier(table) {
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
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	for _, col := range columns {
		if !store.IsSafeColumnName(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
	}
	return s.insertRows(table, columns, rows)
}

// ---------- DDL ----------

// RenameTable renames a table in the SQLite database.
func (s *Store) RenameTable(oldName, newName string) error {
	if !store.IsSafeIdentifier(oldName) {
		return fmt.Errorf("invalid table name: %q", oldName)
	}
	if !store.IsSafeIdentifier(newName) {
		return fmt.Errorf("invalid table name: %q", newName)
	}
	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %q RENAME TO %q", oldName, newName))
	return err
}

// DropTable drops the named table from the SQLite database.
func (s *Store) DropTable(table string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	_, err := s.db.Exec(fmt.Sprintf("DROP TABLE %q", table))
	return err
}

// CreateEmptyTable creates a new empty table with a default schema.
func (s *Store) CreateEmptyTable(tableName string) error {
	if !store.IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}
	_, err := s.db.Exec(fmt.Sprintf(
		"CREATE TABLE %q (id INTEGER PRIMARY KEY, name TEXT, value TEXT)", tableName))
	return err
}

// RenameColumn renames a column in a SQLite table.
func (s *Store) RenameColumn(table, oldName, newName string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !store.IsSafeColumnName(oldName) {
		return fmt.Errorf("invalid column name: %q", oldName)
	}
	if !store.IsSafeColumnName(newName) {
		return fmt.Errorf("invalid column name: %q", newName)
	}
	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %q RENAME COLUMN %q TO %q", table, oldName, newName))
	return err
}

// DropColumn drops a column from a SQLite table.
// Returns an error if it would remove the last column.
func (s *Store) DropColumn(table, column string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !store.IsSafeColumnName(column) {
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
	if !store.IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}

	cols, err := s.TableColumns(table)
	if err != nil {
		return 0, err
	}

	colNames := lo.Map(cols, func(c store.PragmaColumn, _ int) string {
		return fmt.Sprintf("%q", c.Name)
	})
	colList := strings.Join(colNames, ", ")

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
	if !store.IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}
	validated, err := store.ValidateReadOnlySQL(query)
	if err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf("CREATE TABLE %q AS %s", tableName, validated))
	return err
}

// CreateViewAs creates a new view from a SELECT query.
func (s *Store) CreateViewAs(viewName, query string) error {
	if !store.IsSafeIdentifier(viewName) {
		return fmt.Errorf("invalid view name: %q", viewName)
	}
	validated, err := store.ValidateReadOnlySQL(query)
	if err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf("CREATE VIEW %q AS %s", viewName, validated))
	return err
}

// ---------- export ----------

// ExportCSV exports a table to a CSV file.
func (s *Store) ExportCSV(table, csvPath string) error {
	if !store.IsSafeIdentifier(table) {
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

	header := lo.Map(cols, func(c store.PragmaColumn, _ int) string {
		return c.Name
	})
	if err := w.Write(header); err != nil {
		return err
	}

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
				row[i] = formatSQLValue(v)
			}
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return sqlRows.Err()
}

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

// ---------- shared helpers ----------

// tableExists reports whether a table (or view) of the given name lives
// in sqlite_master.
func (s *Store) tableExists(name string) (bool, error) {
	var found string
	err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name = ?",
		name,
	).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table existence: %w", err)
	}
	return true, nil
}

// insertRows runs INSERT statements for header+rows inside a single
// transaction. Empty cells become NULL.
func (s *Store) insertRows(tableName string, header []string, rows [][]string) error {
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
