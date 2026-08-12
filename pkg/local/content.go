package local

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
)

// ContentSource locates the text available for one top-level item. It
// carries locators, not text: a library's worth of paper bodies is
// hundreds of megabytes, and the indexer reads them one at a time.
//
// Both sources are optional, but a row is only returned when at least
// one is present — an item with no text is not a candidate for the
// content index.
type ContentSource struct {
	ItemKey string

	// DoclingNoteID is the itemID of the item's docling extraction note,
	// or 0 when it has none. DoclingVersion is that note's Zotero object
	// version, used as the staleness token.
	DoclingNoteID  int64
	DoclingVersion int64

	// AttachmentKey is the key of an attachment Zotero has fulltext-
	// indexed, which is also the name of its storage directory (where
	// .zotero-ft-cache lives). Empty when no attachment is indexed.
	// ZoteroVersion is that attachment's object version.
	AttachmentKey string
	ZoteroVersion int64
}

// contentSourcesQuery finds, per top-level item, the best locator for
// each of the two text sources.
//
// "Top-level" means an item that is neither a note nor an attachment —
// notes and attachments are where content comes from, not things that
// have content. Both sub-selects pick deterministically with MIN(itemID)
// (oldest wins) so a re-run produces the same plan; items routinely have
// several attachments, and re-extraction can leave more than one note.
//
// The EXISTS on fulltextItemWords is what makes the Zotero fallback
// honest: it selects attachments Zotero actually indexed, which is the
// same condition under which it wrote a .zotero-ft-cache file to disk.
const contentSourcesQuery = `
WITH tops AS (
  SELECT i.itemID, i.key
  FROM items i
  LEFT JOIN deletedItems d ON d.itemID = i.itemID
  LEFT JOIN itemNotes n ON n.itemID = i.itemID
  LEFT JOIN itemAttachments a ON a.itemID = i.itemID
  WHERE i.libraryID = ?
    AND d.itemID IS NULL
    AND n.itemID IS NULL
    AND a.itemID IS NULL
),
docling AS (
  SELECT n.parentItemID AS parentID, MIN(n.itemID) AS noteID
  FROM itemNotes n
  JOIN itemTags itg ON itg.itemID = n.itemID
  JOIN tags t ON t.tagID = itg.tagID AND t.name = 'docling'
  LEFT JOIN deletedItems d ON d.itemID = n.itemID
  WHERE n.parentItemID IS NOT NULL AND d.itemID IS NULL
  GROUP BY n.parentItemID
),
indexed AS (
  SELECT ia.parentItemID AS parentID, MIN(ia.itemID) AS attID
  FROM itemAttachments ia
  LEFT JOIN deletedItems d ON d.itemID = ia.itemID
  WHERE ia.parentItemID IS NOT NULL
    AND d.itemID IS NULL
    AND EXISTS (SELECT 1 FROM fulltextItemWords fiw WHERE fiw.itemID = ia.itemID)
  GROUP BY ia.parentItemID
)
SELECT t.key,
       COALESCE(dc.noteID, 0),
       COALESCE(ni.version, 0),
       COALESCE(ai.key, ''),
       COALESCE(ai.version, 0)
FROM tops t
LEFT JOIN docling dc ON dc.parentID = t.itemID
LEFT JOIN items ni   ON ni.itemID = dc.noteID
LEFT JOIN indexed ix ON ix.parentID = t.itemID
LEFT JOIN items ai   ON ai.itemID = ix.attID
WHERE dc.noteID IS NOT NULL OR ix.attID IS NOT NULL
ORDER BY t.key
`

