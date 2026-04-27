package data

// csv_helpers.go — input-cleaning helpers for tabular file imports
// (CSV, TSV, JSON). Centralized here because both [internal/db/data] and
// [internal/tui/dbtui/data] need the same handling for real-world files
// produced by Excel, instruments, and ad-hoc data pipelines.

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
	xunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

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
		// Find the next free "h_N" suffix.
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
