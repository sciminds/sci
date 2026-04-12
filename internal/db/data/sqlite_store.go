package data

// sqlite_store.go — [SQLiteStore] implements [DataStore] using pocketbase/dbx
// over modernc.org/sqlite. Handles table listing, row CRUD, and CSV import.

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/samber/lo"
)

// SQLiteStore implements DataStore using the pure-Go modernc.org/sqlite driver.
type SQLiteStore struct {
	db       *dbx.DB
	views    map[string]bool // names that are SQL views (not tables)
	virtuals map[string]bool // names that are virtual tables
	shadows  map[string]bool // names that are shadow tables (FTS5 internal)
}

// OpenSQLiteStore opens a file-backed SQLite database.
func OpenSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := OpenFile(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.NewQuery("SELECT 1").Execute(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

// OpenMemoryStore opens an in-memory SQLite database.
func OpenMemoryStore() (*SQLiteStore, error) {
	db, err := OpenMemory()
	if err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

// Close implements DataStore.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ---------- introspection ----------

// TableNames implements DataStore.
func (s *SQLiteStore) TableNames() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type nameTypeDDL struct {
		Name string  `db:"name"`
		Type string  `db:"type"`
		DDL  *string `db:"sql"`
	}
	var rows []nameTypeDDL
	err := s.db.WithContext(ctx).
		NewQuery(`SELECT name, type, sql FROM sqlite_master
		 WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`).
		All(&rows)
	if err != nil {
		return nil, fmt.Errorf("table names: %w", err)
	}

	s.views = make(map[string]bool, len(rows))
	s.virtuals = make(map[string]bool, len(rows))
	s.shadows = make(map[string]bool, len(rows))
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		switch {
		case r.Type == "view":
			s.views[r.Name] = true
			names = append(names, r.Name)
		case r.DDL != nil && strings.HasPrefix(strings.ToUpper(*r.DDL), "CREATE VIRTUAL TABLE"):
			s.virtuals[r.Name] = true
			names = append(names, r.Name)
		case r.DDL == nil:
			// Shadow tables (e.g. FTS5 _config, _data) have NULL sql.
			s.shadows[r.Name] = true
		default:
			names = append(names, r.Name)
		}
	}
	// Mark shadow tables: any table whose name starts with a known virtual
	// table name + "_" is a shadow table (e.g. fts5: X_config, X_data, …).
	filtered := names[:0]
	for _, name := range names {
		if s.views[name] || s.virtuals[name] {
			filtered = append(filtered, name)
			continue
		}
		isShadow := false
		for vt := range s.virtuals {
			if strings.HasPrefix(name, vt+"_") {
				s.shadows[name] = true
				isShadow = true
				break
			}
		}
		if !isShadow {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

// IsView reports whether name is a SQL view (not a table).
func (s *SQLiteStore) IsView(name string) bool {
	return s.views[name]
}

// IsVirtual reports whether name is a virtual table (e.g. FTS5 shadow tables).
func (s *SQLiteStore) IsVirtual(name string) bool {
	return s.virtuals[name]
}

type pragmaInfoRow struct {
	CID       int     `db:"cid"`
	Name      string  `db:"name"`
	Type      string  `db:"type"`
	NotNull   int     `db:"notnull"`
	DfltValue *string `db:"dflt_value"`
	PK        int     `db:"pk"`
}

// TableColumns implements DataStore.
func (s *SQLiteStore) TableColumns(table string) ([]PragmaColumn, error) {
	if !IsSafeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var prows []pragmaInfoRow
	err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf("PRAGMA table_info('%s')", table)).
		All(&prows)
	if err != nil {
		return nil, fmt.Errorf("table columns %q: %w", table, err)
	}

	cols := lo.Map(prows, func(r pragmaInfoRow, _ int) PragmaColumn {
		return PragmaColumn{
			CID:       r.CID,
			Name:      r.Name,
			Type:      r.Type,
			NotNull:   r.NotNull != 0,
			DfltValue: r.DfltValue,
			PK:        r.PK,
		}
	})
	return cols, nil
}

// TableRowCount implements DataStore.
func (s *SQLiteStore) TableRowCount(table string) (int, error) {
	if !IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table)).
		Row(&count)
	if err != nil {
		return 0, fmt.Errorf("row count %q: %w", table, err)
	}
	return count, nil
}

// TableSummaries implements DataStore.
func (s *SQLiteStore) TableSummaries() ([]TableSummary, error) {
	names, err := s.TableNames()
	if err != nil {
		return nil, err
	}

	summaries := make([]TableSummary, len(names))
	errs := make([]error, len(names))

	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			count, err := s.TableRowCount(name)
			if err != nil {
				errs[i] = err
				return
			}
			cols, err := s.TableColumns(name)
			if err != nil {
				errs[i] = err
				return
			}
			summaries[i] = TableSummary{
				Name:    name,
				Rows:    count,
				Columns: len(cols),
			}
		}(i, name)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return summaries, nil
}

