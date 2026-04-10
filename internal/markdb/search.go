package markdb

import "strings"

// SearchHit represents a single full-text search result.
type SearchHit struct {
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

// Search performs a full-text search across file paths, body text, and frontmatter.
func (s *Store) Search(query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}

	query = sanitizeFTS(query)
	if query == "" {
		return nil, nil
	}

	rows, err := s.db.Query(`
		SELECT path,
		       snippet(files_fts, 1, '»', '«', '…', 20) as snippet,
		       COALESCE(rank, 0.0) as rank
		FROM files_fts
		WHERE files_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Path, &h.Snippet, &h.Rank); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// sanitizeFTS prepares a user-supplied query for FTS5 MATCH. Balanced
// "phrase queries" are preserved. Everything else is split into tokens and
// individually quoted so FTS5 operators, unbalanced parens, and special
// punctuation never cause parse errors.
func sanitizeFTS(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	var parts []string
	rest := query
	for {
		// Find a balanced "phrase" pair.
		start := strings.IndexByte(rest, '"')
		if start == -1 {
			parts = append(parts, quoteTokens(rest)...)
			break
		}
		end := strings.IndexByte(rest[start+1:], '"')
		if end == -1 {
			// Unbalanced quote — strip it and quote remaining tokens.
			parts = append(parts, quoteTokens(strings.ReplaceAll(rest, `"`, ""))...)
			break
		}
		// Tokens before the phrase.
		parts = append(parts, quoteTokens(rest[:start])...)
		// The phrase itself (already has balanced quotes).
		phrase := rest[start : start+1+end+1]
		parts = append(parts, phrase)
		rest = rest[start+1+end+1:]
	}
	return strings.Join(parts, " ")
}

// quoteTokens splits text on whitespace and wraps each token in double
// quotes, stripping FTS5 syntax characters. Bare FTS5 operators (AND, OR,
// NOT, NEAR) are also quoted so they're treated as literals.
func quoteTokens(s string) []string {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', '*', ':', '^', '"', '\'':
			return -1
		default:
			return r
		}
	}, s)

	var out []string
	for _, tok := range strings.Fields(cleaned) {
		out = append(out, `"`+tok+`"`)
	}
	return out
}
