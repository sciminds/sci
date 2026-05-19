package duck

// store.go — DataStore implementation backed by a duckdb subprocess.
// Every read goes through subproc.query(); row-level mutations resolve
// synthetic row IDs back to PK values via the rowKeys cache before
// emitting UPDATE/DELETE.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// Store is a DataStore over a `.duckdb` file. The underlying duckdb CLI
// subprocess is kept alive for the lifetime of the Store so interactive
// workloads (per-keystroke filter queries) don't pay per-call
// process-start latency.
//
// Phase 3 makes the subprocess read-write. Row-level mutations
// (UpdateCell, DeleteRows) require the target table to have a PRIMARY
// KEY — DuckDB has no implicit rowid. rowKeys caches each visible row's
// PK values so mutations can resolve a synthetic row ID back to a
// `WHERE pk1=? …` clause; the cache for a table is rebuilt on every
// QueryTable.
type Store struct {
	proc *subproc
	path string

	mu          sync.RWMutex
	views       map[string]bool                        // populated by TableNames
	rowEditable map[string]bool                        // populated by TableColumns: table → has-PK
	rowKeys     map[string]map[int64]map[string]string // populated by QueryTable: table → synthID → pkCol → value
}

// Open starts a `duckdb -jsonlines <path>` subprocess and returns a
// Store backed by it. The path must already exist — opening a missing
// file is rejected so typos in `sci view foo.duckdb` don't silently
// produce an empty database. Returns the underlying ErrNotInstalled
// when duckdb is not on PATH. Probes the subprocess with a one-row
// SELECT so corrupt database files surface as an error here rather
// than on the first real call.
func Open(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	p, err := startSubproc(path)
	if err != nil {
		return nil, err
	}
	if _, err := p.query("SELECT 1 AS ok"); err != nil {
		_ = p.close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Store{proc: p, path: path}, nil
}

// Close shuts the subprocess down. Idempotent.
func (s *Store) Close() error {
	return s.proc.close()
}

// ---------- helpers ----------

// scanRows parses a slice of jsonlines rows into the (rows, nullFlags)
// shape used by QueryTable / ReadOnlyQuery. Column order is taken from
// cols. Missing keys render as null. JSON numbers preserve precision via
// json.Number; complex types (STRUCT/LIST/MAP) marshal back to compact
// JSON so the user at least sees something useful.
func scanRows(lines [][]byte, cols []string) ([][]string, [][]bool, error) {
	rows := make([][]string, 0, len(lines))
	nullFlags := make([][]bool, 0, len(lines))
	for i, raw := range lines {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return nil, nil, fmt.Errorf("row %d: %w", i, err)
		}
		row := make([]string, len(cols))
		nf := make([]bool, len(cols))
		for j, c := range cols {
			v, ok := obj[c]
			if !ok || v == nil {
				nf[j] = true
				continue
			}
			row[j] = formatValue(v)
		}
		rows = append(rows, row)
		nullFlags = append(nullFlags, nf)
	}
	return rows, nullFlags, nil
}

// formatValue renders a JSON-decoded value as a display string. Strings
// pass through verbatim; numbers preserve precision via json.Number;
// rich types (objects, arrays) marshal back to compact JSON so the cell
// shows something useful pending Phase 2's preview-pane rendering.
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}

// describeColumns returns the ordered list of (name, type) pairs for an
// arbitrary SELECT query. Uses duckdb's `DESCRIBE (<select>)` which
// returns one row per column with column_name and column_type.
func (s *Store) describeColumns(selectSQL string) ([]store.PragmaColumn, error) {
	lines, err := s.proc.query("DESCRIBE " + selectSQL)
	if err != nil {
		return nil, err
	}
	out := make([]store.PragmaColumn, 0, len(lines))
	for _, l := range lines {
		var row struct {
			Name string `json:"column_name"`
			Type string `json:"column_type"`
			Null string `json:"null"`
			Key  string `json:"key"`
		}
		if err := json.Unmarshal(l, &row); err != nil {
			return nil, fmt.Errorf("decode describe row: %w", err)
		}
		pk := 0
		if strings.EqualFold(row.Key, "PRI") {
			pk = 1
		}
		out = append(out, store.PragmaColumn{
			CID:     len(out),
			Name:    row.Name,
			Type:    row.Type,
			NotNull: strings.EqualFold(row.Null, "NO"),
			PK:      pk,
		})
	}
	return out, nil
}

