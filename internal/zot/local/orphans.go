package local

// orphans.go — read-only scans for the `zot orphans` hygiene check.
// Each scan is a standalone query; the hygiene orchestrator calls
// whichever subset the user asked for and merges the results into a
// single report.

import (
	"database/sql"
	"fmt"
	"strings"
)

// StandaloneAttachment is an attachment row whose parentItemID is NULL.
// The file fields carry enough context for the renderer to tell the user
// which file is dangling.
type StandaloneAttachment struct {
	Key         string
	ContentType string
	Filename    string
	LinkMode    int
}

// StandaloneNote is a note item whose parentItemID is NULL. Zotero stores
// a short note title separately from the HTML body.
type StandaloneNote struct {
	Key   string
	Title string
}

// ScanEmptyCollections returns collections in the user library that have
// neither items nor child collections. Collections that only hold trashed
// items are also considered empty (trashed items are invisible to the
// user, so the collection is effectively empty).
func (d *DB) ScanEmptyCollections() ([]Collection, error) {
	q := `
SELECT c.key, c.collectionName, COALESCE(p.key, '')
FROM collections c
LEFT JOIN collections p ON c.parentCollectionID = p.collectionID
WHERE c.libraryID = ?
  AND NOT EXISTS (
    SELECT 1 FROM collectionItems ci
    LEFT JOIN deletedItems di ON ci.itemID = di.itemID
    WHERE ci.collectionID = c.collectionID AND di.itemID IS NULL
  )
  AND NOT EXISTS (
    SELECT 1 FROM collections sub
    WHERE sub.parentCollectionID = c.collectionID
  )
ORDER BY c.collectionName
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan empty collections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.Key, &c.Name, &c.ParentKey); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ScanStandaloneAttachments returns attachments whose parentItemID is
// NULL. These may be legitimate (a loose PDF the user imported directly)
// or accidental (parent got removed). The check reports them all and
// lets the user decide.
func (d *DB) ScanStandaloneAttachments() ([]StandaloneAttachment, error) {
	q := `
SELECT ch.key, ia.contentType, ia.path, ia.linkMode
FROM itemAttachments ia
JOIN items ch ON ia.itemID = ch.itemID
LEFT JOIN deletedItems di ON ch.itemID = di.itemID
WHERE ia.parentItemID IS NULL
  AND di.itemID IS NULL
  AND ch.libraryID = ?
ORDER BY ch.dateAdded DESC
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan standalone attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []StandaloneAttachment
	for rows.Next() {
		var a StandaloneAttachment
		var ct, path sql.NullString
		if err := rows.Scan(&a.Key, &ct, &path, &a.LinkMode); err != nil {
			return nil, err
		}
		a.ContentType = ct.String
		a.Filename = strings.TrimPrefix(path.String, "storage:")
		out = append(out, a)
	}
	return out, rows.Err()
}

// ScanStandaloneNotes returns notes whose parentItemID is NULL. Zotero
// allows these intentionally, so the finding severity is Info. The
// renderer uses the note title when present.
func (d *DB) ScanStandaloneNotes() ([]StandaloneNote, error) {
	q := `
SELECT ch.key, COALESCE(n.title, '')
FROM itemNotes n
JOIN items ch ON n.itemID = ch.itemID
LEFT JOIN deletedItems di ON ch.itemID = di.itemID
WHERE n.parentItemID IS NULL
  AND di.itemID IS NULL
  AND ch.libraryID = ?
ORDER BY ch.dateAdded DESC
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan standalone notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []StandaloneNote
	for rows.Next() {
		var n StandaloneNote
		if err := rows.Scan(&n.Key, &n.Title); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ScanUncollectedItems returns content items that have no collection
// membership. Attachment/note rows are excluded (they belong to their
// parent item, not directly to collections).
func (d *DB) ScanUncollectedItems() ([]Item, error) {
	args := listArgs()
	args = append(args, d.libraryID)
	q := baseSelect() + `
WHERE i.libraryID = ? AND di.itemID IS NULL
` + contentItemTypeFilter + `
  AND NOT EXISTS (
    SELECT 1 FROM collectionItems ci WHERE ci.itemID = i.itemID
  )
ORDER BY i.dateAdded DESC
`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("scan uncollected items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	for rows.Next() {
		it, err := scanListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ScanAttachmentFiles returns every attachment row in the user library
// along with its linkMode and resolved filename. Used by the missing-file
// orphan sub-check to decide which on-disk paths to stat.
//
// Trashed attachments are excluded — if the user empties trash, those
// rows disappear anyway, so flagging their missing files adds noise.
func (d *DB) ScanAttachmentFiles() ([]StandaloneAttachment, error) {
	q := `
SELECT ch.key, COALESCE(ia.contentType, ''), COALESCE(ia.path, ''), ia.linkMode
FROM itemAttachments ia
JOIN items ch ON ia.itemID = ch.itemID
LEFT JOIN deletedItems di ON ch.itemID = di.itemID
WHERE di.itemID IS NULL
  AND ch.libraryID = ?
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan attachment files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []StandaloneAttachment
	for rows.Next() {
		var a StandaloneAttachment
		var path string
		if err := rows.Scan(&a.Key, &a.ContentType, &path, &a.LinkMode); err != nil {
			return nil, err
		}
		a.Filename = strings.TrimPrefix(path, "storage:")
		out = append(out, a)
	}
	return out, rows.Err()
}

// ScanUnusedTags returns tags that are not associated with any item.
// Strictly structural — a tag used only by trashed items is NOT flagged
// here (its itemTags row still exists). Callers looking for "effectively
// unused" semantics would need to join against deletedItems.
func (d *DB) ScanUnusedTags() ([]Tag, error) {
	q := `
SELECT t.name
FROM tags t
WHERE NOT EXISTS (
  SELECT 1 FROM itemTags it WHERE it.tagID = t.tagID
)
ORDER BY t.name COLLATE NOCASE
`
	rows, err := d.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("scan unused tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Tag
	for rows.Next() {
		var tg Tag
		if err := rows.Scan(&tg.Name); err != nil {
			return nil, err
		}
		out = append(out, tg)
	}
	return out, rows.Err()
}
