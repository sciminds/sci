package duck

// promote.go — declared-schema probe-and-fall-back for SQLite sources.
//
// SQLite is dynamically typed per-cell: a column declared INTEGER may
// store "" or "abc". duckdb's sqlite_scanner errors on those at scan
// time. Loading everything as VARCHAR (sqlite_all_varchar=true) avoids
// errors but defeats numeric stats and produces all-string parquet on
// convert.
//
// promote() takes the middle path: honor SQLite's declared schema where
// data complies, fall back to VARCHAR per-column when any non-empty
// cell fails to cast (so the original value survives). It runs two
// extra duckdb queries — DESCRIBE (clean attach, declared types) and a
// per-column probe (non-empty count + TRY_CAST success count) — then
// wraps src.Expr in TRY_CAST projections.
//
// For non-SQLite sources this is a thin pass: DESCRIBE for the typed
// column list, src returned unchanged.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/lo"
)

// promote inspects src's columns and returns a typed Source plus per-
// column metadata. For SQLite, the returned Source has a preamble that
// SETs sqlite_all_varchar and an Expr that wraps the original FROM
// clause in TRY_CAST projections.
func promote(src Source) (Source, []ColumnInfo, error) {
	if !isSQLitePreamble(src.Preamble) {
		cols, err := describeSource(src)
		if err != nil {
			return Source{}, nil, err
		}
		return src, cols, nil
	}
	return promoteSQLite(src)
}

// isSQLitePreamble distinguishes a SQLite attach (which needs promotion)
// from csv/parquet/duckdb sources (which don't).
func isSQLitePreamble(preamble string) bool {
	return strings.Contains(preamble, "TYPE SQLITE")
}

// describeSource runs DESCRIBE against src and returns the column list.
// DESCRIBE is a metadata operation — it does not scan rows, so it does
// not trip type-mismatch errors against SQLite's dynamic typing.
func describeSource(src Source) ([]ColumnInfo, error) {
	sql := fmt.Sprintf("%s SELECT column_name, column_type FROM (DESCRIBE SELECT * FROM %s)", src.Preamble, src.Expr)
	out, err := runJSON(sql)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name string `json:"column_name"`
		Type string `json:"column_type"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse describe: %w", err)
	}
	return lo.Map(rows, func(r struct {
		Name string `json:"column_name"`
		Type string `json:"column_type"`
	}, _ int) ColumnInfo {
		return ColumnInfo{Name: r.Name, Type: r.Type}
	}), nil
}

// promoteCandidate is one column that needs the TRY_CAST probe.
type promoteCandidate struct {
	Name     string
	Declared string
}

func promoteSQLite(src Source) (Source, []ColumnInfo, error) {
	declared, err := describeSource(src)
	if err != nil {
		return Source{}, nil, err
	}

	candidates := lo.FilterMap(declared, func(c ColumnInfo, _ int) (promoteCandidate, bool) {
		if !needsPromotion(c.Type) {
			return promoteCandidate{}, false
		}
		return promoteCandidate{Name: c.Name, Declared: c.Type}, true
	})

	failures := map[string]int{}
	if len(candidates) > 0 {
		probeSQL := buildProbeSQL(src, candidates)
		out, err := runJSON(probeSQL)
		if err != nil {
			return Source{}, nil, fmt.Errorf("probe column types: %w", err)
		}
		var rows []map[string]any
		if err := json.Unmarshal(out, &rows); err != nil {
			return Source{}, nil, fmt.Errorf("parse probe: %w", err)
		}
		if len(rows) != 1 {
			return Source{}, nil, fmt.Errorf("probe: expected 1 row, got %d", len(rows))
		}
		for _, c := range candidates {
			ne, _ := asInt(rows[0][probeKeyNonEmpty(c.Name)])
			ok, _ := asInt(rows[0][probeKeyCastOK(c.Name)])
			failures[c.Name] = ne - ok
		}
	}

	typed := make([]ColumnInfo, len(declared))
	projParts := make([]string, len(declared))
	for i, c := range declared {
		resolved := c.Type
		if needsPromotion(c.Type) && failures[c.Name] > 0 {
			resolved = "VARCHAR"
		}
		typed[i] = ColumnInfo{
			Name:         c.Name,
			Type:         resolved,
			Declared:     c.Type,
			FailingCells: failures[c.Name],
		}
		projParts[i] = projectionExpr(c.Name, c.Type, resolved)
	}

	wrappedExpr := fmt.Sprintf("(SELECT %s FROM %s)", strings.Join(projParts, ", "), src.Expr)
	return Source{
		Preamble: "SET sqlite_all_varchar=true; " + src.Preamble,
		Expr:     wrappedExpr,
	}, typed, nil
}

// needsPromotion reports whether a column declared as t should be probed.
// VARCHAR and BLOB pass through verbatim — there's no type to honor.
func needsPromotion(t string) bool {
	up := strings.ToUpper(t)
	return up != "VARCHAR" && up != "BLOB"
}

// projectionExpr returns the SELECT-list expression for one column. For
// resolved=VARCHAR the bare column name is fine (sqlite_all_varchar=true
// already delivers strings). For everything else we TRY_CAST the trimmed
// non-empty value so blanks become NULL instead of erroring.
func projectionExpr(name, declared, resolved string) string {
	if resolved == "VARCHAR" {
		return fmt.Sprintf(`"%s"`, name)
	}
	return fmt.Sprintf(`TRY_CAST(NULLIF(TRIM("%s"), '') AS %s) AS "%s"`, name, declared, name)
}

func probeKeyNonEmpty(col string) string { return "ne_" + col }
func probeKeyCastOK(col string) string   { return "ok_" + col }

// buildProbeSQL constructs a single SELECT that returns per-candidate
// (non_empty, cast_ok) counts. Columns that don't appear here keep their
// declared types (VARCHAR/BLOB don't need probing).
func buildProbeSQL(src Source, candidates []promoteCandidate) string {
	parts := make([]string, 0, 2*len(candidates))
	for _, c := range candidates {
		parts = append(parts,
			fmt.Sprintf(`COUNT(NULLIF(TRIM("%s"), '')) AS "%s"`, c.Name, probeKeyNonEmpty(c.Name)),
			fmt.Sprintf(`COUNT(TRY_CAST(NULLIF(TRIM("%s"), '') AS %s)) AS "%s"`, c.Name, c.Declared, probeKeyCastOK(c.Name)),
		)
	}
	return fmt.Sprintf("SET sqlite_all_varchar=true; %s SELECT %s FROM %s",
		src.Preamble, strings.Join(parts, ", "), src.Expr)
}

// asInt is a defensive numeric coercion for probe COUNT() results — Go
// unmarshals JSON numbers as float64 by default but we want plain ints.
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}
