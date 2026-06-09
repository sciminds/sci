package duck

// verbs.go — one function per `sci db` data-inspection verb. Each verb:
//
//  1. Resolves the file to a [Source] (preamble + FROM expression).
//  2. Builds its SQL.
//  3. Runs it with -json for the typed payload and returns a typed result;
//     human rendering happens lazily in render.go via uikit.RenderTable.
//
// Snapshot verbs only (one-shot CLI commands), not hot-loop operations.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// Cols lists the column names + duckdb-inferred types of file at path.
// table disambiguates sqlite/duckdb tables and xlsx sheets.
//
// For SQLite sources the columns include both the resolved type (used
// to read the column) and the declared type, with a fallback note when
// any non-empty cell prevented honoring the declared type.
func Cols(path, table string) (*ColsResult, error) {
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	_, cols, err := promote(src)
	if err != nil {
		return nil, err
	}
	return &ColsResult{Path: path, Table: table, Columns: cols}, nil
}

const defaultRowLimit = 10

// Head returns the first n rows of a tabular file. n <= 0 means
// [defaultRowLimit].
func Head(path, table string, n int) (*RowsResult, error) {
	if n <= 0 {
		n = defaultRowLimit
	}
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	psrc, _, err := promote(src)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf("%s SELECT * FROM %s LIMIT %d", psrc.Preamble, psrc.Expr, n)
	return runRowsQuery(path, table, sql)
}

// Tail returns the last n rows. duckdb has no implicit ordering on
// flat files, so we use ROW_NUMBER() OVER () to recover insertion order
// and select the trailing n.
func Tail(path, table string, n int) (*RowsResult, error) {
	if n <= 0 {
		n = defaultRowLimit
	}
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	psrc, _, err := promote(src)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf(
		"%s WITH ranked AS (SELECT *, ROW_NUMBER() OVER () AS _rn FROM %s),"+
			" bottom AS (SELECT * FROM ranked ORDER BY _rn DESC LIMIT %d)"+
			" SELECT * EXCLUDE (_rn) FROM bottom ORDER BY _rn",
		psrc.Preamble, psrc.Expr, n)
	return runRowsQuery(path, table, sql)
}

// Glimpse returns the dplyr-style transposed preview: one row per
// column, each carrying name, type, and the first samples values.
func Glimpse(path, table string, samples int) (*GlimpseResult, error) {
	if samples <= 0 {
		samples = 5
	}
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	psrc, typed, err := promote(src)
	if err != nil {
		return nil, err
	}

	// Sample rows from the typed projection. UseNumber preserves duckdb's
	// exact numeric text rather than coercing through float64.
	sampleSQL := fmt.Sprintf("%s SELECT * FROM %s LIMIT %d", psrc.Preamble, psrc.Expr, samples)
	sampleOut, err := runJSON(sampleSQL)
	if err != nil {
		return nil, err
	}
	sampleRows, err := decodeRows(sampleOut)
	if err != nil {
		return nil, fmt.Errorf("parse samples: %w", err)
	}

	rowCount, err := countRows(psrc)
	if err != nil {
		return nil, err
	}

	// Transpose: one GlimpseColumn per column, samples drawn from the rows.
	cols := lo.Map(typed, func(d ColumnInfo, _ int) GlimpseColumn {
		samples := lo.Map(sampleRows, func(r map[string]any, _ int) any {
			return r[d.Name]
		})
		return GlimpseColumn{Name: d.Name, Type: d.Type, Samples: samples}
	})

	return &GlimpseResult{Path: path, Table: table, RowCount: rowCount, Columns: cols}, nil
}

// Shape returns the (rows, cols) shape of a tabular file.
func Shape(path, table string) (*ShapeResult, error) {
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	psrc, _, err := promote(src)
	if err != nil {
		return nil, err
	}
	// Single query: SELECT (count, column_count). We use a CROSS JOIN
	// of two subqueries since DESCRIBE itself isn't trivially countable
	// inside another SELECT in older duckdb shells.
	sql := fmt.Sprintf(
		"%s SELECT (SELECT COUNT(*) FROM %s) AS rows,"+
			" (SELECT COUNT(*) FROM (DESCRIBE SELECT * FROM %s)) AS cols",
		psrc.Preamble, psrc.Expr, psrc.Expr)
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	}
	if err := unmarshalJSON(out, &rows); err != nil {
		return nil, fmt.Errorf("parse shape: %w", err)
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("shape: expected 1 row, got %d", len(rows))
	}
	return &ShapeResult{Path: path, Table: table, Rows: rows[0].Rows, Columns: rows[0].Cols}, nil
}