// ---------- introspection ----------

// TableNames returns base tables and views in the database, sorted
// alphabetically. View flags are cached for the [store.ViewLister] hook.
func (s *Store) TableNames() ([]string, error) {
	lines, err := s.proc.query(
		"SELECT table_name AS name, table_type AS type " +
			"FROM information_schema.tables " +
			"WHERE table_schema='main' ORDER BY name")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(lines))
	views := make(map[string]bool, len(lines))
	for _, l := range lines {
		var row struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(l, &row); err != nil {
			return nil, fmt.Errorf("decode tables row: %w", err)
		}
		if !store.IsSafeIdentifier(row.Name) {
			return nil, fmt.Errorf("unsupported table name %q", row.Name)
		}
		names = append(names, row.Name)
		if strings.EqualFold(row.Type, "VIEW") {
			views[row.Name] = true
		}
	}
	s.mu.Lock()
	s.views = views
	s.mu.Unlock()
	return names, nil
}

// IsView reports whether name was returned as a SQL view by the most
// recent TableNames call.
func (s *Store) IsView(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.views[name]
}

// TableColumns returns column metadata for table. Names and types come
// from a DESCRIBE — null/key flags from duckdb's information_schema-
// compatible output. The row-editability cache is updated as a side
// effect so a subsequent IsRowEditable call is a synchronous lookup.
func (s *Store) TableColumns(table string) ([]store.PragmaColumn, error) {
	if !store.IsSafeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	cols, err := s.describeColumns(fmt.Sprintf(`SELECT * FROM "%s"`, table))
	if err != nil {
		return nil, err
	}
	hasPK := lo.SomeBy(cols, func(c store.PragmaColumn) bool { return c.PK > 0 })
	s.mu.Lock()
	if s.rowEditable == nil {
		s.rowEditable = make(map[string]bool)
	}
	s.rowEditable[table] = hasPK
	s.mu.Unlock()
	return cols, nil
}

// IsRowEditable reports whether the named table supports row-level
// mutations (UpdateCell, DeleteRows). True when the table has at least
// one PRIMARY KEY column. Views and tables that have not been
// introspected yet (via TableColumns) return false — call TableColumns
// first to prime the cache. Implements [store.RowEditabilityChecker].
func (s *Store) IsRowEditable(name string) bool {
	s.mu.RLock()
	if editable, ok := s.rowEditable[name]; ok {
		s.mu.RUnlock()
		return editable
	}
	s.mu.RUnlock()
	// Prime the cache by introspecting once. Failures fall through to
	// "not editable" — refusing edits is the safe default.
	if _, err := s.TableColumns(name); err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rowEditable[name]
}

// TableRowCount returns COUNT(*) for table.
func (s *Store) TableRowCount(table string) (int, error) {
	if !store.IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}
	lines, err := s.proc.query(fmt.Sprintf(`SELECT COUNT(*) AS n FROM "%s"`, table))
	if err != nil {
		return 0, err
	}
	if len(lines) != 1 {
		return 0, fmt.Errorf("expected 1 row from COUNT, got %d", len(lines))
	}
	var row struct {
		N json.Number `json:"n"`
	}
	if err := json.Unmarshal(lines[0], &row); err != nil {
		return 0, fmt.Errorf("decode count row: %w", err)
	}
	n, err := row.N.Int64()
	if err != nil {
		return 0, fmt.Errorf("parse count: %w", err)
	}
	return int(n), nil
}

