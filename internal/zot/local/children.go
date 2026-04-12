package local

import "fmt"

// ChildItem is the projection of a child item (note or attachment) of a
// parent content item. Used by the `zot item children` command and
// anywhere the UI needs to show what's attached to a library item.
//
// Mixed fields are populated based on ItemType:
//
//   - ItemType="note":       Note body + Title set, Filename/ContentType empty
//   - ItemType="attachment": Filename + ContentType set, Note empty
type ChildItem struct {
	Key         string   `json:"key"`
	ItemType    string   `json:"item_type"`
	Title       string   `json:"title,omitempty"`
	Note        string   `json:"note,omitempty"`         // body, notes only
	ContentType string   `json:"content_type,omitempty"` // attachments only
	Filename    string   `json:"filename,omitempty"`     // attachments only
	Tags        []string `json:"tags,omitempty"`
}

// ListChildren returns every non-trashed child of parentKey — notes,
// attachments, and anything else Zotero supports. Results are ordered
// by dateAdded ascending for stability.
func (d *DB) ListChildren(parentKey string) ([]ChildItem, error) {
	const q = `
SELECT ch.key, it.typeName,
       COALESCE(n.title, ''),
       COALESCE(n.note, ''),
       COALESCE(ia.contentType, ''),
       COALESCE(REPLACE(ia.path, 'storage:', ''), '')
FROM items p
JOIN items ch ON ch.libraryID = p.libraryID
LEFT JOIN itemAttachments ia ON ia.itemID = ch.itemID AND ia.parentItemID = p.itemID
LEFT JOIN itemNotes n ON n.itemID = ch.itemID AND n.parentItemID = p.itemID
JOIN itemTypes it ON ch.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems pdi ON p.itemID = pdi.itemID
LEFT JOIN deletedItems cdi ON ch.itemID = cdi.itemID
WHERE p.libraryID = ?
  AND p.key = ?
  AND pdi.itemID IS NULL
  AND cdi.itemID IS NULL
  AND (ia.parentItemID = p.itemID OR n.parentItemID = p.itemID)
ORDER BY ch.dateAdded
`
	rows, err := d.db.Query(q, d.libraryID, parentKey)
	if err != nil {
		return nil, fmt.Errorf("list children of %s: %w", parentKey, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ChildItem
	for rows.Next() {
		var ci ChildItem
		if err := rows.Scan(&ci.Key, &ci.ItemType, &ci.Title, &ci.Note,
			&ci.ContentType, &ci.Filename); err != nil {
			return nil, fmt.Errorf("scan child of %s: %w", parentKey, err)
		}
		out = append(out, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Hydrate tags in a second pass — avoids a GROUP BY / GROUP_CONCAT
	// that complicates the main query.
	for i := range out {
		tags, err := d.childTags(out[i].Key)
		if err != nil {
			return nil, err
		}
		out[i].Tags = tags
	}
	return out, nil
}

// childTags returns the tag names for a single item key.
func (d *DB) childTags(itemKey string) ([]string, error) {
	const q = `
SELECT t.name
FROM tags t
JOIN itemTags it ON t.tagID = it.tagID
JOIN items i ON it.itemID = i.itemID
WHERE i.key = ? AND i.libraryID = ?
ORDER BY t.name
`
	rows, err := d.db.Query(q, itemKey, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("tags for %s: %w", itemKey, err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
