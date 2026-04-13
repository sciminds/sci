// Package match provides text matching and search query parsing.
//
// This package contains pure functions with no UI or framework dependencies.
// It is used by the TUI's search bar and table-switcher overlay.
//
// [Fuzzy] performs case-insensitive fuzzy matching using [sahilm/fuzzy],
// returning a score and the matched rune positions for highlighting.
//
// Search query parsing is provided via [ParseClauses] and [ParseQuery],
// which handle the "@column: terms" syntax for column-scoped search,
// "|" for OR groups, and "-" prefix for negation.
package match

import (
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/samber/lo"
)

// Fuzzy scores how well query matches target using case-insensitive fuzzy matching.
//
// Returns (0, nil) if the query doesn't match at all.
// Returns (score, positions) on match, where score > 0 (higher is better)
// and positions lists the rune indices in target that matched each query character.
//
// An empty query matches everything with score 1 and nil positions.
func Fuzzy(query, target string) (int, []int) {
	if query == "" {
		return 1, nil
	}

	matches := fuzzy.Find(strings.ToLower(query), []string{strings.ToLower(target)})
	if len(matches) == 0 {
		return 0, nil
	}

	m := matches[0]
	// sahilm/fuzzy scores can be negative for weak matches; shift so all
	// matches return score > 0 while preserving relative ranking.
	score := m.Score + 100
	if score < 1 {
		score = 1
	}
	return score, m.MatchedIndexes
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