// TableSummaries returns names+row+column counts for every user table
// and view in a single round-trip per dimension.
func (s *Store) TableSummaries() ([]store.TableSummary, error) {
	names, err := s.TableNames()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}

	colLines, err := s.proc.query(
		"SELECT table_name AS name, COUNT(*) AS n " +
			"FROM information_schema.columns " +
			"WHERE table_schema='main' GROUP BY table_name")
	if err != nil {
		return nil, err
	}
	colsByName := make(map[string]int, len(names))
	for _, l := range colLines {
		var row struct {
			Name string      `json:"name"`
			N    json.Number `json:"n"`
		}
		if err := json.Unmarshal(l, &row); err != nil {
			return nil, fmt.Errorf("decode column counts: %w", err)
		}
		n, _ := row.N.Int64()
		colsByName[row.Name] = int(n)
	}

	// Row counts: UNION ALL one COUNT(*) per table. Table names are
	// IsSafeIdentifier-validated by TableNames so direct interpolation
	// (double-quoted) is safe.
	parts := lo.Map(names, func(name string, _ int) string {
		return fmt.Sprintf(`SELECT '%s' AS name, COUNT(*) AS n FROM "%s"`,
			strings.ReplaceAll(name, "'", "''"), name)
	})
	rowLines, err := s.proc.query(strings.Join(parts, " UNION ALL "))
	if err != nil {
		return nil, err
	}
	rowsByName := make(map[string]int, len(names))
	for _, l := range rowLines {
		var row struct {
			Name string      `json:"name"`
			N    json.Number `json:"n"`
		}
		if err := json.Unmarshal(l, &row); err != nil {
			return nil, fmt.Errorf("decode row counts: %w", err)
		}
		n, _ := row.N.Int64()
		rowsByName[row.Name] = int(n)
	}

	return lo.Map(names, func(name string, _ int) store.TableSummary {
		return store.TableSummary{
			Name:    name,
			Rows:    rowsByName[name],
			Columns: colsByName[name],
		}
	}), nil
}

// QueryTable returns rows from table, capped at [store.MaxTableRows].
// RowIDs are synthetic 1-based counters; mutations resolve them back to
// PK values via the rowKeys cache populated here. The cache for table
// is replaced wholesale on every call so it stays consistent with the
// rows the caller now sees.
func (s *Store) QueryTable(table string) (colNames []string, rows [][]string, nullFlags [][]bool, rowIDs []int64, err error) {
	if !store.IsSafeIdentifier(table) {
		return nil, nil, nil, nil, fmt.Errorf("invalid table name: %q", table)
	}
	cols, err := s.describeColumns(fmt.Sprintf(`SELECT * FROM "%s"`, table))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	colNames = lo.Map(cols, func(c store.PragmaColumn, _ int) string { return c.Name })

	lines, err := s.proc.query(fmt.Sprintf(`SELECT * FROM "%s" LIMIT %d`, table, store.MaxTableRows))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rows, nullFlags, err = scanRows(lines, colNames)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rowIDs = make([]int64, len(rows))
	for i := range rowIDs {
		rowIDs[i] = int64(i + 1)
	}
	s.refreshRowKeys(table, cols, rows, rowIDs)
	return colNames, rows, nullFlags, rowIDs, nil
}

