package duck

// lossy.go — identify columns whose duckdb types collapse to TEXT when
// the file is mirrored to SQLite for the `sci view` viewer. Callers
// surface a one-line warning so users aren't surprised that complex
// columns render as stringified payloads.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
)

// LossyColumn names one column whose duckdb type does not round-trip
// cleanly through SQLite (STRUCT, LIST/array, MAP, INTERVAL, UNION).
// The Type field carries the original duckdb data_type string.
type LossyColumn struct {
	Table  string
	Column string
	Type   string
}

// LossyColumns lists every column in path whose duckdb type would be
// stringified by the SQLite mirror used by `sci view`. Returns an
// empty slice when the file contains no rich-typed columns.
func LossyColumns(path string) ([]LossyColumn, error) {
	sql := fmt.Sprintf(
		"ATTACH '%s' AS d (READ_ONLY); "+
			"SELECT table_name AS table, column_name AS column, data_type AS type "+
			"FROM information_schema.columns "+
			"WHERE table_catalog='d' ORDER BY table_name, ordinal_position",
		sqlEscape(path),
	)
	out, err := runJSON(sql)
	if err != nil {
		return nil, fmt.Errorf("scan columns in %s: %w", filepath.Base(path), err)
	}
	var rows []struct {
		Table  string `json:"table"`
		Column string `json:"column"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse column listing: %w", err)
	}
	lossy := lo.Filter(rows, func(r struct {
		Table  string `json:"table"`
		Column string `json:"column"`
		Type   string `json:"type"`
	}, _ int) bool {
		return isLossyType(r.Type)
	})
	return lo.Map(lossy, func(r struct {
		Table  string `json:"table"`
		Column string `json:"column"`
		Type   string `json:"type"`
	}, _ int) LossyColumn {
		return LossyColumn{Table: r.Table, Column: r.Column, Type: r.Type}
	}), nil
}

// isLossyType reports whether a duckdb data_type string names a type
// that doesn't round-trip cleanly through the SQLite mirror. Matched
// case-insensitively. The intent here is "render as a stringified
// payload in SQLite", which catches STRUCT, MAP, INTERVAL, LIST/array,
// and UNION — but leaves clean scalar coercions (DATE, DECIMAL, UUID,
// TIMESTAMP, etc.) alone.
func isLossyType(typ string) bool {
	t := strings.ToUpper(strings.TrimSpace(typ))
	switch {
	case t == "INTERVAL":
		return true
	case strings.HasPrefix(t, "STRUCT"):
		return true
	case strings.HasPrefix(t, "MAP"):
		return true
	case strings.HasPrefix(t, "LIST("):
		return true
	case strings.HasPrefix(t, "UNION"):
		return true
	case strings.HasSuffix(t, "]"):
		// Array/list types: VARCHAR[], INTEGER[5], etc.
		return true
	}
	return false
}
