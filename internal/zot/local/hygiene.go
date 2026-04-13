package local

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
)

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

// CiteKeyRow is one row per content item surfacing the two places Zotero
// can stash a cite-key: the native top-level `citationKey` field (Zotero 7+,
// also where Better BibTeX writes in Z7) and the legacy `Citation Key:`
// line inside the `extra` field (pre-Z7 BBT libraries).
//
// The row carries raw field values only — resolution and validation belong
// in the hygiene check, which imports the zot package where ResolveCiteKey
// and ValidateCiteKey live. Keeping the helper dumb here avoids an
// import cycle (zot → local is fine, local → zot is not).
type CiteKeyRow struct {
	Key         string // 8-char Zotero item key
	Title       string
	CitationKey string // native citationKey field value ("" if unset)
	Extra       string // raw extra field value ("" if unset)
}

// FieldValue is one (item, field, value) tuple produced by ScanFieldValues,
// with the item's title attached for display. It's the generic shape the
// `invalid` hygiene check iterates over — one FieldValue per field present
// on each item, NOT one per item.
type FieldValue struct {
	Key   string
	Title string
	Field string
	Value string
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

// ScanCiteKeys returns one row per content item with the stored
// `citationKey` and `extra` field values. Empty strings mean "field not
// present for this item" — the check layer treats both the same way.
//
// Scope matches the other hygiene scans: content items only (attachments
// and notes excluded), trashed items excluded, scoped to the configured
// library. One query with two correlated subqueries is adequate at the
// 10k-item ceiling we care about.
func (d *DB) ScanCiteKeys() ([]CiteKeyRow, error) {
	q := `
SELECT i.key,
	COALESCE((SELECT idv.value FROM itemData id
	          JOIN fields f ON id.fieldID = f.fieldID
	          JOIN itemDataValues idv ON id.valueID = idv.valueID
	          WHERE id.itemID = i.itemID AND f.fieldName = 'title'), '') AS title,
	COALESCE((SELECT idv.value FROM itemData id
	          JOIN fields f ON id.fieldID = f.fieldID
	          JOIN itemDataValues idv ON id.valueID = idv.valueID
	          WHERE id.itemID = i.itemID AND f.fieldName = 'citationKey'), '') AS citation_key,
	COALESCE((SELECT idv.value FROM itemData id
	          JOIN fields f ON id.fieldID = f.fieldID
	          JOIN itemDataValues idv ON id.valueID = idv.valueID
	          WHERE id.itemID = i.itemID AND f.fieldName = 'extra'), '') AS extra
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL
` + contentItemTypeFilter + `
ORDER BY i.key
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("scan cite-keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CiteKeyRow
	for rows.Next() {
		var r CiteKeyRow
		if err := rows.Scan(&r.Key, &r.Title, &r.CitationKey, &r.Extra); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ScanFieldValues returns every non-empty (item, field, value) tuple for
// the requested field names, scoped to content items in the user library
// (trashed, attachment, and note rows excluded). Each row carries the
// owning item's title for display.
//
// Fields are Zotero's internal names (case-sensitive): "DOI", "ISBN",
// "url", "date", "title", "abstractNote", etc. Passing nil or empty
// returns zero rows.
//
// The query uses a single IN-list against the fields table, so cost
// scales with the number of matching itemData rows — much cheaper than
// one correlated subquery per field.
func (d *DB) ScanFieldValues(fields []string) ([]FieldValue, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	placeholders := lo.Times(len(fields), func(_ int) string { return "?" })
	args := append(lo.Map(fields, func(f string, _ int) any { return f }), d.libraryID)

	q := `
SELECT i.key,
	COALESCE((SELECT idv2.value FROM itemData id2
	          JOIN fields f2 ON id2.fieldID = f2.fieldID
	          JOIN itemDataValues idv2 ON id2.valueID = idv2.valueID
	          WHERE id2.itemID = i.itemID AND f2.fieldName = 'title'), '') AS title,
	f.fieldName,
	idv.value
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
JOIN itemData id ON i.itemID = id.itemID
JOIN fields f ON id.fieldID = f.fieldID
JOIN itemDataValues idv ON id.valueID = idv.valueID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE f.fieldName IN (` + strings.Join(placeholders, ",") + `)
  AND di.itemID IS NULL
  AND i.libraryID = ?
  AND TRIM(idv.value) <> ''
` + contentItemTypeFilter + `
ORDER BY i.key, f.fieldName
`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("scan field values: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FieldValue
	for rows.Next() {
		var fv FieldValue
		if err := rows.Scan(&fv.Key, &fv.Title, &fv.Field, &fv.Value); err != nil {
			return nil, err
		}
		out = append(out, fv)
	}
	return out, rows.Err()
}
