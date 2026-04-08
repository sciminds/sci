package markdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// ExportStats tracks what happened during an export operation.
type ExportStats struct {
	Written int    `json:"written"`
	Dir     string `json:"dir"`
}

// Export reconstructs markdown files from the database into the given directory.
// An optional WHERE clause filters which files to export.
func (s *Store) Export(dir string, where string) (*ExportStats, error) {
	query := "SELECT path, frontmatter_raw, body FROM files"
	if where != "" {
		query += " WHERE " + where
	}

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := &ExportStats{Dir: dir}

	for rows.Next() {
		var path, body string
		var fmRaw sql.NullString
		if err := rows.Scan(&path, &fmRaw, &body); err != nil {
			return nil, err
		}

		// Reconstruct file content.
		var content string
		if fmRaw.Valid && fmRaw.String != "" {
			content = "---\n" + fmRaw.String + "---\n" + body
		} else {
			content = body
		}

		// Write file.
		outPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %q: %w", filepath.Dir(outPath), err)
		}
		if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write %q: %w", outPath, err)
		}
		stats.Written++
	}

	return stats, rows.Err()
}