// ContentSources returns one row per top-level item that has text
// available from either source, for the content index to plan against.
//
// Libraries without Zotero's fulltext tables (a non-Zotero SQLite file)
// still work — those items simply report no Zotero source.
func (d *DB) ContentSources() ([]ContentSource, error) {
	if !d.hasFulltextTables() {
		return d.contentSourcesDoclingOnly()
	}
	rows, err := d.db.Query(contentSourcesQuery, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list content sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ContentSource
	for rows.Next() {
		var c ContentSource
		if err := rows.Scan(&c.ItemKey, &c.DoclingNoteID, &c.DoclingVersion,
			&c.AttachmentKey, &c.ZoteroVersion); err != nil {
			return nil, fmt.Errorf("scan content source: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// contentSourcesDoclingOnly is the same query with the Zotero fallback
// removed, for databases that have no fulltext tables at all.
func (d *DB) contentSourcesDoclingOnly() ([]ContentSource, error) {
	const q = `
WITH tops AS (
  SELECT i.itemID, i.key
  FROM items i
  LEFT JOIN deletedItems d ON d.itemID = i.itemID
  LEFT JOIN itemNotes n ON n.itemID = i.itemID
  LEFT JOIN itemAttachments a ON a.itemID = i.itemID
  WHERE i.libraryID = ?
    AND d.itemID IS NULL
    AND n.itemID IS NULL
    AND a.itemID IS NULL
),
docling AS (
  SELECT n.parentItemID AS parentID, MIN(n.itemID) AS noteID
  FROM itemNotes n
  JOIN itemTags itg ON itg.itemID = n.itemID
  JOIN tags t ON t.tagID = itg.tagID AND t.name = 'docling'
  LEFT JOIN deletedItems d ON d.itemID = n.itemID
  WHERE n.parentItemID IS NOT NULL AND d.itemID IS NULL
  GROUP BY n.parentItemID
)
SELECT t.key, dc.noteID, COALESCE(ni.version, 0)
FROM tops t
JOIN docling dc ON dc.parentID = t.itemID
LEFT JOIN items ni ON ni.itemID = dc.noteID
ORDER BY t.key
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list content sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ContentSource
	for rows.Next() {
		var c ContentSource
		if err := rows.Scan(&c.ItemKey, &c.DoclingNoteID, &c.DoclingVersion); err != nil {
			return nil, fmt.Errorf("scan content source: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ContentSignature returns a cheap fingerprint of everything the content
// index is built from: how many extractions exist, and the newest object
// version among notes and attachments.
//
// It exists so a search can detect a stale index in milliseconds.
// [DB.ContentSources] is the accurate answer but joins across the whole
// library (~0.5s on a real one), which is too slow to pay on every
// query. These are three unjoined aggregates.
//
// The fingerprint can theoretically miss a change that leaves all three
// numbers untouched, so callers must use it to warn, never to decide
// what to index.
func (d *DB) ContentSignature() (string, error) {
	const q = `
SELECT
  (SELECT COUNT(*)
     FROM itemNotes n
     JOIN items ni ON ni.itemID = n.itemID
     JOIN itemTags itg ON itg.itemID = n.itemID
     JOIN tags t ON t.tagID = itg.tagID AND t.name = 'docling'
    WHERE ni.libraryID = ?),
  (SELECT COALESCE(MAX(i.version), 0)
     FROM items i JOIN itemNotes n ON n.itemID = i.itemID
    WHERE i.libraryID = ?),
  (SELECT COALESCE(MAX(i.version), 0)
     FROM items i JOIN itemAttachments a ON a.itemID = i.itemID
    WHERE i.libraryID = ?)`

	var notes, maxNoteVer, maxAttVer int64
	if err := d.db.QueryRow(q, d.libraryID, d.libraryID, d.libraryID).
		Scan(&notes, &maxNoteVer, &maxAttVer); err != nil {
		return "", fmt.Errorf("compute content signature: %w", err)
	}
	return fmt.Sprintf("v1:%d:%d:%d", notes, maxNoteVer, maxAttVer), nil
}

// itemIDBatch caps how many keys go into one IN clause. SQLite's
// parameter limit is far higher, but batching keeps the statement small
// enough to plan quickly when a broad content query matches thousands of
// items.
const itemIDBatch = 500

// ItemIDsForKeys resolves item keys to local row ids, skipping keys that
// are unknown or trashed. The content index stores keys (stable, and
// what the API speaks); the search path filters on row ids, so this is
// the bridge between them.
func (d *DB) ItemIDsForKeys(keys []string) ([]int64, error) {
	var out []int64
	for chunk := range slices.Chunk(keys, itemIDBatch) {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		q := `
SELECT i.itemID
FROM items i
LEFT JOIN deletedItems d ON d.itemID = i.itemID
WHERE i.libraryID = ? AND d.itemID IS NULL AND i.key IN (` + placeholders + `)`

		args := slices.Concat([]any{d.libraryID}, lo.ToAnySlice(chunk))
		rows, err := d.db.Query(q, args...)
		if err != nil {
			return nil, fmt.Errorf("resolve item keys: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// NoteBodyByID returns one note's stored body by its local row id. The
// content indexer resolves ids from [DB.ContentSources] and pulls bodies
// one at a time, so this is the per-item read on that path.
func (d *DB) NoteBodyByID(noteID int64) (string, error) {
	var body string
	err := d.db.QueryRow(
		`SELECT COALESCE(note, '') FROM itemNotes WHERE itemID = ?`, noteID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("note %d not found", noteID)
	}
	if err != nil {
		return "", fmt.Errorf("read note body %d: %w", noteID, err)
	}
	return body, nil
}
