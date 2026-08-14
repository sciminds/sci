package local

import "fmt"

// ListTags returns every tag attached to at least one item in the library,
// with its usage count, ordered by count descending.
func (d *DB) ListTags() ([]Tag, error) {
	rows, err := d.db.Query(`
		SELECT tg.name, COUNT(DISTINCT it.itemID), MIN(it.type)
		FROM tags tg
		JOIN itemTags it ON tg.tagID = it.tagID
		JOIN items i ON it.itemID = i.itemID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ? AND di.itemID IS NULL
		GROUP BY tg.tagID, tg.name
		ORDER BY COUNT(DISTINCT it.itemID) DESC, tg.name COLLATE NOCASE ASC
	`, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.Name, &t.Count, &t.Type); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