// Summarize returns per-column statistics via duckdb's SUMMARIZE.
func Summarize(path, table string) (*SummarizeResult, error) {
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	psrc, typed, err := promote(src)
	if err != nil {
		return nil, err
	}
	// DuckDB's SUMMARIZE errors with "STDDEV_SAMP is out of range!" if any
	// floating-point column contains NaN/±Infinity, and would otherwise emit
	// those as bare JSON tokens. Map them to NULL up front (a no-op for clean
	// columns) so the surviving values still produce real stats and special
	// floats count as nulls rather than failing the whole summary.
	sql := fmt.Sprintf("%s SUMMARIZE SELECT %s FROM %s",
		psrc.Preamble, summarizeProjection(typed), psrc.Expr)

	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ColumnName     string `json:"column_name"`
		ColumnType     string `json:"column_type"`
		Min            string `json:"min"`
		Max            string `json:"max"`
		ApproxUnique   int    `json:"approx_unique"`
		Avg            string `json:"avg"`
		Std            string `json:"std"`
		Q25            string `json:"q25"`
		Q50            string `json:"q50"`
		Q75            string `json:"q75"`
		Count          int    `json:"count"`
		NullPercentage string `json:"null_percentage"`
	}
	if err := unmarshalJSON(out, &rows); err != nil {
		return nil, fmt.Errorf("parse summarize: %w", err)
	}
	cols := lo.Map(rows, func(r struct {
		ColumnName     string `json:"column_name"`
		ColumnType     string `json:"column_type"`
		Min            string `json:"min"`
		Max            string `json:"max"`
		ApproxUnique   int    `json:"approx_unique"`
		Avg            string `json:"avg"`
		Std            string `json:"std"`
		Q25            string `json:"q25"`
		Q50            string `json:"q50"`
		Q75            string `json:"q75"`
		Count          int    `json:"count"`
		NullPercentage string `json:"null_percentage"`
	}, _ int) SummarizeColumn {
		return SummarizeColumn{
			Name: r.ColumnName, Type: r.ColumnType,
			Min: r.Min, Max: r.Max, ApproxUnique: r.ApproxUnique,
			Avg: r.Avg, Std: r.Std, Q25: r.Q25, Q50: r.Q50, Q75: r.Q75,
			Count: r.Count, NullPercentage: r.NullPercentage,
		}
	})
	return &SummarizeResult{Path: path, Table: table, Columns: cols}, nil
}

// summarizeProjection builds the SELECT list for [Summarize], wrapping every
// floating-point column in a CASE that nulls NaN/±Infinity (which DuckDB's
// SUMMARIZE cannot aggregate) and passing all other columns through unchanged.
// Falls back to "*" when the column types are unknown.
func summarizeProjection(cols []ColumnInfo) string {
	if len(cols) == 0 {
		return "*"
	}
	items := lo.Map(cols, func(c ColumnInfo, _ int) string {
		ident := quoteIdent(c.Name)
		if isFloatType(c.Type) {
			return fmt.Sprintf("CASE WHEN isnan(%[1]s) OR isinf(%[1]s) THEN NULL ELSE %[1]s END AS %[1]s", ident)
		}
		return ident
	})
	return strings.Join(items, ", ")
}

// isFloatType reports whether a duckdb column type can hold NaN/±Infinity — only
// the binary floating-point types can (DECIMAL, HUGEINT, integers cannot).
func isFloatType(duckType string) bool {
	switch strings.ToUpper(strings.TrimSpace(duckType)) {
	case "FLOAT", "DOUBLE", "REAL", "FLOAT4", "FLOAT8":
		return true
	default:
		return false
	}
}

