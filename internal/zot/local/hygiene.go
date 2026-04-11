package local

import "fmt"

// DuplicateCandidate is the pared-down per-item view the duplicate
// detector needs: just enough to bucket by DOI, compare titles, and show
// the user a summary row when a cluster is found. Lives in `local` so
// the SQL scan and the clustering code share a single definition.
type DuplicateCandidate struct {
	Key      string
	Title    string
	DOI      string
	Date     string
	PDFCount int
}

// ItemFieldPresence is a per-item report of which hygiene-relevant fields
// are populated. One row is emitted per non-deleted content item.
//
// Presence is intentionally coarse — "does this field have *any* value" —
// because that's the bar hygiene checks score against. Validity of the
// value (e.g. whether the DOI parses) is a separate check.
//
// Title is carried twice: as the display string (possibly empty) and as
// HasTitle so the missing check can distinguish "no title row" from
// "title row with empty string".
type ItemFieldPresence struct {
	Key          string
	Title        string
	HasTitle     bool
	HasDOI       bool
	HasAbstract  bool
	HasDate      bool
	HasURL       bool
	CreatorCount int
	TagCount     int
	PDFCount     int
}

// ScanFieldPresence returns presence flags for every content item in the
// user library. Runs one SELECT with correlated subqueries — fine for
// libraries in the 10k-item range, which is the ceiling we care about.
//
// Attachments and notes are excluded, matching the same filter used by
// List/Search/Stats.
func (d *DB) ScanFieldPresence() ([]ItemFieldPresence, error) {
	q := `
SELECT i.key,
	(SELECT idv.value FROM itemData id
	 JOIN fields f ON id.fieldID = f.fieldID
	 JOIN itemDataValues idv ON id.valueID = idv.valueID
	 WHERE id.itemID = i.itemID AND f.fieldName = 'title') AS title,
	EXISTS(SELECT 1 FROM itemData id
	       JOIN fields f ON id.fieldID = f.fieldID
	       JOIN itemDataValues idv ON id.valueID = idv.valueID
	       WHERE id.itemID = i.itemID AND f.fieldName = 'title' AND TRIM(idv.value) <> '') AS has_title,
	(SELECT COUNT(*) FROM itemCreators ic WHERE ic.itemID = i.itemID) AS creator_count,
	EXISTS(SELECT 1 FROM itemData id
	       JOIN fields f ON id.fieldID = f.fieldID
	       JOIN itemDataValues idv ON id.valueID = idv.valueID
	       WHERE id.itemID = i.itemID AND f.fieldName = 'DOI' AND TRIM(idv.value) <> '') AS has_doi,
	EXISTS(SELECT 1 FROM itemData id
	       JOIN fields f ON id.fieldID = f.fieldID
	       JOIN itemDataValues idv ON id.valueID = idv.valueID
	       WHERE id.itemID = i.itemID AND f.fieldName = 'abstractNote' AND TRIM(idv.value) <> '') AS has_abstract,
	EXISTS(SELECT 1 FROM itemData id
	       JOIN fields f ON id.fieldID = f.fieldID
	       JOIN itemDataValues idv ON id.valueID = idv.valueID
	       WHERE id.itemID = i.itemID AND f.fieldName = 'date' AND TRIM(idv.value) <> '') AS has_date,
	EXISTS(SELECT 1 FROM itemData id
	       JOIN fields f ON id.fieldID = f.fieldID
	       JOIN itemDataValues idv ON id.valueID = idv.valueID
	       WHERE id.itemID = i.itemID AND f.fieldName = 'url' AND TRIM(idv.value) <> '') AS has_url,
	(SELECT COUNT(*) FROM itemTags it2 WHERE it2.itemID = i.itemID) AS tag_count,
	(SELECT COUNT(*) FROM itemAttachments ia
	 JOIN items ch ON ia.itemID = ch.itemID
	 LEFT JOIN deletedItems cdi ON ch.itemID = cdi.itemID
	 WHERE ia.parentItemID = i.itemID
	   AND cdi.itemID IS NULL
	   AND (ia.contentType = 'application/pdf'
	        OR (ia.path IS NOT NULL AND lower(ia.path) LIKE '%.pdf'))) AS pdf_count
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL
` + contentItemTypeFilter + `
ORDER BY i.dateAdded DESC
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan field presence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ItemFieldPresence
	for rows.Next() {
		var p ItemFieldPresence
		var title *string
		if err := rows.Scan(
			&p.Key, &title,
			&p.HasTitle, &p.CreatorCount,
			&p.HasDOI, &p.HasAbstract, &p.HasDate, &p.HasURL,
			&p.TagCount, &p.PDFCount,
		); err != nil {
			return nil, err
		}
		if title != nil {
			p.Title = *title
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ScanDuplicateCandidates returns one row per content item with the
// fields the duplicate clusterers need. Like ScanFieldPresence, this
// excludes trashed items, attachments, and notes.
//
// Dates are emitted raw (Zotero's "YYYY-MM-DD originalText" dual-encoding).
// The display layer trims them via cleanDate as needed.
func (d *DB) ScanDuplicateCandidates() ([]DuplicateCandidate, error) {
	q := `
SELECT i.key,
	COALESCE((SELECT idv.value FROM itemData id
	          JOIN fields f ON id.fieldID = f.fieldID
	          JOIN itemDataValues idv ON id.valueID = idv.valueID
	          WHERE id.itemID = i.itemID AND f.fieldName = 'title'), '') AS title,
	COALESCE((SELECT idv.value FROM itemData id
	          JOIN fields f ON id.fieldID = f.fieldID
	          JOIN itemDataValues idv ON id.valueID = idv.valueID
	          WHERE id.itemID = i.itemID AND f.fieldName = 'DOI'), '') AS doi,
	COALESCE((SELECT idv.value FROM itemData id
	          JOIN fields f ON id.fieldID = f.fieldID
	          JOIN itemDataValues idv ON id.valueID = idv.valueID
	          WHERE id.itemID = i.itemID AND f.fieldName = 'date'), '') AS date,
	(SELECT COUNT(*) FROM itemAttachments ia
	 JOIN items ch ON ia.itemID = ch.itemID
	 LEFT JOIN deletedItems cdi ON ch.itemID = cdi.itemID
	 WHERE ia.parentItemID = i.itemID
	   AND cdi.itemID IS NULL
	   AND (ia.contentType = 'application/pdf'
	        OR (ia.path IS NOT NULL AND lower(ia.path) LIKE '%.pdf'))) AS pdf_count
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL
` + contentItemTypeFilter + `
ORDER BY i.dateAdded DESC
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan duplicate candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DuplicateCandidate
	for rows.Next() {
		var c DuplicateCandidate
		if err := rows.Scan(&c.Key, &c.Title, &c.DOI, &c.Date, &c.PDFCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
