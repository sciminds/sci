package local

// hydrate.go — set-based hydration of the per-item collections that
// List/ListAll deliberately leave empty.
//
// Read fills Tags/Collections/Attachments with one query each, per item.
// That is right for reading one item and wrong for reading the library:
// at 6k items it is 18k round trips. These return the whole scope's
// membership in one query apiece, keyed by item key, for callers that
// want every item hydrated — the same "one query, not N" discipline
// ItemLabels follows.
//
// All three filter through libIn, so they answer correctly under ForAll.

import (
	"database/sql"
	"fmt"
	"strings"
)

// TagsByItem returns every item's tag names in the handle's library
// scope, keyed by item key. Items with no tags are absent from the map.
func (d *DB) TagsByItem() (map[string][]string, error) {
	where, args := d.libIn("i")
	rows, err := d.db.Query(`
		SELECT i.key, tg.name
		FROM itemTags it
		JOIN items i ON it.itemID = i.itemID
		JOIN tags tg ON it.tagID = tg.tagID
		WHERE `+where+`
		ORDER BY i.key, tg.name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("tags by item: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]string{}
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, err
		}
		out[key] = append(out[key], name)
	}
	return out, rows.Err()
}

// CollectionsByItem returns every item's collection keys in the handle's
// library scope, keyed by item key. Items in no collection are absent.
func (d *DB) CollectionsByItem() (map[string][]string, error) {
	where, args := d.libIn("i")
	rows, err := d.db.Query(`
		SELECT i.key, c.key
		FROM collectionItems ci
		JOIN items i ON ci.itemID = i.itemID
		JOIN collections c ON ci.collectionID = c.collectionID
		WHERE `+where+`
		ORDER BY i.key, c.key
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("collections by item: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]string{}
	for rows.Next() {
		var itemKey, collKey string
		if err := rows.Scan(&itemKey, &collKey); err != nil {
			return nil, err
		}
		out[itemKey] = append(out[itemKey], collKey)
	}
	return out, rows.Err()
}

// AttachmentsByItem returns every parent item's attachments in the
// handle's library scope, keyed by PARENT item key. Parents with no
// attachments are absent from the map.
//
// ParentKey is populated on each Attachment so a row stays meaningful
// once lifted out of the map.
func (d *DB) AttachmentsByItem() (map[string][]Attachment, error) {
	where, args := d.libIn("p")
	rows, err := d.db.Query(`
		SELECT p.key, ch.key, ia.contentType, ia.path, ia.linkMode
		FROM itemAttachments ia
		JOIN items ch ON ia.itemID = ch.itemID
		JOIN items p ON ia.parentItemID = p.itemID
		WHERE `+where+`
		ORDER BY p.key, ch.dateAdded
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("attachments by item: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]Attachment{}
	for rows.Next() {
		var parentKey string
		var a Attachment
		var ct, path sql.NullString
		if err := rows.Scan(&parentKey, &a.Key, &ct, &path, &a.LinkMode); err != nil {
			return nil, err
		}
		a.ContentType = ct.String
		a.ParentKey = parentKey
		// Zotero stores attachment paths as "storage:filename.pdf".
		a.Filename = strings.TrimPrefix(path.String, "storage:")
		out[parentKey] = append(out[parentKey], a)
	}
	return out, rows.Err()
}

// HydrateAll fills Tags, Collections, and Attachments on every item in
// place, using three set-based queries regardless of len(items).
func (d *DB) HydrateAll(items []Item) error {
	tags, err := d.TagsByItem()
	if err != nil {
		return err
	}
	colls, err := d.CollectionsByItem()
	if err != nil {
		return err
	}
	atts, err := d.AttachmentsByItem()
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Tags = tags[items[i].Key]
		items[i].Collections = colls[items[i].Key]
		items[i].Attachments = atts[items[i].Key]
	}
	return nil
}
