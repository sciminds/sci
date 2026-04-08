package markdb

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