// ---------- dynamic queries ----------

// QueryTable implements DataStore.
func (s *SQLiteStore) QueryTable(table string) ([]string, [][]string, [][]bool, []int64, error) {
	if !IsSafeIdentifier(table) {
		return nil, nil, nil, nil, fmt.Errorf("invalid table name: %q", table)
	}

	// Views and virtual tables don't have rowid — use a synthetic counter instead.
	if s.IsView(table) || s.IsVirtual(table) {
		return s.queryView(table)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sqlRows, err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf(`SELECT rowid, * FROM "%s" LIMIT %d`, table, MaxTableRows)).
		Rows()
	if err != nil {
		// Virtual tables (FTS shadow tables, WITHOUT ROWID tables, etc.)
		// lack a rowid column. Fall back to the view path with synthetic IDs.
		cancel()
		return s.queryView(table)
	}
	defer func() { _ = sqlRows.Close() }()

	allCols, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// allCols[0] == "rowid", rest are data columns.
	colNames := allCols[1:]

	var rows [][]string
	var nullFlags [][]bool
	var rowIDs []int64

	// Allocate scan buffers once and reuse across rows.
	var rowID int64
	dest := make([]any, len(colNames))
	ptrs := make([]any, len(allCols))
	ptrs[0] = &rowID
	for i := range dest {
		ptrs[i+1] = &dest[i]
	}

	for sqlRows.Next() {
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("scan: %w", err)
		}

		row := make([]string, len(colNames))
		nf := make([]bool, len(colNames))
		for i, v := range dest {
			if v == nil {
				nf[i] = true
			} else {
				row[i] = anyToString(v)
			}
			dest[i] = nil // reset for next scan
		}
		rows = append(rows, row)
		nullFlags = append(nullFlags, nf)
		rowIDs = append(rowIDs, rowID)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}

	return colNames, rows, nullFlags, rowIDs, nil
}

// queryView queries a SQL view, which has no rowid. Synthetic row IDs are
// assigned starting at 1.
func (s *SQLiteStore) queryView(view string) ([]string, [][]string, [][]bool, []int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sqlRows, err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf(`SELECT * FROM "%s" LIMIT %d`, view, MaxTableRows)).
		Rows()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("query view %q: %w", view, err)
	}
	defer func() { _ = sqlRows.Close() }()

	colNames, rows, nullFlags, err := scanDynamicRows(sqlRows)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	rowIDs := make([]int64, len(rows))
	for i := range rowIDs {
		rowIDs[i] = int64(i + 1)
	}

	return colNames, rows, nullFlags, rowIDs, nil
}

