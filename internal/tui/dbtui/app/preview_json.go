package app

// preview_json.go — Pretty-printer and type annotation for JSON cell
// values shown in the note preview overlay (Enter on a cell). DuckDB
// rich types — STRUCT, LIST, MAP, INTERVAL — arrive as compact JSON
// from internal/store/duck. The cell stays narrow; the overlay expands
// the JSON and (when the column type is compound) shows the SQL type
// signature above it as small italic context.

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

// compoundTypeHeader returns a one-line markdown header describing the
// column's SQL type if it looks "compound" — STRUCT/LIST/MAP/JSON/
// INTERVAL or anything with parens / brackets / angle-brackets in it.
// For plain types (VARCHAR, INTEGER, TEXT, …) it returns "" so the
// overlay stays uncluttered. The duckdb type string is rendered as
// italic so glamour shows it as a subtle subhead above the JSON.
func compoundTypeHeader(dbType string) string {
	t := strings.TrimSpace(dbType)
	if t == "" {
		return ""
	}
	if !isCompoundType(t) {
		return ""
	}
	return "*" + t + "*\n\n"
}

// isCompoundType reports whether a SQL type string describes a nested
// type whose rendering benefits from explicit annotation.
func isCompoundType(t string) bool {
	if strings.ContainsAny(t, "([<") {
		return true
	}
	switch strings.ToUpper(t) {
	case "STRUCT", "LIST", "MAP", "JSON", "INTERVAL":
		return true
	}
	return false
}
