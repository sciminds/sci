package local

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/samber/lo"
)

// ViewRow is a denormalized row for the read-only library view: one row per
// top-level content item, with a semicolon-joined author list (authors only,
// no editors) and raw passthrough of dateAdded plus the `extra` field.
//
// Year is the first four characters of the `date` field — Zotero stores date
// as "YYYY-MM-DD originalText" or a bare year, both start with the four-digit
// year we care about.
type ViewRow struct {
	ID        int64
	Authors   string
	Year      string
	Journal   string
	Title     string
	DateAdded string // raw "YYYY-MM-DD HH:MM:SS" UTC from items.dateAdded
	Extra     string
}

// viewRowSelect pulls one denormalized row per content item. Creators are
// hydrated in a second pass via authorsByItem — inlining them as a
// GROUP_CONCAT subquery here breaks ordering in older SQLite.
const viewRowSelect = `
SELECT
  i.itemID,
  i.dateAdded,
  (SELECT idv.value FROM itemData d JOIN fields f ON d.fieldID = f.fieldID
    JOIN itemDataValues idv ON d.valueID = idv.valueID
    WHERE d.itemID = i.itemID AND f.fieldName = 'title') AS title,
  (SELECT idv.value FROM itemData d JOIN fields f ON d.fieldID = f.fieldID
    JOIN itemDataValues idv ON d.valueID = idv.valueID
    WHERE d.itemID = i.itemID AND f.fieldName = 'date') AS date,
  (SELECT idv.value FROM itemData d JOIN fields f ON d.fieldID = f.fieldID
    JOIN itemDataValues idv ON d.valueID = idv.valueID
    WHERE d.itemID = i.itemID AND f.fieldName = 'publicationTitle') AS pub,
  (SELECT idv.value FROM itemData d JOIN fields f ON d.fieldID = f.fieldID
    JOIN itemDataValues idv ON d.valueID = idv.valueID
    WHERE d.itemID = i.itemID AND f.fieldName = 'extra') AS extra
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL ` + contentItemTypeFilter + `
ORDER BY i.dateAdded DESC
`

// ListViewRows returns every top-level content item in the user library as a
// denormalized ViewRow, sorted by dateAdded descending.
func (d *DB) ListViewRows() ([]ViewRow, error) {
	rows, err := d.db.Query(viewRowSelect, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list view rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out []ViewRow
		ids []int64
	)
	for rows.Next() {
		var r ViewRow
		var title, date, pub, extra sql.NullString
		if err := rows.Scan(&r.ID, &r.DateAdded, &title, &date, &pub, &extra); err != nil {
			return nil, err
		}
		r.Title = title.String
		r.Journal = pub.String
		r.Extra = extra.String
		if len(date.String) >= 4 {
			r.Year = date.String[:4]
		}
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	authors, err := d.authorsByItem(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Authors = authors[out[i].ID]
	}
	return out, nil
}

// CountViewRows returns the number of top-level content items — same filter
// as ListViewRows, but just a count.
func (d *DB) CountViewRows() (int, error) {
	const q = `
SELECT COUNT(*)
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL ` + contentItemTypeFilter
	var n int
	if err := d.db.QueryRow(q, d.libraryID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count view rows: %w", err)
	}
	return n, nil
}

// authorsByItem returns a pre-joined "Last, First; Last, First" string per
// item, restricted to creatorType='author' and ordered by itemCreators.orderIndex.
// One batched query instead of N per-item lookups.
func (d *DB) authorsByItem(itemIDs []int64) (map[int64]string, error) {
	result := make(map[int64]string, len(itemIDs))
	if len(itemIDs) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(itemIDs))
	placeholders = placeholders[:len(placeholders)-1]
	q := `
SELECT ic.itemID, c.fieldMode, c.firstName, c.lastName
FROM itemCreators ic
JOIN creators c ON ic.creatorID = c.creatorID
JOIN creatorTypes ct ON ic.creatorTypeID = ct.creatorTypeID
WHERE ct.creatorType = 'author' AND ic.itemID IN (` + placeholders + `)
ORDER BY ic.itemID, ic.orderIndex
`
	args := lo.Map(itemIDs, func(id int64, _ int) any {
		return id
	})
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("authors by item: %w", err)
	}
	defer func() { _ = rows.Close() }()

	parts := make(map[int64][]string, len(itemIDs))
	for rows.Next() {
		var id int64
		var fieldMode int
		var first, last sql.NullString
		if err := rows.Scan(&id, &fieldMode, &first, &last); err != nil {
			return nil, err
		}
		if name := formatViewAuthor(fieldMode, first.String, last.String); name != "" {
			parts[id] = append(parts[id], name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, names := range parts {
		result[id] = strings.Join(names, "; ")
	}
	return result, nil
}

// formatViewAuthor renders a Zotero creator row as "Last, First", falling
// back to single-name rendering for institutional creators (fieldMode=1) and
// for rows missing either component.
func formatViewAuthor(fieldMode int, first, last string) string {
	if fieldMode == 1 {
		return last
	}
	switch {
	case first == "" && last == "":
		return ""
	case first == "":
		return last
	case last == "":
		return first
	default:
		return last + ", " + first
	}
}