// Query runs a user-supplied read-only SELECT against the file.
//
// Two source models, dispatched on file type:
//
//   - Database files (sqlite/duckdb) have named tables the user already
//     knows, so we ATTACH the file, USE its schema, and run the query
//     verbatim against real table names (e.g. `SELECT title FROM documents`).
//     This is the only model that works for multi-table databases.
//   - Flat / single-table files (csv, parquet, json, single-sheet xlsx)
//     have no inherent table name, so we expose the file as `src` via a CTE
//     (e.g. `SELECT name FROM src`).
//
// ValidateReadOnlySQL rejects multi-statement strings and any write keyword
// (INSERT/UPDATE/DELETE/...); the ATTACH/USE preamble below is ours, not the
// user's, so its semicolons don't trip that check.
func Query(path, sql string) (*RowsResult, error) {
	validated, err := store.ValidateReadOnlySQL(sql)
	if err != nil {
		return nil, err
	}
	if preamble, alias, ok := attachForQuery(path); ok {
		return queryAttached(path, preamble, alias, validated)
	}
	src, err := Resolve(path, "")
	if err != nil {
		return nil, err
	}
	psrc, _, err := promote(src)
	if err != nil {
		return nil, err
	}
	wrapped := fmt.Sprintf("%s WITH src AS (SELECT * FROM %s) %s", psrc.Preamble, psrc.Expr, validated)
	return runRowsQuery(path, "", wrapped)
}

// queryAttached runs validated SQL against an ATTACHed database file. It
// tries native typing first (clean databases get proper column types), and
// for SQLite falls back to loading every column as VARCHAR if a value
// violates its declared type — SQLite's dynamic typing allows e.g. a "" or
// "abc" in an INTEGER column, which duckdb's sqlite_scanner otherwise rejects
// with a raw "Mismatch Type Error". The fallback mirrors promote's no-data-
// loss contract: the query succeeds and the offending cell survives as text.
func queryAttached(path, preamble, alias, validated string) (*RowsResult, error) {
	wrapped := fmt.Sprintf("%s USE %s; %s", preamble, alias, validated)
	res, err := runRowsQuery(path, "", wrapped)
	if err == nil || !isSQLitePreamble(preamble) || !isTypeMismatch(err) {
		return res, err
	}
	retry := fmt.Sprintf("SET sqlite_all_varchar=true; %s USE %s; %s", preamble, alias, validated)
	return runRowsQuery(path, "", retry)
}

// isTypeMismatch reports whether err is duckdb's sqlite_scanner refusing to
// cast a cell to its declared type. duckdb's own message names the remedy
// (sqlite_all_varchar), which is the most stable signal to key off.
func isTypeMismatch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "sqlite_all_varchar")
}

// attachForQuery returns the ATTACH preamble and alias for a database file
// (sqlite/duckdb), whose named tables [Query] exposes directly. ok is false
// for flat/single-table files, which [Query] exposes as `src` instead. The
// preambles match those in resolveSQLite/resolveDuckDB — notably, the SQLite
// attach does not set sqlite_all_varchar, so duckdb's sqlite_scanner reports
// each column under its declared type.
func attachForQuery(path string) (preamble, alias string, ok bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".duckdb":
		return fmt.Sprintf("ATTACH '%s' AS d (READ_ONLY);", sqlEscape(path)), "d", true
	case ".db", ".sqlite", ".sqlite3":
		return fmt.Sprintf("ATTACH '%s' AS s (TYPE SQLITE, READ_ONLY);", sqlEscape(path)), "s", true
	default:
		return "", "", false
	}
}

// runRowsQuery is the shared executor for verbs that return row-shaped
// data: it runs sql once with -json and assembles a [RowsResult],
// preserving duckdb's projection column order.
func runRowsQuery(path, table, sql string) (*RowsResult, error) {
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	rows, err := decodeRows(out)
	if err != nil {
		return nil, fmt.Errorf("parse rows: %w", err)
	}
	cols, err := columnOrder(out)
	if err != nil {
		return nil, fmt.Errorf("parse columns: %w", err)
	}
	return &RowsResult{Path: path, Table: table, Columns: cols, Rows: rows}, nil
}

