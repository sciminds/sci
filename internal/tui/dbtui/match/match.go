// Package match provides text matching and search query parsing.
//
// This package contains pure functions with no UI or framework dependencies.
// It is used by the TUI's search bar and table-switcher overlay.
//
// [MatchRow] performs case-insensitive substring matching with token-AND
// across row cells — the query is split on whitespace and every token must
// appear in some cell. This mirrors Zotero's "All Fields & Tags" semantics
// and is the right default for grid/table search.
//
// Search query parsing is provided via [ParseClauses] and [ParseQuery],
// which handle the "@column: terms" syntax for column-scoped search,
// "|" for OR groups, and "-" prefix for negation.
package match

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/samber/lo"
)

// MatchRow reports whether the given tokens all appear, as case-insensitive
// substrings, across the row's cells. Every token must hit at least one cell;
// different tokens may land in different cells (Zotero-style "all fields"
// AND semantics).
//
// If scopedCol >= 0, only that column's cell is considered — used for
// "@col: terms" queries.
//
// Returns per-cell rune-index positions for rendering highlights, with
// overlapping and adjacent spans merged and sorted. Returns (nil, false) when
// tokens is empty or any token fails to match — callers decide what an empty
// query means.
//
// Semantics are deliberately substring-only (not sahilm-style fuzzy): a grid
// viewer's mental model is "find rows containing these words", and scattered
// per-cell fuzzy hits across long text are confusing. Overlay UIs that want
// ranked fuzzy lists still use [Fuzzy] directly.
func MatchRow(tokens, cells []string, scopedCol int) (map[int][]int, bool) {
	if len(tokens) == 0 {
		return nil, false
	}
	lowerCells := lo.Map(cells, func(c string, _ int) string { return strings.ToLower(c) })

	hl := map[int][]int{}
	for _, tok := range tokens {
		tok = strings.ToLower(tok)
		if tok == "" {
			continue
		}
		tokRunes := utf8.RuneCountInString(tok)
		found := false
		for i, lc := range lowerCells {
			if scopedCol >= 0 && i != scopedCol {
				continue
			}
			spans := substringRuneSpans(lc, tok, tokRunes)
			if len(spans) > 0 {
				found = true
				hl[i] = append(hl[i], spans...)
			}
		}
		if !found {
			return nil, false
		}
	}
	for i := range hl {
		hl[i] = sortedUnique(hl[i])
	}
	return hl, true
}

// TokenSpansInText returns rune-index positions in text covered by
// occurrences of any token. Case-insensitive; empty tokens skipped.
// Used by overlays to reuse the same tokenizer as MatchRow so a row
// matched by row-search highlights the same spans when opened in an
// overlay preview.
func TokenSpansInText(tokens []string, text string) []int {
	if len(tokens) == 0 || text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	var out []int
	for _, tok := range tokens {
		tok = strings.ToLower(tok)
		if tok == "" {
			continue
		}
		tokRunes := utf8.RuneCountInString(tok)
		out = append(out, substringRuneSpans(lower, tok, tokRunes)...)
	}
	return sortedUnique(out)
}

// substringRuneSpans returns every rune-index position in `cell` covered by
// an occurrence of `tok`. Both inputs must be lowercased by the caller.
// Works in byte space then translates to rune indices so unicode cells keep
// correct highlight offsets.
func substringRuneSpans(cell, tok string, tokRunes int) []int {
	if tok == "" || cell == "" {
		return nil
	}
	var out []int
	start := 0
	for {
		idx := strings.Index(cell[start:], tok)
		if idx < 0 {
			return out
		}
		byteStart := start + idx
		runeStart := utf8.RuneCountInString(cell[:byteStart])
		for k := 0; k < tokRunes; k++ {
			out = append(out, runeStart+k)
		}
		start = byteStart + len(tok)
	}
}

// sortedUnique sorts and dedupes an int slice in place-ish.
func sortedUnique(xs []int) []int {
	if len(xs) < 2 {
		return xs
	}
	slices.Sort(xs)
	return slices.Compact(xs)
}

// Clause represents one column-scoped or global search term.
type Clause struct {
	Column string // column name; empty means search all columns
	Terms  string // the search text (with any leading "-" stripped)
	Negate bool   // true if the original terms started with "-" (exclude matches)
}