// ReadOnlyQuery implements DataStore.
func (s *SQLiteStore) ReadOnlyQuery(query string) ([]string, [][]string, error) {
	trimmed, err := ValidateReadOnlySQL(query)
	if err != nil {
		return nil, nil, err
	}

	wrapped := fmt.Sprintf("SELECT * FROM (%s) AS __roq LIMIT %d", trimmed, maxQueryRows)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sqlRows, err := s.db.WithContext(ctx).NewQuery(wrapped).Rows()
	if err != nil {
		return nil, nil, fmt.Errorf("execute query: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	colNames, rows, _, err := scanDynamicRows(sqlRows)
	if err != nil {
		return nil, nil, err
	}
	return colNames, rows, nil
}

// ---------- mutations ----------

// UpdateCell implements DataStore.
func (s *SQLiteStore) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !IsSafeIdentifier(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}

	params := dbx.Params{"value": value}
	var whereClause string
	if len(pkValues) > 0 {
		pkCols := slices.Sorted(maps.Keys(pkValues))
		var parts []string
		for i, col := range pkCols {
			if !IsSafeIdentifier(col) {
				return fmt.Errorf("invalid PK column name: %q", col)
			}
			paramName := fmt.Sprintf("pk%d", i)
			parts = append(parts, fmt.Sprintf(`"%s" = {:%s}`, col, paramName))
			params[paramName] = pkValues[col]
		}
		whereClause = strings.Join(parts, " AND ")
	} else {
		whereClause = fmt.Sprintf("rowid = %d", rowID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	updateSQL := fmt.Sprintf(`UPDATE "%s" SET "%s" = {:value} WHERE %s`, table, column, whereClause)
	if _, err := s.db.WithContext(ctx).NewQuery(updateSQL).Bind(params).Execute(); err != nil {
		return fmt.Errorf("update %q.%q: %w", table, column, err)
	}
	return nil
}

// DeleteRows implements DataStore.
func (s *SQLiteStore) DeleteRows(table string, ids []RowIdentifier) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if !IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}

	useRowID := len(ids[0].PKValues) == 0

	params := dbx.Params{}
	var whereClause string
	if useRowID {
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			paramName := fmt.Sprintf("id%d", i)
			placeholders[i] = fmt.Sprintf("{:%s}", paramName)
			params[paramName] = id.RowID
		}
		whereClause = fmt.Sprintf("rowid IN (%s)", strings.Join(placeholders, ", "))
	} else {
		var conditions []string
		paramIdx := 0
		for _, id := range ids {
			pkCols := slices.Sorted(maps.Keys(id.PKValues))
			var parts []string
			for _, col := range pkCols {
				if !IsSafeIdentifier(col) {
					return 0, fmt.Errorf("invalid PK column name: %q", col)
				}
				paramName := fmt.Sprintf("pk%d", paramIdx)
				paramIdx++
				parts = append(parts, fmt.Sprintf(`"%s" = {:%s}`, col, paramName))
				params[paramName] = id.PKValues[col]
			}
			conditions = append(conditions, "("+strings.Join(parts, " AND ")+")")
		}
		whereClause = strings.Join(conditions, " OR ")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deleteSQL := fmt.Sprintf(`DELETE FROM "%s" WHERE %s`, table, whereClause)
	result, err := s.db.WithContext(ctx).NewQuery(deleteSQL).Bind(params).Execute()
	if err != nil {
		return 0, fmt.Errorf("delete from %q: %w", table, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

// InsertRows implements DataStore.
func (s *SQLiteStore) InsertRows(table string, columns []string, rows [][]string) error {
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
		return fmt.Sprintf(`"%s"`, c)
	})
	colList := strings.Join(quotedCols, ", ")

	return s.insertBatch(table, colList, columns, rows)
}

// ---------- DDL + file I/O ----------

// RenameTable implements DataStore.
func (s *SQLiteStore) RenameTable(oldName, newName string) error {
	if !IsSafeIdentifier(oldName) {
		return fmt.Errorf("invalid table name: %q", oldName)
	}
	if !IsSafeIdentifier(newName) {
		return fmt.Errorf("invalid table name: %q", newName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, oldName, newName)).
		Execute()
	return err
}

// DropTable implements DataStore.
func (s *SQLiteStore) DropTable(table string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf(`DROP TABLE "%s"`, table)).
		Execute()
	return err
}

// ExportCSV implements DataStore.
func (s *SQLiteStore) ExportCSV(table, csvPath string) error {
	if !IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sqlRows, err := s.db.WithContext(ctx).
		NewQuery(fmt.Sprintf(`SELECT * FROM "%s"`, table)).
		Rows()
	if err != nil {
		return fmt.Errorf("export %q: %w", table, err)
	}
	defer func() { _ = sqlRows.Close() }()

	colNames, err := sqlRows.Columns()
	if err != nil {
		return err
	}

	f, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer func() { _ = f.Close() }()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header.
	if err := w.Write(colNames); err != nil {
		return err
	}

	// Write rows.
	dest := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	for sqlRows.Next() {
		if err := sqlRows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		record := make([]string, len(colNames))
		for i, v := range dest {
			if v != nil {
				record[i] = anyToString(v)
			}
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return sqlRows.Err()
}

// ImportCSV implements DataStore.
func (s *SQLiteStore) ImportCSV(csvPath, tableName string) error {
	if !IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}

	f, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)

	// Read header only.
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("csv file is empty or unreadable: %w", err)
	}

	// Create table with TEXT columns (SQLite is dynamically typed anyway).
	quotedCols := lo.Map(header, func(col string, _ int) string {
		return fmt.Sprintf(`"%s" TEXT`, col)
	})
	createSQL := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, tableName, strings.Join(quotedCols, ", "))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := s.db.WithContext(ctx).NewQuery(createSQL).Execute(); err != nil {
		return fmt.Errorf("create table %q: %w", tableName, err)
	}

	// Stream rows in batches to avoid loading the entire file into memory.
	if err := s.streamInsert(r, tableName, header); err != nil {
		return fmt.Errorf("import csv into %q: %w", tableName, err)
	}

	return nil
}

