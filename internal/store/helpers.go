package store

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
	xunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// ValidateReadOnlySQL checks that query is a safe, single-statement SELECT
// (or a read-only WITH/CTE). Returns the trimmed query on success.
func ValidateReadOnlySQL(query string) (string, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", fmt.Errorf("empty query")
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return "", fmt.Errorf("only SELECT queries are allowed")
	}
	if strings.Contains(trimmed, ";") {
		return "", fmt.Errorf("multiple statements are not allowed")
	}
	if strings.HasPrefix(upper, "WITH") {
		if ContainsWriteKeyword(upper) {
			return "", fmt.Errorf("only SELECT queries are allowed")
		}
	}
	return trimmed, nil
}

// ContainsWriteKeyword checks if an uppercased query contains SQL write
// keywords that would allow a writable CTE to slip through the prefix check.
// Matches keywords at word boundaries (space, paren, or start/end of string),
// so "WITH x AS (...)INSERT INTO" is caught even without a space before INSERT.
func ContainsWriteKeyword(upper string) bool {
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE"} {
		idx := 0
		for idx <= len(upper)-len(kw) {
			pos := strings.Index(upper[idx:], kw)
			if pos < 0 {
				break
			}
			abs := idx + pos
			before := abs == 0 || !unicode.IsLetter(rune(upper[abs-1]))
			after := abs+len(kw) >= len(upper) || !unicode.IsLetter(rune(upper[abs+len(kw)]))
			if before && after {
				return true
			}
			idx = abs + len(kw)
		}
	}
	return false
}

// IsSafeIdentifier allows alphanumerics, underscores, and spaces
// (some backends like DuckDB allow spaces in table/column names).
func IsSafeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '_' && r != ' ' {
			return false
		}
	}
	return true
}

// DecodeReader returns a reader that strips a leading byte-order mark if
// present. UTF-8 BOMs are dropped; UTF-16 LE/BE BOMs are decoded to UTF-8.
// Inputs without a BOM pass through unchanged as UTF-8.
//
// Pair with [encoding/csv] or [encoding/json] decoders to tolerate files
// produced by Excel and other tools that emit BOM-prefixed text.
func DecodeReader(r io.Reader) io.Reader {
	return transform.NewReader(r, xunicode.BOMOverride(xunicode.UTF8.NewDecoder()))
}

// isUnsafeColumnRune reports whether r would make a column name unsafe to
// interpolate as a quoted SQLite identifier. Rejects ASCII control chars,
// double quote, backslash, and Unicode format chars (which include U+FEFF
// "ZERO WIDTH NO-BREAK SPACE" — the BOM — plus zero-width joiners and
// directional overrides that aren't legitimate in a column header).
func isUnsafeColumnRune(r rune) bool {
	switch {
	case r == '"', r == '\\':
		return true
	case unicode.IsControl(r):
		return true
	case unicode.Is(unicode.Cf, r):
		return true
	}
	return false
}

// IsSafeColumnName reports whether a string is safe to interpolate as a
// quoted SQLite identifier in our CSV/JSON import path. More permissive
// than [IsSafeIdentifier] — allows spaces, punctuation, and Unicode letters
// that real-world headers contain (e.g. "Date (UTC)", "temp_°C", "% complete").
//
// Rejects: empty strings, invalid UTF-8, ASCII control characters (including
// NUL/tab/newline), backslash, and double quote — these would either break
// our %q-based identifier quoting or produce garbled column names.
func IsSafeColumnName(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if isUnsafeColumnRune(r) {
			return false
		}
	}
	return true
}

// SanitizeImportHeaders returns a clean header row for use as SQLite column
// names. It always returns a slice the same length as raw, where every entry
// satisfies [IsSafeColumnName].
//
// Steps:
//  1. Trim surrounding whitespace.
//  2. Replace unsafe characters (see [IsSafeColumnName]) with underscores.
//  3. Fill empty entries with "column_N" (1-indexed).
//  4. Disambiguate duplicates by appending "_1", "_2", … suffixes.
//
// Original column order is preserved.
func SanitizeImportHeaders(raw []string) []string {
	cleaned := lo.Map(raw, func(h string, i int) string {
		h = replaceUnsafe(strings.TrimSpace(h))
		if h == "" {
			return fmt.Sprintf("column_%d", i+1)
		}
		return h
	})

	seen := make(map[string]int, len(cleaned))
	for i, h := range cleaned {
		if _, taken := seen[h]; !taken {
			seen[h] = 0
			continue
		}
		for {
			seen[h]++
			candidate := fmt.Sprintf("%s_%d", h, seen[h])
			if _, taken := seen[candidate]; !taken {
				cleaned[i] = candidate
				seen[candidate] = 0
				break
			}
		}
	}
	return cleaned
}

// replaceUnsafe substitutes any character rejected by [IsSafeColumnName]
// (other than empty/invalid-UTF8, which the caller handles) with '_'.
func replaceUnsafe(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "_")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isUnsafeColumnRune(r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// importableExts lists file extensions that can be imported.
var importableExts = []string{".csv", ".tsv", ".json", ".jsonl", ".ndjson"}

// ImportableExtensions returns the list of file extensions that ImportFile supports.
func ImportableExtensions() []string {
	out := make([]string, len(importableExts))
	copy(out, importableExts)
	return out
}

// IsImportableExt returns true if ext (including the dot) is importable.
func IsImportableExt(ext string) bool {
	return slices.Contains(importableExts, strings.ToLower(ext))
}

// TableNameFromFile derives a SQL-safe table name from a filename.
// Dashes and spaces become underscores; leading digits get a _ prefix.
func TableNameFromFile(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	var clean strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			clean.WriteRune(r)
		}
	}
	name = clean.String()
	if name == "" {
		name = "imported"
	}
	if unicode.IsDigit(rune(name[0])) {
		name = "_" + name
	}
	return name
}
