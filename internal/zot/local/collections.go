package local

import (
	"database/sql"
	"fmt"
)

// CollectionByKey returns the collection with the given key, or (nil, nil)
// when no such collection exists in this library. Used by the CLI to detect
// "this key isn't in the local DB" — the agent-friendly hint that the
// collection was likely created moments ago and a `--remote` lookup is
// needed before Zotero desktop syncs.
func (d *DB) CollectionByKey(key string) (*Collection, error) {
	row := d.db.QueryRow(`
		SELECT
			c.key,
			c.collectionName,
			parent.key,
			(SELECT COUNT(*) FROM collectionItems ci WHERE ci.collectionID = c.collectionID)
		FROM collections c
		LEFT JOIN collections parent ON c.parentCollectionID = parent.collectionID
		WHERE c.libraryID = ? AND c.key = ?
	`, d.libraryID, key)
	var c Collection
	var parent *string
	if err := row.Scan(&c.Key, &c.Name, &parent, &c.ItemCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("collection %s lookup: %w", key, err)
	}
	if parent != nil {
		c.ParentKey = *parent
	}
	return &c, nil
}

// ListCollections returns all collections in the user library with item
// counts and parent references. Results are returned flat (not nested).
func (d *DB) ListCollections() ([]Collection, error) {
	rows, err := d.db.Query(`
		SELECT
			c.key,
			c.collectionName,
			parent.key,
			(SELECT COUNT(*) FROM collectionItems ci WHERE ci.collectionID = c.collectionID)
		FROM collections c
		LEFT JOIN collections parent ON c.parentCollectionID = parent.collectionID
		WHERE c.libraryID = ?
		ORDER BY c.collectionName COLLATE NOCASE ASC
	`, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Collection
	for rows.Next() {
		var c Collection
		var parent *string
		if err := rows.Scan(&c.Key, &c.Name, &parent, &c.ItemCount); err != nil {
			return nil, err
		}
		if parent != nil {
			c.ParentKey = *parent
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