// streamInsert reads rows from a csv.Reader and inserts them in batched
// transactions. This keeps memory usage O(batchSize) instead of O(file).
func (s *SQLiteStore) streamInsert(r *csv.Reader, table string, columns []string) error {
	const batchSize = 500

	quotedCols := lo.Map(columns, func(c string, _ int) string {
		return fmt.Sprintf(`"%s"`, c)
	})
	colList := strings.Join(quotedCols, ", ")

	batch := make([][]string, 0, batchSize)
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read row: %w", err)
		}
		batch = append(batch, record)
		if len(batch) >= batchSize {
			if err := s.insertBatch(table, colList, columns, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	// Flush remaining rows.
	if len(batch) > 0 {
		return s.insertBatch(table, colList, columns, batch)
	}
	return nil
}

// insertBatch inserts a batch of rows in a single transaction using
// multi-row INSERT VALUES for efficiency.
func (s *SQLiteStore) insertBatch(table, colList string, columns []string, rows [][]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.db.WithContext(ctx).Transactional(func(tx *dbx.Tx) error {
		// Build multi-row INSERT: INSERT INTO t (cols) VALUES (?,?),(?,?),...
		// SQLite supports up to 999 parameters; batch accordingly.
		const maxParams = 999
		rowsPerStmt := maxParams / max(len(columns), 1)
		if rowsPerStmt < 1 {
			rowsPerStmt = 1
		}

		for _, chunk := range lo.Chunk(rows, rowsPerStmt) {
			valueTuples := make([]string, len(chunk))
			params := dbx.Params{}
			for ri, row := range chunk {
				placeholders := make([]string, len(columns))
				for ci, col := range columns {
					key := fmt.Sprintf("%s_%d", col, ri)
					placeholders[ci] = fmt.Sprintf("{:%s}", key)
					if ci < len(row) && row[ci] != "" {
						params[key] = row[ci]
					} else {
						params[key] = nil
					}
				}
				valueTuples[ri] = "(" + strings.Join(placeholders, ", ") + ")"
			}

			insertSQL := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES %s`,
				table, colList, strings.Join(valueTuples, ", "))
			if _, err := tx.NewQuery(insertSQL).Bind(params).Execute(); err != nil {
				return fmt.Errorf("batch insert: %w", err)
			}
		}
		return nil
	})
}

// ImportFile implements DataStore (not supported; use ImportCSV).
func (s *SQLiteStore) ImportFile(filePath, tableName string) error {
	return fmt.Errorf("ImportFile not supported on SQLiteStore — use ImportCSV for CSV files")
}

// CreateEmptyTable implements DataStore.
func (s *SQLiteStore) CreateEmptyTable(tableName string) error {
	if !IsSafeIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q", tableName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	createSQL := fmt.Sprintf(`CREATE TABLE "%s" (id INTEGER PRIMARY KEY, name TEXT, value TEXT)`, tableName)
	_, err := s.db.WithContext(ctx).NewQuery(createSQL).Execute()
	return err
}

// ---------- internal helpers ----------

// scanDynamicRows scans all rows from a *dbx.Rows into string slices.
// dbx.Rows embeds *sql.Rows, so all scan methods are available.
func scanDynamicRows(sqlRows *dbx.Rows) (colNames []string, rows [][]string, nullFlags [][]bool, err error) {
	colNames, err = sqlRows.Columns()
	if err != nil {
		return nil, nil, nil, err
	}

	// Allocate scan buffers once and reuse across rows.
	dest := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	for i := range dest {
		ptrs[i] = &dest[i]
	}

	const prealloc = 256
	rows = make([][]string, 0, prealloc)
	nullFlags = make([][]bool, 0, prealloc)

	for sqlRows.Next() {
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, nil, err
		}

		row := make([]string, len(colNames))
		nf := make([]bool, len(colNames))
		for i, v := range dest {
			if v == nil {
				nf[i] = true
			} else {
				row[i] = anyToString(v)
			}
			dest[i] = nil // reset for next scan
		}
		rows = append(rows, row)
		nullFlags = append(nullFlags, nf)
	}
	return colNames, rows, nullFlags, sqlRows.Err()
}

// anyToString converts a driver.Value to its string representation.
func anyToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%v", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}
