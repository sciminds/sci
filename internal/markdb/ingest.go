package markdb

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IngestStats tracks what happened during an ingest operation.
type IngestStats struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// fileEntry holds parsed data for a single file before DB insertion.
type fileEntry struct {
	relPath        string
	content        []byte
	hash           string
	mtime          float64
	parsed         ParsedFile
	links          []RawLink
	bodyText       string
	frontmatterTxt string
}

// ProgressFunc is called during ingestion to report progress.
// phase is "scan" or "upsert", current/total are counts within that phase.
type ProgressFunc func(phase string, current, total int)

// Ingest walks a directory, parses all .md files, and upserts them into the database.
// Uses a two-pass approach: first discover schema from all frontmatter, then upsert rows.
func (s *Store) Ingest(root string) (*IngestStats, error) {
	return s.IngestWithProgress(root, nil)
}

// IngestWithProgress is like Ingest but calls onProgress to report status.
func (s *Store) IngestWithProgress(root string, onProgress ProgressFunc) (*IngestStats, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	// Upsert source.
	sourceID, err := s.upsertSource(root)
	if err != nil {
		return nil, err
	}

	report := func(phase string, cur, tot int) {
		if onProgress != nil {
			onProgress(phase, cur, tot)
		}
	}

	// Pass 1: walk directory, collect file entries.
	entries, err := s.collectFiles(root, report)
	if err != nil {
		return nil, err
	}

	// Check existing hashes to determine adds/updates/skips.
	existingHashes, err := s.getExistingHashes(sourceID)
	if err != nil {
		return nil, err
	}

	stats := &IngestStats{}
	var toUpsert []fileEntry
	seenPaths := make(map[string]bool)

	for i := range entries {
		e := &entries[i]
		seenPaths[e.relPath] = true

		if oldHash, exists := existingHashes[e.relPath]; exists {
			if oldHash == e.hash {
				stats.Skipped++
				continue
			}
			stats.Updated++
		} else {
			stats.Added++
		}

		if e.parsed.ParseError != "" {
			stats.Errors++
		}
		toUpsert = append(toUpsert, *e)
	}

	// Count removed files.
	for path := range existingHashes {
		if !seenPaths[path] {
			stats.Removed++
		}
	}

	// Discover schema from all files (including unchanged ones, for accurate counts).
	allFrontmatters := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.parsed.Frontmatter != nil {
			allFrontmatters = append(allFrontmatters, e.parsed.Frontmatter)
		}
	}
	if len(allFrontmatters) > 0 {
		cols := DiscoverSchema(allFrontmatters)
		if err := s.AddDynamicColumns(cols); err != nil {
			return nil, fmt.Errorf("add columns: %w", err)
		}
	}

	// Pass 2: upsert changed files.
	for i, e := range toUpsert {
		report("upsert", i+1, len(toUpsert))
		if err := s.upsertFile(sourceID, e); err != nil {
			return nil, fmt.Errorf("upsert %q: %w", e.relPath, err)
		}
	}

	// Remove files no longer on disk.
	if err := s.removeStaleFiles(sourceID, seenPaths); err != nil {
		return nil, err
	}

	return stats, nil
}

