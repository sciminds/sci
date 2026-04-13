package local

import (
	"database/sql"
	"fmt"
)

// DoclingNoteSummary is a lightweight projection of a docling-tagged
// child note used by bulk listing. Body is the raw HTML — callers
// handle snippet extraction at the presentation layer.
type DoclingNoteSummary struct {
	NoteKey     string   `json:"note_key"`
	ParentKey   string   `json:"parent_key"`
	ParentTitle string   `json:"parent_title"`
	Body        string   `json:"body"`
	DateAdded   string   `json:"date_added"`
	Tags        []string `json:"tags,omitempty"`
}

// NoteDetail is the full projection of a single note item, used by
// the `zot notes read` command.
type NoteDetail struct {
	Key       string   `json:"key"`
	ParentKey string   `json:"parent_key"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Tags      []string `json:"tags,omitempty"`
	DateAdded string   `json:"date_added"`
}

// ListDoclingNotes returns every non-trashed child note of parentKey
// that is tagged "docling". Same join pattern as DoclingNoteKeys but
// returns full ChildItem structs with tags hydrated via queryChildren.
func (d *DB) ListDoclingNotes(parentKey string) ([]ChildItem, error) {
	const q = `
SELECT ni.key, it.typeName,
       COALESCE(n.title, ''),
       COALESCE(n.note, ''),
       '' AS contentType,
       '' AS filename
FROM items p
JOIN itemNotes n ON n.parentItemID = p.itemID
JOIN items ni ON ni.itemID = n.itemID
JOIN itemTypes it ON ni.itemTypeID = it.itemTypeID
JOIN itemTags itg ON ni.itemID = itg.itemID
JOIN tags t ON itg.tagID = t.tagID
LEFT JOIN deletedItems pdi ON p.itemID = pdi.itemID
LEFT JOIN deletedItems ndi ON ni.itemID = ndi.itemID
WHERE p.libraryID = ?
  AND p.key = ?
  AND t.name = 'docling'
  AND pdi.itemID IS NULL
  AND ndi.itemID IS NULL
ORDER BY ni.dateAdded
`
	return d.queryChildren(q, "list docling notes for", parentKey)
}

// ListAllDoclingNotes returns a summary of every non-trashed
// docling-tagged note in the library, along with its parent item's key
// and title. Ordered by note dateAdded DESC (most recent first).
func (d *DB) ListAllDoclingNotes() ([]DoclingNoteSummary, error) {
	const q = `
SELECT ni.key, p.key,
       COALESCE((
         SELECT idv.value
         FROM itemData id
         JOIN fields f ON f.fieldID = id.fieldID
         JOIN itemDataValues idv ON idv.valueID = id.valueID
         WHERE id.itemID = p.itemID AND f.fieldName = 'title'
       ), '') AS parentTitle,
       COALESCE(n.note, ''),
       ni.dateAdded
FROM items p
JOIN itemNotes n ON n.parentItemID = p.itemID
JOIN items ni ON ni.itemID = n.itemID
JOIN itemTags itg ON ni.itemID = itg.itemID
JOIN tags t ON itg.tagID = t.tagID
LEFT JOIN deletedItems pdi ON p.itemID = pdi.itemID
LEFT JOIN deletedItems ndi ON ni.itemID = ndi.itemID
WHERE p.libraryID = ?
  AND t.name = 'docling'
  AND pdi.itemID IS NULL
  AND ndi.itemID IS NULL
ORDER BY ni.dateAdded DESC
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list all docling notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DoclingNoteSummary
	for rows.Next() {
		var s DoclingNoteSummary
		if err := rows.Scan(&s.NoteKey, &s.ParentKey, &s.ParentTitle,
			&s.Body, &s.DateAdded); err != nil {
			return nil, fmt.Errorf("scan docling note summary: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		tags, err := d.childTags(out[i].NoteKey)
		if err != nil {
			return nil, err
		}
		out[i].Tags = tags
	}
	return out, nil
}

// ReadNote returns the full detail of a single note item by its key.
// Returns an error if the key doesn't exist, is trashed, or is not a
// note item type.
func (d *DB) ReadNote(noteKey string) (*NoteDetail, error) {
	const q = `
SELECT ni.key, COALESCE(p.key, ''),
       COALESCE(n.title, ''),
       COALESCE(n.note, ''),
       ni.dateAdded
FROM items ni
JOIN itemNotes n ON n.itemID = ni.itemID
LEFT JOIN items p ON p.itemID = n.parentItemID
LEFT JOIN deletedItems ndi ON ni.itemID = ndi.itemID
WHERE ni.libraryID = ?
  AND ni.key = ?
  AND ndi.itemID IS NULL
`
	var nd NoteDetail
	err := d.db.QueryRow(q, d.libraryID, noteKey).Scan(
		&nd.Key, &nd.ParentKey, &nd.Title, &nd.Body, &nd.DateAdded)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("note %s not found (may be missing, trashed, or not a note)", noteKey)
		}
		return nil, fmt.Errorf("read note %s: %w", noteKey, err)
	}

	tags, err := d.childTags(nd.Key)
	if err != nil {
		return nil, err
	}
	nd.Tags = tags
	return &nd, nil
}
