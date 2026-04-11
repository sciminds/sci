package local

import "fmt"

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
