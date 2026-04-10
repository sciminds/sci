// Package markdb ingests directories of markdown files into SQLite with dynamic
// frontmatter columns, a link/backlink graph, and FTS5 full-text search.
package markdb

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database for markdb operations.
type Store struct {
	db *sql.DB
}

// Open opens or creates a markdb SQLite database at the given path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// InitSchema creates the base tables if they don't exist.
func (s *Store) InitSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS _sources (
			id          INTEGER PRIMARY KEY,
			root        TEXT UNIQUE NOT NULL,
			last_ingest TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			id               INTEGER PRIMARY KEY,
			path             TEXT UNIQUE NOT NULL,
			source_id        INTEGER NOT NULL REFERENCES _sources(id),
			frontmatter_raw  TEXT,
			body             TEXT NOT NULL,
			body_text        TEXT,
			frontmatter_text TEXT,
			mtime            REAL NOT NULL,
			hash             TEXT NOT NULL,
			parse_error      TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS _schema (
			key           TEXT PRIMARY KEY,
			column_name   TEXT NOT NULL,
			inferred_type TEXT NOT NULL,
			file_count    INTEGER NOT NULL,
			sample        TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS links (
			id          INTEGER PRIMARY KEY,
			source_id   INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			target_id   INTEGER REFERENCES files(id) ON DELETE SET NULL,
			raw         TEXT NOT NULL,
			target_path TEXT NOT NULL,
			fragment    TEXT,
			alias       TEXT,
			line        INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_links_source ON links(source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_id)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS files_fts USING fts5(
			path, body_text, frontmatter_text,
			content=files, content_rowid=id
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

// existingColumns returns the set of column names on the files table.
func (s *Store) existingColumns() (map[string]bool, error) {
	rows, err := s.db.Query("PRAGMA table_info(files)")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// sqlType maps inferred type names to SQL type keywords.
func sqlType(inferredType string) string {
	switch inferredType {
	case "integer":
		return "INTEGER"
	case "real":
		return "REAL"
	default:
		return "TEXT"
	}
}

// AddDynamicColumns adds frontmatter-derived columns to the files table
// and records them in the _schema table.
func (s *Store) AddDynamicColumns(cols []ColumnDef) error {
	existing, err := s.existingColumns()
	if err != nil {
		return fmt.Errorf("list columns: %w", err)
	}

	for _, c := range cols {
		if existing[c.ColumnName] {
			// Column already exists, just update _schema.
		} else {
			stmt := fmt.Sprintf("ALTER TABLE files ADD COLUMN %s %s",
				quoteIdent(c.ColumnName), sqlType(c.InferredType))
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("add column %q: %w", c.ColumnName, err)
			}
		}

		// Upsert _schema row.
		_, err := s.db.Exec(`INSERT OR REPLACE INTO _schema (key, column_name, inferred_type, file_count, sample)
			VALUES (?, ?, ?, ?, ?)`,
			c.Key, c.ColumnName, c.InferredType, c.FileCount, c.Sample)
		if err != nil {
			return fmt.Errorf("upsert _schema %q: %w", c.Key, err)
		}
	}
	return nil
}

// quoteIdent wraps a SQL identifier in double quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
