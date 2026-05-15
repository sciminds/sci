package duck

// verbs.go — one function per `sci db` data-inspection verb. Each verb:
//
//  1. Resolves the file to a [Source] (preamble + FROM expression).
//  2. Builds its SQL.
//  3. Runs it twice — once with -json for the typed payload and once
//     with -box for the human rendering — and returns a typed result.
//
// Two duckdb invocations per call is acceptable because these are
// snapshot verbs (one-shot CLI commands), not hot-loop operations.

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	dbtuidata "github.com/sciminds/cli/internal/tui/dbtui/data"
)

// Cols lists the column names + duckdb-inferred types of file at path.
// table disambiguates sqlite/duckdb tables and xlsx sheets.
func Cols(path, table string) (*ColsResult, error) {
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf("%s SELECT column_name, column_type FROM (DESCRIBE SELECT * FROM %s)", src.Preamble, src.Expr)

	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ColumnName string `json:"column_name"`
		ColumnType string `json:"column_type"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse cols: %w", err)
	}
	box, err := runBox(sql)
	if err != nil {
		return nil, err
	}
	return &ColsResult{
		Path:  path,
		Table: table,
		Columns: lo.Map(rows, func(r struct {
			ColumnName string `json:"column_name"`
			ColumnType string `json:"column_type"`
		}, _ int) ColumnInfo {
			return ColumnInfo{Name: r.ColumnName, Type: r.ColumnType}
		}),
		humanBox: box,
	}, nil
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
	sql := fmt.Sprintf("%s SELECT * FROM %s LIMIT %d", src.Preamble, src.Expr, n)
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
	sql := fmt.Sprintf(
		"%s WITH ranked AS (SELECT *, ROW_NUMBER() OVER () AS _rn FROM %s),"+
			" bottom AS (SELECT * FROM ranked ORDER BY _rn DESC LIMIT %d)"+
			" SELECT * EXCLUDE (_rn) FROM bottom ORDER BY _rn",
		src.Preamble, src.Expr, n)
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

	// 1) Column names + types via DESCRIBE.
	descSQL := fmt.Sprintf("%s SELECT column_name, column_type FROM (DESCRIBE SELECT * FROM %s)", src.Preamble, src.Expr)
	descOut, err := runJSON(descSQL)
	if err != nil {
		return nil, err
	}
	var desc []struct {
		ColumnName string `json:"column_name"`
		ColumnType string `json:"column_type"`
	}
	if err := json.Unmarshal(descOut, &desc); err != nil {
		return nil, fmt.Errorf("parse describe: %w", err)
	}

	// 2) Sample rows.
	sampleSQL := fmt.Sprintf("%s SELECT * FROM %s LIMIT %d", src.Preamble, src.Expr, samples)
	sampleOut, err := runJSON(sampleSQL)
	if err != nil {
		return nil, err
	}
	var sampleRows []map[string]any
	if err := json.Unmarshal(sampleOut, &sampleRows); err != nil {
		return nil, fmt.Errorf("parse samples: %w", err)
	}

	// 3) Transpose.
	cols := lo.Map(desc, func(d struct {
		ColumnName string `json:"column_name"`
		ColumnType string `json:"column_type"`
	}, _ int) GlimpseColumn {
		samples := lo.Map(sampleRows, func(r map[string]any, _ int) any {
			return r[d.ColumnName]
		})
		return GlimpseColumn{Name: d.ColumnName, Type: d.ColumnType, Samples: samples}
	})

	// 4) Render the transposed view as box for Human().
	humanBox, err := renderGlimpseBox(cols)
	if err != nil {
		return nil, err
	}
	return &GlimpseResult{Path: path, Table: table, Columns: cols, humanBox: humanBox}, nil
}

// renderGlimpseBox builds a one-row-per-column duckdb table by UNION
// ALL'ing constant rows, then reads it through duckdb -box for a
// terminal-pretty rendering.
func renderGlimpseBox(cols []GlimpseColumn) (string, error) {
	if len(cols) == 0 {
		return "", nil
	}
	var parts []string
	for _, c := range cols {
		samplesJoined := strings.Join(lo.Map(c.Samples, func(s any, _ int) string {
			return fmt.Sprintf("%v", s)
		}), ", ")
		parts = append(parts, fmt.Sprintf(
			"SELECT '%s' AS column, '%s' AS type, '%s' AS samples",
			sqlEscape(c.Name), sqlEscape(c.Type), sqlEscape(samplesJoined),
		))
	}
	return runBox(strings.Join(parts, " UNION ALL "))
}

// Shape returns the (rows, cols) shape of a tabular file.
func Shape(path, table string) (*ShapeResult, error) {
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	// Single query: SELECT (count, column_count). We use a CROSS JOIN
	// of two subqueries since DESCRIBE itself isn't trivially countable
	// inside another SELECT in older duckdb shells.
	sql := fmt.Sprintf(
		"%s SELECT (SELECT COUNT(*) FROM %s) AS rows,"+
			" (SELECT COUNT(*) FROM (DESCRIBE SELECT * FROM %s)) AS cols",
		src.Preamble, src.Expr, src.Expr)
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse shape: %w", err)
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("shape: expected 1 row, got %d", len(rows))
	}
	box, err := runBox(sql)
	if err != nil {
		return nil, err
	}
	return &ShapeResult{Path: path, Table: table, Rows: rows[0].Rows, Columns: rows[0].Cols, humanBox: box}, nil
}

// Summarize returns per-column statistics via duckdb's SUMMARIZE.
func Summarize(path, table string) (*SummarizeResult, error) {
	src, err := Resolve(path, table)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf("%s SUMMARIZE SELECT * FROM %s", src.Preamble, src.Expr)

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
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse summarize: %w", err)
	}
	box, err := runBox(sql)
	if err != nil {
		return nil, err
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
	return &SummarizeResult{Path: path, Table: table, Columns: cols, humanBox: box}, nil
}

// Query runs a user-supplied read-only SELECT against the file.
//
// The user references the source as `src` — we wrap their query in a
// CTE that exposes the file under that name. ValidateReadOnlySQL rejects
// multi-statement strings and any write keyword (INSERT/UPDATE/DELETE/...).
func Query(path, sql string) (*RowsResult, error) {
	validated, err := dbtuidata.ValidateReadOnlySQL(sql)
	if err != nil {
		return nil, err
	}
	src, err := Resolve(path, "")
	if err != nil {
		return nil, err
	}
	wrapped := fmt.Sprintf("%s WITH src AS (SELECT * FROM %s) %s", src.Preamble, src.Expr, validated)
	return runRowsQuery(path, "", wrapped)
}

// runRowsQuery is the shared executor for verbs that return row-shaped
// data: it runs sql twice (json + box) and assembles a [RowsResult].
func runRowsQuery(path, table, sql string) (*RowsResult, error) {
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse rows: %w", err)
	}
	cols := extractColumnOrder(rows)
	box, err := runBox(sql)
	if err != nil {
		return nil, err
	}
	return &RowsResult{
		Path:     path,
		Table:    table,
		Columns:  cols,
		Rows:     rows,
		humanBox: box,
	}, nil
}

// extractColumnOrder pulls column names off the first row. duckdb's
// JSON mode preserves projection order in the JSON object, but Go's
// json.Unmarshal into map[string]any loses that order, so we just take
// whatever order Go's range gives us. For the typical 1-3 column files
// we deal with, ordering is cosmetic; the box-mode rendering is what
// the user reads. Callers that care about exact projection order should
// use Cols (which goes through DESCRIBE) instead.
func extractColumnOrder(rows []map[string]any) []string {
	if len(rows) == 0 {
		return nil
	}
	return slices.Collect(maps.Keys(rows[0]))
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
	rowsWritten, err := countRows(src)
	if err != nil {
		return nil, err
	}

	outExt := strings.ToLower(filepath.Ext(out))
	switch outExt {
	case ".csv", ".tsv", ".json", ".jsonl", ".ndjson", ".parquet":
		if err := copyToFlatFile(src, out, outExt); err != nil {
			return nil, err
		}
	case ".db", ".sqlite", ".sqlite3":
		if err := copyToAttached(src, out, destTable, in, srcTable, "s", "(TYPE SQLITE)"); err != nil {
			return nil, err
		}
	case ".duckdb":
		if err := copyToAttached(src, out, destTable, in, srcTable, "d", ""); err != nil {
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
	if err := json.Unmarshal(out, &counted); err != nil {
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
		destTable = dbtuidata.TableNameFromFile(in)
	}
	if !dbtuidata.IsSafeIdentifier(destTable) {
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
