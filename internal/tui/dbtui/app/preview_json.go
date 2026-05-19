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
// object or array, along with ok=true. For anything else (plain text,
// bare numbers, malformed JSON) it returns (s, false) — callers can use
// the bool to switch the overlay path (plain text vs. fenced-code with
// glamour syntax highlighting).
//
// json.Indent reformats the original bytes, so key order in STRUCT
// values matches the duckdb output rather than alphabetical (which is
// what json.Marshal of a map[string]any would do).
func prettyPrintJSON(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 {
		return s, false
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return s, false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s, false
	}
	return buf.String(), true
}
