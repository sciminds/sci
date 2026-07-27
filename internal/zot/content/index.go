package content

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samber/lo"
	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no CGO)
)

// Source names where an item's text came from. It rides on every hit
// because the two sources are not equivalent in quality, and because an
// item with no source at all is the difference between "the phrase isn't
// in this paper" and "this paper has no text to search".
type Source string

const (
	// SourceDocling is a docling extraction note — clean markdown with
	// document structure preserved. Preferred whenever it exists.
	SourceDocling Source = "docling"
	// SourceZotero is Zotero's own .zotero-ft-cache, the plain text it
	// extracts to drive its search. Lower fidelity than docling
	// (pdftotext-grade, no structure) but present for items that were
	// never extracted.
	SourceZotero Source = "zotero"
)

// Doc is the indexable text for one top-level library item. There is at
// most one Doc per item: the fallback between sources happens when a Doc
// is built, not when it is queried, so the search path never branches on
// provenance.
type Doc struct {
	ItemKey string
	Source  Source
	// Version is the Zotero object version of whatever produced Body. It
	// is the staleness token — [Index.Versions] diffs it against the
	// library to decide what needs reindexing.
	Version int64
	Body    string
}

// Hit is one search result, ranked.
type Hit struct {
	ItemKey string  `json:"item_key"`
	Source  Source  `json:"source"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// Query parameterizes a search.
type Query struct {
	// Text is the user's raw query, translated by [MatchExpr].
	Text  string
	Limit int // <= 0 means DefaultLimit
	// SnippetOpen and SnippetClose wrap each matched term inside the
	// returned snippet. Both empty (the default) yields plain text.
	SnippetOpen, SnippetClose string
}

// DefaultLimit caps an unbounded search.
const DefaultLimit = 50

// snippetTokens is how many tokens of context a snippet carries. FTS5
// caps this at 64.
const snippetTokens = 12

// Stats summarizes what is in the index.
type Stats struct {
	Total    int            `json:"total"`
	BySource map[Source]int `json:"by_source"`
	Bytes    int64          `json:"bytes"`
}

// Index is sci's full-text index over item content, stored as a SQLite
// database separate from zotero.sqlite (which is opened read-only and is
// not ours to write). It is a cache: deleting the file costs a rebuild
// and nothing else.
//
// The schema is a plain `docs` table holding the text, plus an FTS5
// external-content table over it. That split matters — an FTS5 table
// that stores its own copy of the text doubled the on-disk size in
// testing, and a plain table is what makes upsert-by-item-key possible
// (FTS5 tables have no primary key of their own).
type Index struct {
	db   *sql.DB
	path string
}

// schema is applied on every Open; every statement is CREATE ... IF NOT
// EXISTS so reopening an existing index is a no-op.
//
// The triggers are not optional decoration: with content='docs' the FTS5
// table holds only postings, so it cannot see writes to docs unless they
// are mirrored explicitly. Without them a reindexed item keeps matching
// its old text forever.
const schema = `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS docs (
  item_key TEXT PRIMARY KEY,
  source   TEXT NOT NULL,
  version  INTEGER NOT NULL,
  body     TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(
  body,
  content='docs',
  content_rowid='rowid',
  tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS docs_ai AFTER INSERT ON docs BEGIN
  INSERT INTO docs_fts(rowid, body) VALUES (new.rowid, new.body);
END;

CREATE TRIGGER IF NOT EXISTS docs_ad AFTER DELETE ON docs BEGIN
  INSERT INTO docs_fts(docs_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
END;

CREATE TRIGGER IF NOT EXISTS docs_au AFTER UPDATE ON docs BEGIN
  INSERT INTO docs_fts(docs_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
  INSERT INTO docs_fts(rowid, body) VALUES (new.rowid, new.body);
END;
`

// Open opens (creating if needed) the index at path, along with any
// missing parent directories.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}
	// WAL + NORMAL keeps bulk indexing from fsyncing per statement; this
	// is a rebuildable cache, so durability past a crash is not worth
	// paying for.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open content index: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize content index schema: %w", err)
	}
	return &Index{db: db, path: path}, nil
}

// Path returns the index's location on disk.
func (ix *Index) Path() string { return ix.path }

// Close releases the underlying database handle.
func (ix *Index) Close() error { return ix.db.Close() }

// Upsert inserts or replaces docs, keyed by item key. Replacing a doc
// purges the old text from the full-text index, so a re-extracted paper
// stops matching what it used to say.
func (ix *Index) Upsert(docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
INSERT INTO docs (item_key, source, version, body) VALUES (?, ?, ?, ?)
ON CONFLICT(item_key) DO UPDATE SET
  source = excluded.source, version = excluded.version, body = excluded.body`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, d := range docs {
		if _, err := stmt.Exec(d.ItemKey, string(d.Source), d.Version, d.Body); err != nil {
			return fmt.Errorf("upsert %s: %w", d.ItemKey, err)
		}
	}
	return tx.Commit()
}

// Delete removes the given item keys from the index. Unknown keys are
// silently ignored.
func (ix *Index) Delete(itemKeys []string) error {
	if len(itemKeys) == 0 {
		return nil
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`DELETE FROM docs WHERE item_key = ?`)
	if err != nil {
		return fmt.Errorf("prepare delete: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, k := range itemKeys {
		if _, err := stmt.Exec(k); err != nil {
			return fmt.Errorf("delete %s: %w", k, err)
		}
	}
	return tx.Commit()
}

// DocState is what the index already knows about one item: enough to
// decide whether it needs reindexing, and nothing heavier.
type DocState struct {
	Source  Source
	Version int64
}

// State returns the indexed source and version of every item, for
// diffing against the library to find what drifted. Cheap — no bodies
// are read, so this is affordable on every query.
//
// Source is part of the state, not just Version: when an item gains a
// docling extraction it should be reindexed even though the new note's
// version may be numerically lower than the Zotero attachment's.
// Comparing versions alone would silently keep serving the worse text.
func (ix *Index) State() (map[string]DocState, error) {
	rows, err := ix.db.Query(`SELECT item_key, source, version FROM docs`)
	if err != nil {
		return nil, fmt.Errorf("read index state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]DocState{}
	for rows.Next() {
		var key, source string
		var version int64
		if err := rows.Scan(&key, &source, &version); err != nil {
			return nil, err
		}
		out[key] = DocState{Source: Source(source), Version: version}
	}
	return out, rows.Err()
}

// MetaSignature is the key under which [Sync] records the library
// fingerprint the index was built from. Comparing it to the library's
// current fingerprint is how a search detects a stale index without
// paying for a full plan.
const MetaSignature = "signature"

// SetMeta stores a key/value pair alongside the index.
func (ix *Index) SetMeta(key, value string) error {
	_, err := ix.db.Exec(`
INSERT INTO meta (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("write index metadata %q: %w", key, err)
	}
	return nil
}

// GetMeta reads a metadata value. A missing key returns "" and no error.
func (ix *Index) GetMeta(key string) (string, error) {
	var value string
	err := ix.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read index metadata %q: %w", key, err)
	}
	return value, nil
}

// ErrNoTerms is returned when a query contains nothing searchable.
var ErrNoTerms = errors.New("query has no searchable terms")

// Search returns hits ranked by relevance, best first.
func (ix *Index) Search(q Query) ([]Hit, error) {
	expr := MatchExpr(q.Text)
	if expr == "" {
		return nil, ErrNoTerms
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	// bm25() returns a negative number that gets *smaller* as relevance
	// rises, so ordering is ascending and the exposed Score is negated
	// to restore the obvious "higher is better".
	const sqlText = `
SELECT d.item_key, d.source, bm25(docs_fts), snippet(docs_fts, 0, ?, ?, '…', ?)
FROM docs_fts
JOIN docs d ON d.rowid = docs_fts.rowid
WHERE docs_fts MATCH ?
ORDER BY bm25(docs_fts)
LIMIT ?`

	rows, err := ix.db.Query(sqlText,
		q.SnippetOpen, q.SnippetClose, snippetTokens, expr, limit)
	if err != nil {
		return nil, fmt.Errorf("search content index: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Hit
	for rows.Next() {
		var h Hit
		var score float64
		var source string
		if err := rows.Scan(&h.ItemKey, &source, &score, &h.Snippet); err != nil {
			return nil, err
		}
		h.Source = Source(source)
		h.Score = -score
		out = append(out, h)
	}
	return out, rows.Err()
}

// Keys returns just the item keys matching a query, unranked and
// unlimited — the shape the metadata search wants when it widens a
// clause with content matches.
func (ix *Index) Keys(query string) ([]string, error) {
	expr := MatchExpr(query)
	if expr == "" {
		return nil, ErrNoTerms
	}
	rows, err := ix.db.Query(`
SELECT d.item_key
FROM docs_fts JOIN docs d ON d.rowid = docs_fts.rowid
WHERE docs_fts MATCH ?`, expr)
	if err != nil {
		return nil, fmt.Errorf("search content index: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Body returns the indexed text for one item, and whether it is present.
// This is what makes the index dual-purpose: having stored the text to
// serve snippets, handing the whole paper to a model costs nothing extra.
func (ix *Index) Body(itemKey string) (string, Source, bool, error) {
	var body, source string
	err := ix.db.QueryRow(
		`SELECT body, source FROM docs WHERE item_key = ?`, itemKey).Scan(&body, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read body for %s: %w", itemKey, err)
	}
	return body, Source(source), true, nil
}

// Stats summarizes index coverage and on-disk size.
func (ix *Index) Stats() (Stats, error) {
	rows, err := ix.db.Query(`SELECT source, COUNT(*) FROM docs GROUP BY source`)
	if err != nil {
		return Stats{}, fmt.Errorf("index stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	st := Stats{BySource: map[Source]int{}}
	for rows.Next() {
		var source string
		var n int
		if err := rows.Scan(&source, &n); err != nil {
			return Stats{}, err
		}
		st.BySource[Source(source)] = n
		st.Total += n
	}
	if err := rows.Err(); err != nil {
		return Stats{}, err
	}
	st.Bytes = ix.diskBytes()
	return st, nil
}

// diskBytes totals the main database and its WAL sidecars. Best-effort:
// a stat failure reports 0 rather than failing the whole Stats call.
func (ix *Index) diskBytes() int64 {
	suffixes := []string{"", "-wal", "-shm"}
	return lo.SumBy(suffixes, func(suffix string) int64 {
		info, err := os.Stat(ix.path + suffix)
		if err != nil {
			return 0
		}
		return info.Size()
	})
}

// Vacuum reclaims space after large deletions and merges the FTS index
// into fewer b-tree segments, which speeds up querying.
func (ix *Index) Vacuum() error {
	if _, err := ix.db.Exec(`INSERT INTO docs_fts(docs_fts) VALUES ('optimize')`); err != nil {
		return fmt.Errorf("optimize fts index: %w", err)
	}
	if _, err := ix.db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum content index: %w", err)
	}
	return nil
}