// Convert exports source file at in to out. srcTable disambiguates
// sqlite/duckdb tables or xlsx sheets on the input side; destTable
// names the destination table when out is a sqlite or duckdb file
// (defaults to the source basename via TableNameFromFile).
//
// Source row count is reported in the result; we derive it via a
// separate COUNT(*) since duckdb's COPY in -json mode returns only the
// output path, not a row tally.
func Convert(in, srcTable, out, destTable string) (*ConvertResult, error) {
	src, err := Resolve(in, srcTable)
	if err != nil {
		return nil, err
	}
	psrc, _, err := promote(src)
	if err != nil {
		return nil, err
	}
	rowsWritten, err := countRows(psrc)
	if err != nil {
		return nil, err
	}

	outExt := strings.ToLower(filepath.Ext(out))
	switch outExt {
	case ".csv", ".tsv", ".json", ".jsonl", ".ndjson", ".parquet":
		if err := copyToFlatFile(psrc, out, outExt); err != nil {
			return nil, err
		}
	case ".db", ".sqlite", ".sqlite3":
		if err := copyToAttached(psrc, out, destTable, in, srcTable, "s", "(TYPE SQLITE)"); err != nil {
			return nil, err
		}
	case ".duckdb":
		if err := copyToAttached(psrc, out, destTable, in, srcTable, "d", ""); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported output extension %q (supported: csv, tsv, json, jsonl, ndjson, parquet, db, sqlite, sqlite3, duckdb)", outExt)
	}

	human := fmt.Sprintf("wrote %s (%d rows) → %s\n", filepath.Base(in), rowsWritten, out)
	return &ConvertResult{Input: in, Output: out, Rows: rowsWritten, humanText: human}, nil
}

// countRows runs COUNT(*) against a Source.
func countRows(src Source) (int, error) {
	out, err := runJSON(fmt.Sprintf("%s SELECT COUNT(*) AS n FROM %s", src.Preamble, src.Expr))
	if err != nil {
		return 0, err
	}
	var counted []struct {
		N int `json:"n"`
	}
	if err := unmarshalJSON(out, &counted); err != nil {
		return 0, fmt.Errorf("parse count: %w", err)
	}
	if len(counted) == 0 {
		return 0, nil
	}
	return counted[0].N, nil
}

// copyToFlatFile writes the source rows to a file via COPY ... TO ... (FORMAT ...).
func copyToFlatFile(src Source, out, outExt string) error {
	format, err := copyFormat(outExt)
	if err != nil {
		return err
	}
	copySQL := fmt.Sprintf("%s COPY (SELECT * FROM %s) TO '%s' (%s)",
		src.Preamble, src.Expr, sqlEscape(out), format)
	_, err = runJSON(copySQL)
	return err
}

// copyToAttached writes the source rows into a sqlite or duckdb file
// by ATTACHing the destination, creating destTable AS SELECT, and
// detaching. attachOpts is "(TYPE SQLITE)" for sqlite, empty for
// native duckdb files. The destination table name defaults, in order:
// explicit --as, the source table name (so cross-DB copies preserve
// the user's data identity), then the source file basename.
func copyToAttached(src Source, out, destTable, in, srcTable, alias, attachOpts string) error {
	if destTable == "" {
		destTable = srcTable
	}
	if destTable == "" {
		destTable = store.TableNameFromFile(in)
	}
	if !store.IsSafeIdentifier(destTable) {
		return fmt.Errorf("invalid destination table name %q (allowed: alphanumerics, underscore, space; pass --as <name> to override)", destTable)
	}
	attach := fmt.Sprintf("ATTACH '%s' AS %s %s;", sqlEscape(out), alias, attachOpts)
	create := fmt.Sprintf(`CREATE TABLE %s."%s" AS SELECT * FROM %s;`, alias, destTable, src.Expr)
	detach := fmt.Sprintf("DETACH %s;", alias)
	full := src.Preamble + attach + create + detach
	_, err := runJSON(full)
	return err
}

// copyFormat maps an output extension to the duckdb COPY ... TO clause body.
func copyFormat(ext string) (string, error) {
	switch ext {
	case ".csv":
		return "FORMAT CSV, HEADER true", nil
	case ".tsv":
		return "FORMAT CSV, HEADER true, DELIMITER '\t'", nil
	case ".json":
		return "FORMAT JSON, ARRAY true", nil
	case ".jsonl", ".ndjson":
		return "FORMAT JSON", nil
	case ".parquet":
		return "FORMAT PARQUET", nil
	default:
		return "", fmt.Errorf("unsupported flat-file extension %q", ext)
	}
}
