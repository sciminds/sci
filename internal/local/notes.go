package local

import (
	"database/sql"
	"fmt"
)

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

// NoteSummary is a lightweight projection of a note a human wrote.
// ParentKey and ParentTitle are empty for a standalone note.
type NoteSummary struct {
	NoteKey     string   `json:"note_key"`
	ParentKey   string   `json:"parent_key,omitempty"`
	ParentTitle string   `json:"parent_title,omitempty"`
	Body        string   `json:"body"`
	DateAdded   string   `json:"date_added"`
	Tags        []string `json:"tags,omitempty"`
}

// ListNotes returns every non-trashed note in the library that is NOT a
// docling extraction — the notes a person wrote, as opposed to the paper
// text sci posted.
//
// The distinction is load-bearing: on the live library the extractions
// outnumber the real notes by roughly a hundred to one, so a listing that
// mixes them is a listing of extractions with the real notes lost inside it.
//
// The parent is LEFT-joined, unlike the docling query's inner join. A
// standalone note has no parent by definition, and those are precisely the
// notes worth surfacing — an inner join would silently drop them.
func (d *DB) ListNotes() ([]NoteSummary, error) {
	const q = `
SELECT ni.key,
       COALESCE(p.key, ''),
       COALESCE((
         SELECT idv.value
         FROM itemData id
         JOIN fields f ON f.fieldID = id.fieldID
         JOIN itemDataValues idv ON idv.valueID = id.valueID
         WHERE id.itemID = p.itemID AND f.fieldName = 'title'
       ), '') AS parentTitle,
       COALESCE(n.note, ''),
       ni.dateAdded
FROM items ni
JOIN itemNotes n ON n.itemID = ni.itemID
LEFT JOIN items p ON p.itemID = n.parentItemID
LEFT JOIN deletedItems ndi ON ni.itemID = ndi.itemID
LEFT JOIN deletedItems pdi ON p.itemID = pdi.itemID
WHERE ni.libraryID = ?
  AND ndi.itemID IS NULL
  AND pdi.itemID IS NULL
  AND NOT EXISTS (
        SELECT 1 FROM itemTags itg
        JOIN tags t ON itg.tagID = t.tagID
        WHERE itg.itemID = ni.itemID AND t.name = 'docling'
      )
ORDER BY ni.dateAdded DESC
`
	return d.queryNoteSummaries(q, "list notes")
}

// queryNoteSummaries runs a note-summary query and hydrates each row's tags.
// Both note listings — extractions and real notes — project the same five
// columns in the same order, so the scan-and-hydrate half is shared and only
// the WHERE clause differs.
//
// what names the operation in any error ("list notes", "list all docling
// notes"); the query must select key, parentKey, parentTitle, body,
// dateAdded in that order and take libraryID as its only parameter.
func (d *DB) queryNoteSummaries(q, what string) ([]NoteSummary, error) {
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()

	var out []NoteSummary
	for rows.Next() {
		var s NoteSummary
		if err := rows.Scan(&s.NoteKey, &s.ParentKey, &s.ParentTitle,
			&s.Body, &s.DateAdded); err != nil {
			return nil, fmt.Errorf("scan note summary: %w", err)
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
