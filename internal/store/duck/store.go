package duck

// store.go — DataStore implementation backed by a duckdb subprocess.
// Every read goes through subproc.query(); every mutation method shorts
// to [store.ErrReadOnly] until Phase 3 introduces edit support.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// Store is a read-only DataStore over a `.duckdb` file. The underlying
// duckdb CLI subprocess is kept alive for the lifetime of the Store so
// interactive workloads (per-keystroke filter queries) don't pay
// per-call process-start latency.
type Store struct {
	proc *subproc
	path string

	mu    sync.RWMutex
	views map[string]bool // populated by TableNames
}

// Open starts a `duckdb -readonly -jsonlines <path>` subprocess and
// returns a Store backed by it. Returns the underlying ErrNotInstalled
// when duckdb is not on PATH. Probes the subprocess with a one-row
// SELECT so missing/corrupt database files surface as an error here
// rather than on the first real call.
func Open(path string) (*Store, error) {
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
// compatible output.
func (s *Store) TableColumns(table string) ([]store.PragmaColumn, error) {
	if !store.IsSafeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	return s.describeColumns(fmt.Sprintf(`SELECT * FROM "%s"`, table))
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
// RowIDs are synthetic 1-based counters; mutations are not supported in
// Phase 1 so callers can treat them as opaque positional indices.
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
	return colNames, rows, nullFlags, rowIDs, nil
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

// ---------- mutations (all return store.ErrReadOnly in Phase 1) ----------

// UpdateCell — Phase 1 returns store.ErrReadOnly.
func (s *Store) UpdateCell(string, string, int64, map[string]string, *string) error {
	return store.ErrReadOnly
}

// DeleteRows — Phase 1 returns store.ErrReadOnly.
func (s *Store) DeleteRows(string, []store.RowIdentifier) (int64, error) {
	return 0, store.ErrReadOnly
}

// InsertRows — Phase 1 returns store.ErrReadOnly.
func (s *Store) InsertRows(string, []string, [][]string) error {
	return store.ErrReadOnly
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