// ParseQuery extracts an optional column scope and search terms from a query string.
// This is a convenience wrapper around [ParseClauses] that returns only the first clause.
//
//	"@column_name search terms" → ("column_name", "search terms")
//	"search terms"              → ("", "search terms")
func ParseQuery(query string) (columnName string, terms string) {
	groups := ParseClauses(query)
	if len(groups) == 0 || len(groups[0]) == 0 {
		return "", ""
	}
	return groups[0][0].Column, groups[0][0].Terms
}

// ParseClauses parses a query into OR-groups of AND-clauses.
//
// Groups are separated by " | ". Within each group, clauses are ANDed.
// A "-" prefix on terms negates the clause (exclude matches).
//
// Supported forms:
//
//	"plain text"                       → [[{Terms:"plain text"}]]
//	"@col: terms"                      → [[{Column:"col", Terms:"terms"}]]
//	"@col1: v1 @col2: v2"             → [[{col1,v1}, {col2,v2}]]  (AND)
//	"@col1: v1 | @col2: v2"           → [[{col1,v1}], [{col2,v2}]]  (OR)
//	"@col: -val"                       → [[{Column:"col", Terms:"val", Negate:true}]]
func ParseClauses(query string) [][]Clause {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	// Split on " | " for OR groups.
	orParts := strings.Split(q, " | ")
	return lo.FilterMap(orParts, func(part string, _ int) ([]Clause, bool) {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		group := parseAndGroup(part)
		return group, len(group) > 0
	})
}

// parseAndGroup parses a single OR branch into AND-clauses.
func parseAndGroup(q string) []Clause {
	if q[0] == '@' && len(q) >= 2 {
		return parseColumnClauses(q)
	}
	c := Clause{Terms: q}
	applyNegate(&c)
	return []Clause{c}
}

// parseColumnClauses parses one or more @col: value segments.
func parseColumnClauses(q string) []Clause {
	parts := splitClauses(q)
	clauses := lo.FilterMap(parts, func(part string, _ int) (Clause, bool) {
		part = strings.TrimSpace(part)
		if part == "" {
			return Clause{}, false
		}
		c := parseSingleClause(part)
		if c.Column == "" {
			return Clause{}, false
		}
		applyNegate(&c)
		return c, true
	})
	if len(clauses) == 0 {
		c := Clause{Terms: q}
		applyNegate(&c)
		return []Clause{c}
	}
	return clauses
}

// applyNegate checks for a "-" prefix on Terms and sets Negate accordingly.
func applyNegate(c *Clause) {
	if strings.HasPrefix(c.Terms, "-") && len(c.Terms) > 1 {
		c.Terms = c.Terms[1:]
		c.Negate = true
	}
}

// splitClauses splits a query on clause boundaries: ", @" or " @".
// The "@" is kept on each subsequent part.
func splitClauses(q string) []string {
	var parts []string
	for {
		// Try ", @" first (more specific), then fall back to " @".
		idx := strings.Index(q, ", @")
		skip := 2 // skip ", " — keep the "@"
		if idx < 0 {
			idx = strings.Index(q, " @")
			skip = 1 // skip " " — keep the "@"
		}
		if idx < 0 {
			parts = append(parts, q)
			break
		}
		parts = append(parts, q[:idx])
		q = q[idx+skip:]
	}
	return parts
}

// parseSingleClause parses "@col: terms" or "@col terms".
func parseSingleClause(s string) Clause {
	if s == "" || s[0] != '@' {
		return Clause{Terms: s}
	}
	rest := s[1:]
	if rest == "" {
		return Clause{Terms: s}
	}

	// Try "@col: terms" (colon separator).
	if colonIdx := strings.IndexByte(rest, ':'); colonIdx > 0 {
		col := strings.TrimSpace(rest[:colonIdx])
		terms := strings.TrimSpace(rest[colonIdx+1:])
		if col != "" {
			return Clause{Column: col, Terms: terms}
		}
	}

	// Fall back to "@col terms" (space separator).
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		col := rest
		if col == "" {
			return Clause{Terms: s}
		}
		return Clause{Column: col}
	}
	col := rest[:spaceIdx]
	if col == "" {
		return Clause{Terms: s}
	}
	return Clause{Column: col, Terms: strings.TrimSpace(rest[spaceIdx+1:])}
}
