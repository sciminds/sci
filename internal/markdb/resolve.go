package markdb

import (
	"database/sql"
	"path"
	"strings"
)

// ResolveLinks extracts links from all files in the database and resolves
// wikilinks/relative links to target file IDs. Returns counts of resolved
// and broken (unresolvable) links.
func (s *Store) ResolveLinks() (resolved, broken int, err error) {
	// Clear existing links.
	if _, err := s.db.Exec("DELETE FROM links"); err != nil {
		return 0, 0, err
	}

	// Get all files with their bodies.
	rows, err := s.db.Query("SELECT id, path, body FROM files")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()

	type fileRow struct {
		id   int64
		path string
		body string
	}
	var files []fileRow
	for rows.Next() {
		var f fileRow
		if err := rows.Scan(&f.id, &f.path, &f.body); err != nil {
			return 0, 0, err
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	for _, f := range files {
		links := ExtractLinks(f.body)
		for _, link := range links {
			targetID, err := s.resolveTarget(f.path, link)
			if err != nil {
				return 0, 0, err
			}

			var tID sql.NullInt64
			if targetID > 0 {
				tID = sql.NullInt64{Int64: targetID, Valid: true}
				resolved++
			} else {
				broken++
			}

			var fragment, alias *string
			if link.Fragment != "" {
				fragment = &link.Fragment
			}
			if link.Alias != "" {
				alias = &link.Alias
			}

			_, err = s.db.Exec(
				`INSERT INTO links (source_id, target_id, raw, target_path, fragment, alias, line)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				f.id, tID, link.Raw, link.TargetPath, fragment, alias, link.Line,
			)
			if err != nil {
				return 0, 0, err
			}
		}
	}

	return resolved, broken, nil
}

// resolveTarget attempts to find the file ID for a link target.
// Returns 0 if the target cannot be resolved.
func (s *Store) resolveTarget(sourcePath string, link RawLink) (int64, error) {
	target := link.TargetPath

	// For markdown relative links (contain / or .md extension), resolve relative to source.
	if strings.HasSuffix(target, ".md") || strings.Contains(target, "/") {
		resolved := path.Clean(path.Join(path.Dir(sourcePath), target))
		var id int64
		err := s.db.QueryRow("SELECT id FROM files WHERE path = ?", resolved).Scan(&id)
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return id, err
	}

	// Wikilink: match by filename (case-insensitive), strip .md if present.
	name := strings.TrimSuffix(target, ".md")
	nameLower := strings.ToLower(name)

	// Try exact filename match (basename without extension).
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM files WHERE LOWER(path) = ? OR LOWER(path) LIKE ?`,
		nameLower+".md",
		"%/"+nameLower+".md",
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// LinkStats returns counts for link analysis.
func (s *Store) LinkStats() (total, resolved, broken int, err error) {
	err = s.db.QueryRow("SELECT COUNT(*) FROM links").Scan(&total)
	if err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM links WHERE target_id IS NOT NULL").Scan(&resolved)
	if err != nil {
		return
	}
	broken = total - resolved
	return
}

// Backlinks returns all files that link to the given file path.
func (s *Store) Backlinks(targetPath string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT f.path FROM links l
		JOIN files f ON f.id = l.source_id
		JOIN files t ON t.id = l.target_id
		WHERE t.path = ?`, targetPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// BrokenLinks returns all links that couldn't be resolved to a target file.
func (s *Store) BrokenLinks() ([]struct{ Source, Raw string }, error) {
	rows, err := s.db.Query(`
		SELECT f.path, l.raw FROM links l
		JOIN files f ON f.id = l.source_id
		WHERE l.target_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []struct{ Source, Raw string }
	for rows.Next() {
		var item struct{ Source, Raw string }
		if err := rows.Scan(&item.Source, &item.Raw); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// Orphans returns files with no incoming links.
func (s *Store) Orphans() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT path FROM files
		WHERE id NOT IN (SELECT target_id FROM links WHERE target_id IS NOT NULL)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}