// refreshRowKeys replaces the rowKeys cache for table with the PK
// values from the rows we just fetched. PK-less tables clear any prior
// cache entry. Each row's PK values come from the row strings at the
// indices where cols[i].PK > 0.
func (s *Store) refreshRowKeys(table string, cols []store.PragmaColumn, rows [][]string, rowIDs []int64) {
	pkIdx := []int{}
	for i, c := range cols {
		if c.PK > 0 {
			pkIdx = append(pkIdx, i)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rowKeys == nil {
		s.rowKeys = make(map[string]map[int64]map[string]string)
	}
	if len(pkIdx) == 0 {
		delete(s.rowKeys, table)
		return
	}
	keys := make(map[int64]map[string]string, len(rows))
	for r, row := range rows {
		if r >= len(rowIDs) {
			break
		}
		kv := make(map[string]string, len(pkIdx))
		for _, ci := range pkIdx {
			if ci < len(row) {
				kv[cols[ci].Name] = row[ci]
			}
		}
		keys[rowIDs[r]] = kv
	}
	s.rowKeys[table] = keys
}

// lookupRowKey returns the cached PK values for a synthetic row ID. The
// caller must have called QueryTable on table since opening the store —
// returns ok=false otherwise.
func (s *Store) lookupRowKey(table string, rowID int64) (map[string]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if perTable, ok := s.rowKeys[table]; ok {
		if kv, ok := perTable[rowID]; ok {
			return kv, true
		}
	}
	return nil, false
}

// sqlQuote escapes s for use as a single-quoted SQL literal. Embedded
// quotes are doubled; the duckdb CLI subprocess does not support
// parameterised statements over stdin, so callers building UPDATE /
// DELETE / INSERT SQL must funnel string values through this helper.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ReadOnlyQuery executes a validated SELECT and returns columns + rows.
// Caps at 200 rows like the SQLite backend so the user query overlay
// never has to deal with a multi-megabyte result set.
func (s *Store) ReadOnlyQuery(query string) (columns []string, rows [][]string, err error) {
	trimmed, err := store.ValidateReadOnlySQL(query)
	if err != nil {
		return nil, nil, err
	}
	cols, err := s.describeColumns("(" + trimmed + ")")
	if err != nil {
		return nil, nil, fmt.Errorf("describe query: %w", err)
	}
	columns = lo.Map(cols, func(c store.PragmaColumn, _ int) string { return c.Name })

	const maxRows = 200
	lines, err := s.proc.query(fmt.Sprintf("%s LIMIT %d", trimmed, maxRows))
	if err != nil {
		return nil, nil, fmt.Errorf("execute query: %w", err)
	}
	rows, _, err = scanRows(lines, columns)
	if err != nil {
		return nil, nil, err
	}
	return columns, rows, nil
}

// ---------- mutations ----------

// UpdateCell updates one cell of a single row. The row is addressed by
// the synthetic int64 rowID returned from the most recent QueryTable on
// the same table — UpdateCell resolves it back to a WHERE clause built
// from the row's PRIMARY KEY values via the rowKeys cache. Callers may
// alternatively pass pkValues explicitly; when non-nil it bypasses the
// cache. A nil value writes SQL NULL.
//
// Returns an error if the table has no PRIMARY KEY (DuckDB has no
// implicit rowid), if QueryTable has not been called yet (cache empty),
// or if duckdb rejects the resulting UPDATE.
func (s *Store) UpdateCell(table, column string, rowID int64, pkValues map[string]string, value *string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !store.IsSafeColumnName(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}
	keys, err := s.resolvePKValues(table, rowID, pkValues)
	if err != nil {
		return err
	}
	whereSQL, err := buildPKWhere(keys)
	if err != nil {
		return err
	}
	var setSQL string
	if value == nil {
		setSQL = "NULL"
	} else {
		setSQL = sqlQuote(*value)
	}
	sql := fmt.Sprintf(`UPDATE "%s" SET "%s" = %s WHERE %s`, table, column, setSQL, whereSQL)
	if _, err := s.proc.query(sql); err != nil {
		return fmt.Errorf("update %q.%q: %w", table, column, err)
	}
	return nil
}

// resolvePKValues returns the PK values to use for a row mutation.
// Prefers an explicit pkValues argument; falls back to the cache
// populated by QueryTable. Errors when neither is available — meaning
// the table has no PK, or QueryTable hasn't been called yet for it.
func (s *Store) resolvePKValues(table string, rowID int64, pkValues map[string]string) (map[string]string, error) {
	if len(pkValues) > 0 {
		return pkValues, nil
	}
	if !s.IsRowEditable(table) {
		return nil, fmt.Errorf("table %q has no PRIMARY KEY; row-level edits are not supported", table)
	}
	cached, ok := s.lookupRowKey(table, rowID)
	if !ok {
		return nil, fmt.Errorf("no cached PK values for %q row %d (call QueryTable first)", table, rowID)
	}
	return cached, nil
}

// buildPKWhere builds a `col1 = 'v1' AND col2 = 'v2'` fragment from a
// PK column → value map. Column names are validated; values are quoted.
// Returns an error for unsafe column names or an empty map.
func buildPKWhere(keys map[string]string) (string, error) {
	if len(keys) == 0 {
		return "", errors.New("empty PK values")
	}
	parts := make([]string, 0, len(keys))
	for col, val := range keys {
		if !store.IsSafeColumnName(col) {
			return "", fmt.Errorf("invalid PK column name: %q", col)
		}
		parts = append(parts, fmt.Sprintf(`"%s" = %s`, col, sqlQuote(val)))
	}
	// Deterministic order so error/debug output is stable.
	slices.Sort(parts)
	return strings.Join(parts, " AND "), nil
}

// DeleteRows deletes rows identified by ids. Each RowIdentifier is
// resolved the same way UpdateCell resolves its arguments: explicit
// PKValues win, otherwise RowID is looked up in the per-table cache
// populated by the most recent QueryTable. Returns the number of rows
// actually removed (counted via `DELETE … RETURNING 1`). An empty ids
// slice succeeds with count 0.
func (s *Store) DeleteRows(table string, ids []store.RowIdentifier) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if !store.IsSafeIdentifier(table) {
		return 0, fmt.Errorf("invalid table name: %q", table)
	}
	clauses := make([]string, 0, len(ids))
	for _, id := range ids {
		keys, err := s.resolvePKValues(table, id.RowID, id.PKValues)
		if err != nil {
			return 0, fmt.Errorf("row %d: %w", id.RowID, err)
		}
		clause, err := buildPKWhere(keys)
		if err != nil {
			return 0, err
		}
		clauses = append(clauses, "("+clause+")")
	}
	sql := fmt.Sprintf(`DELETE FROM "%s" WHERE %s RETURNING 1 AS __sci_deleted__`,
		table, strings.Join(clauses, " OR "))
	lines, err := s.proc.query(sql)
	if err != nil {
		return 0, fmt.Errorf("delete from %q: %w", table, err)
	}
	// Invalidate the row-key cache — the synth IDs the caller knew about
	// no longer match the next QueryTable's row ordering.
	s.mu.Lock()
	delete(s.rowKeys, table)
	s.mu.Unlock()
	return int64(len(lines)), nil
}

// InsertRows inserts one or more rows into table. Empty-string cells
// become SQL NULL (matching the SQLite store contract); shorter rows
// are right-padded with NULL so the columns/row-length mismatch is
// permissive. All values are funneled through sqlQuote — duckdb's
// stdin subprocess doesn't accept parameterised statements. Works on
// tables without a PRIMARY KEY since INSERT doesn't address existing
// rows. Invalidates the rowKeys cache for table so subsequent calls
// re-resolve synthetic IDs against the new row set.
func (s *Store) InsertRows(table string, columns []string, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	for _, c := range columns {
		if !store.IsSafeColumnName(c) {
			return fmt.Errorf("invalid column name: %q", c)
		}
	}
	quotedCols := lo.Map(columns, func(c string, _ int) string {
		return fmt.Sprintf(`"%s"`, c)
	})
	tuples := lo.Map(rows, func(row []string, _ int) string {
		vals := make([]string, len(columns))
		for i := range columns {
			switch {
			case i >= len(row):
				vals[i] = "NULL"
			case row[i] == "":
				vals[i] = "NULL"
			default:
				vals[i] = sqlQuote(row[i])
			}
		}
		return "(" + strings.Join(vals, ", ") + ")"
	})
	sql := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES %s`,
		table, strings.Join(quotedCols, ", "), strings.Join(tuples, ", "))
	if _, err := s.proc.query(sql); err != nil {
		return fmt.Errorf("insert into %q: %w", table, err)
	}
	s.mu.Lock()
	delete(s.rowKeys, table)
	s.mu.Unlock()
	return nil
}

// RenameTable — Phase 1 returns store.ErrReadOnly.
func (s *Store) RenameTable(string, string) error { return store.ErrReadOnly }

// DropTable — Phase 1 returns store.ErrReadOnly.
func (s *Store) DropTable(string) error { return store.ErrReadOnly }

// ImportCSV — Phase 1 returns store.ErrReadOnly. Use `sci db add` on
// the .duckdb file from outside dbtui instead.
func (s *Store) ImportCSV(string, string) error { return store.ErrReadOnly }

// AppendCSV — Phase 1 returns store.ErrReadOnly.
func (s *Store) AppendCSV(string, string) error { return store.ErrReadOnly }

// ImportFile — Phase 1 returns store.ErrReadOnly.
func (s *Store) ImportFile(string, string) error { return store.ErrReadOnly }

// CreateEmptyTable — Phase 1 returns store.ErrReadOnly.
func (s *Store) CreateEmptyTable(string) error { return store.ErrReadOnly }

// ExportCSV writes table to csvPath using duckdb's COPY ... TO, since
// export is a read operation against the database. csvPath must be an
// absolute path on the host.
func (s *Store) ExportCSV(table, csvPath string) error {
	if !store.IsSafeIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if strings.ContainsAny(csvPath, "'\n") {
		return errors.New("invalid CSV path")
	}
	_, err := s.proc.query(fmt.Sprintf(`COPY (SELECT * FROM "%s") TO '%s' (FORMAT CSV, HEADER)`,
		table, strings.ReplaceAll(csvPath, "'", "''")))
	return err
}