func (s *Store) upsertSource(root string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO _sources (root, last_ingest) VALUES (?, ?)
		ON CONFLICT(root) DO UPDATE SET last_ingest = excluded.last_ingest`, root, now)
	if err != nil {
		return 0, fmt.Errorf("upsert source: %w", err)
	}
	var id int64
	err = s.db.QueryRow("SELECT id FROM _sources WHERE root = ?", root).Scan(&id)
	return id, err
}

func (s *Store) collectFiles(root string, report func(string, int, int)) ([]fileEntry, error) {
	var entries []fileEntry

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		parsed := ExtractFrontmatter(content)
		links := ExtractLinks(parsed.Body)
		bodyText := StripMarkdown(parsed.Body)
		fmText := FlattenFrontmatter(parsed.Frontmatter)

		h := sha256.Sum256(content)
		hash := fmt.Sprintf("%x", h)

		entries = append(entries, fileEntry{
			relPath:        rel,
			content:        content,
			hash:           hash,
			mtime:          float64(info.ModTime().Unix()),
			parsed:         parsed,
			links:          links,
			bodyText:       bodyText,
			frontmatterTxt: fmText,
		})
		report("scan", len(entries), 0) // total unknown during scan
		return nil
	})

	return entries, err
}

func (s *Store) getExistingHashes(sourceID int64) (map[string]string, error) {
	rows, err := s.db.Query("SELECT path, hash FROM files WHERE source_id = ?", sourceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	hashes := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		hashes[path] = hash
	}
	return hashes, rows.Err()
}

func (s *Store) upsertFile(sourceID int64, e fileEntry) error {
	// Build dynamic column names and values.
	var dynCols []string
	var dynPlaceholders []string
	var dynValues []any

	if e.parsed.Frontmatter != nil {
		for k, v := range e.parsed.Frontmatter {
			colName := SanitizeColumnName(k)
			dynCols = append(dynCols, quoteIdent(colName))
			dynPlaceholders = append(dynPlaceholders, "?")
			dynValues = append(dynValues, toSQLValue(v))
		}
	}

	// Base columns.
	baseCols := `path, source_id, frontmatter_raw, body, body_text, frontmatter_text, mtime, hash, parse_error`
	basePlaceholders := `?, ?, ?, ?, ?, ?, ?, ?, ?`

	var parseErr *string
	if e.parsed.ParseError != "" {
		parseErr = &e.parsed.ParseError
	}

	var fmRaw *string
	if e.parsed.FrontmatterRaw != "" {
		fmRaw = &e.parsed.FrontmatterRaw
	}

	baseValues := []any{
		e.relPath, sourceID, fmRaw, e.parsed.Body,
		e.bodyText, e.frontmatterTxt, e.mtime, e.hash, parseErr,
	}

	allCols := baseCols
	allPlaceholders := basePlaceholders
	allValues := baseValues

	if len(dynCols) > 0 {
		allCols += ", " + strings.Join(dynCols, ", ")
		allPlaceholders += ", " + strings.Join(dynPlaceholders, ", ")
		allValues = append(allValues, dynValues...)
	}

	// Build UPDATE SET clause for conflict.
	setClauses := []string{
		"source_id = excluded.source_id",
		"frontmatter_raw = excluded.frontmatter_raw",
		"body = excluded.body",
		"body_text = excluded.body_text",
		"frontmatter_text = excluded.frontmatter_text",
		"mtime = excluded.mtime",
		"hash = excluded.hash",
		"parse_error = excluded.parse_error",
	}
	for _, col := range dynCols {
		setClauses = append(setClauses, col+" = excluded."+col)
	}

	query := fmt.Sprintf(
		"INSERT INTO files (%s) VALUES (%s) ON CONFLICT(path) DO UPDATE SET %s",
		allCols, allPlaceholders, strings.Join(setClauses, ", "),
	)

	result, err := s.db.Exec(query, allValues...)
	if err != nil {
		return err
	}

	// Get the file ID for FTS.
	var fileID int64
	if id, err := result.LastInsertId(); err == nil && id > 0 {
		fileID = id
	} else {
		err = s.db.QueryRow("SELECT id FROM files WHERE path = ?", e.relPath).Scan(&fileID)
		if err != nil {
			return fmt.Errorf("get file id: %w", err)
		}
	}

	// Update FTS index: delete-then-insert. The delete is best-effort because
	// the row may not exist in the FTS index yet (first ingest).
	_, _ = s.db.Exec( //nolint:errcheck // expected to fail on first insert
		"INSERT INTO files_fts(files_fts, rowid, path, body_text, frontmatter_text) VALUES ('delete', ?, ?, ?, ?)",
		fileID, e.relPath, e.bodyText, e.frontmatterTxt,
	)
	_, err = s.db.Exec(
		"INSERT INTO files_fts(rowid, path, body_text, frontmatter_text) VALUES (?, ?, ?, ?)",
		fileID, e.relPath, e.bodyText, e.frontmatterTxt,
	)
	return err
}

func (s *Store) removeStaleFiles(sourceID int64, seenPaths map[string]bool) error {
	rows, err := s.db.Query("SELECT id, path FROM files WHERE source_id = ?", sourceID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var toDelete []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return err
		}
		if !seenPaths[path] {
			toDelete = append(toDelete, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(toDelete) == 0 {
		return nil
	}
	// Batch delete in a single transaction for efficiency.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit
	for _, id := range toDelete {
		if _, err := tx.Exec("DELETE FROM files WHERE id = ?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// toSQLValue converts a Go value from YAML parsing to a SQL-compatible value.
func toSQLValue(v any) any {
	switch val := v.(type) {
	case bool:
		if val {
			return 1
		}
		return 0
	case []any, map[string]any:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	default:
		return v
	}
}
