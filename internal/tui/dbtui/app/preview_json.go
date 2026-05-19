package app

// preview_json.go — Pretty-printer for JSON cell values shown in the
// note preview overlay (Enter on a cell). DuckDB rich types — STRUCT,
// LIST, MAP, INTERVAL — arrive as compact JSON from internal/store/duck.
// The cell stays narrow; the overlay expands.

import (
	"bytes"
	"encoding/json"
	"strings"
)

// prettyPrintJSON returns an indented form of s if it parses as a JSON
// object or array. Anything else (plain text, bare numbers, malformed
// JSON) is returned unchanged. json.Indent reformats the original bytes,
// so key order in STRUCT values matches the duckdb output rather than
// alphabetical (which is what json.Marshal of a map[string]any would do).
func prettyPrintJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 {
		return s
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s
	}
	return buf.String()
}
