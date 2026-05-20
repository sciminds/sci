package duck

// heavy.go — classification of DuckDB column types whose JSON-serialised
// values are too large to render row-by-row in a TUI. Heavy columns
// (FLOAT[768] embeddings, STRUCTs, BLOBs, …) are rewritten in QueryTable
// to a short placeholder produced server-side, so the wire payload and
// per-render width-measurement costs stay bounded. The full value is
// available via Store.FetchCell on demand.

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// isHeavyType reports whether a DuckDB column type's natural rendering is
// expensive. The classifier intentionally over-includes — placeholders
// stay informative for any type we don't recognise as "definitely cheap",
// and the alternative (silently materialising a 4096-dim float vector
// into a Go string per row) is dramatically worse. The set is closed:
//
//   - BLOB / BIT
//   - JSON
//   - any array/list type (suffix `[]` or `[N]`)
//   - STRUCT(…) / MAP(…) / UNION(…)
//
// Scalars (INT*, DOUBLE, VARCHAR, TIMESTAMP, INTERVAL, UUID, …) are not
// heavy.
func isHeavyType(t string) bool {
	upper := strings.ToUpper(strings.TrimSpace(t))
	if upper == "" {
		return false
	}
	switch upper {
	case "BLOB", "BIT", "JSON":
		return true
	}
	// Array / list types end in `]`: `FLOAT[]`, `INTEGER[10]`,
	// `STRUCT(...)[]`, etc.
	if strings.HasSuffix(upper, "]") {
		return true
	}
	return lo.ContainsBy([]string{"STRUCT(", "MAP(", "UNION("}, func(prefix string) bool {
		return strings.HasPrefix(upper, prefix)
	})
}

// heavyPlaceholderExpr returns a SQL expression that evaluates to a short
// human placeholder for a heavy column instead of the full value. The
// expression preserves NULLs as SQL NULL so the existing null-flag path
// in scanRows stays correct.
//
// col must be a name that has already passed store.IsSafeColumnName —
// it is interpolated as a double-quoted identifier. dbType is treated as
// a string literal (sqlQuote-escaped) and never executed as SQL.
//
// Placeholder shapes:
//
//	FLOAT[]               → <FLOAT[N]>           (N = LEN(col))
//	INTEGER[10]           → <INTEGER[N]>
//	MAP(VARCHAR,INTEGER)  → <MAP[N]>             (N = entry count)
//	BLOB                  → <BLOB N bytes>       (N = OCTET_LENGTH(col))
//	JSON                  → <JSON N chars>
//	STRUCT(…) / UNION(…)  → <STRUCT> / <UNION>   (no size — cheap to compute)
//
// An unrecognised "heavy" type falls back to `<TYPE>` without size.
func heavyPlaceholderExpr(col, dbType string) (string, error) {
	if !store.IsSafeColumnName(col) {
		return "", fmt.Errorf("invalid column name: %q", col)
	}
	upper := strings.ToUpper(strings.TrimSpace(dbType))
	quotedCol := `"` + col + `"`
	switch {
	case upper == "BLOB" || upper == "BIT":
		return fmt.Sprintf(
			"CASE WHEN %s IS NULL THEN NULL ELSE '<%s ' || OCTET_LENGTH(%s) || ' bytes>' END",
			quotedCol, upper, quotedCol,
		), nil
	case upper == "JSON":
		return fmt.Sprintf(
			"CASE WHEN %s IS NULL THEN NULL ELSE '<JSON ' || LENGTH(CAST(%s AS VARCHAR)) || ' chars>' END",
			quotedCol, quotedCol,
		), nil
	case strings.HasSuffix(upper, "]"):
		base := arrayBaseLabel(dbType)
		return fmt.Sprintf(
			"CASE WHEN %s IS NULL THEN NULL ELSE %s || '[' || LEN(%s) || ']>' END",
			quotedCol, sqlQuote("<"+base), quotedCol,
		), nil
	case strings.HasPrefix(upper, "MAP("):
		return fmt.Sprintf(
			"CASE WHEN %s IS NULL THEN NULL ELSE '<MAP[' || LEN(%s) || ']>' END",
			quotedCol, quotedCol,
		), nil
	case strings.HasPrefix(upper, "STRUCT("):
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE '<STRUCT>' END", quotedCol), nil
	case strings.HasPrefix(upper, "UNION("):
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE '<UNION>' END", quotedCol), nil
	default:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE %s END",
			quotedCol, sqlQuote("<"+upper+">")), nil
	}
}

// arrayBaseLabel returns the element-type label for an array/list DuckDB
// type. For `FLOAT[]` → `FLOAT`; for `INTEGER[10]` → `INTEGER`; for
// `STRUCT(x INTEGER)[]` → `STRUCT(x INTEGER)`. The trailing `[…]` is
// stripped off the right-most pair of brackets only — nested array types
// (`FLOAT[][]`) still surface their outer dimension in the placeholder.
func arrayBaseLabel(dbType string) string {
	t := strings.TrimSpace(dbType)
	if i := strings.LastIndex(t, "["); i >= 0 {
		return strings.TrimSpace(t[:i])
	}
	return t
}
