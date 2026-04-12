package local

import (
	"database/sql"
	"fmt"
	"strings"
)

// PDFAttachment describes a resolved PDF child attachment of a parent
// item. Used by the extract flow to locate the PDF on disk and
// populate the note header's human-readable title.
//
// The on-disk path is <dataDir>/storage/<Key>/<Filename> — caller
// combines with zot.AttachmentPath or filepath.Join to resolve.
//
// Title is the parent item's `title` field (the paper's canonical
// name), NOT the attachment item's title. Zotero stores the "real"
// bibliographic metadata on the parent; attachment titles are
// import-time artifacts (often garbage URLs for scraped PDFs).
type PDFAttachment struct {
	Key      string // 8-char attachment item key
	Filename string // basename under storage/<Key>/ (no "storage:" prefix)
	Title    string // parent item's title (paper name), not the attachment's
}

// ResolvePDFAttachment returns the first PDF attachment of parentKey.
// "PDF" means either contentType = "application/pdf" or path ending
// in .pdf — mirroring the hygiene checks for consistency.
//
// Returns an error when:
//   - parent item does not exist or is trashed
//   - parent has no PDF attachment (trashed attachments are filtered)
//
// Ordering: oldest-first (ch.dateAdded ASC) so repeat calls are stable
// across library state that doesn't change.
func (d *DB) ResolvePDFAttachment(parentKey string) (*PDFAttachment, error) {
	// Title lookup is a correlated subquery against the PARENT item,
	// not the attachment. Zotero stores canonical bibliographic titles
	// on the parent; the attachment's own title is an import-time
	// artifact (often a source URL for scraped PDFs).
	const q = `
SELECT ch.key, COALESCE(ia.path, ''),
       COALESCE((
         SELECT idv.value
         FROM itemData id
         JOIN fields f ON f.fieldID = id.fieldID
         JOIN itemDataValues idv ON idv.valueID = id.valueID
         WHERE id.itemID = p.itemID AND f.fieldName = 'title'
       ), '') AS title
FROM items p
JOIN itemAttachments ia ON ia.parentItemID = p.itemID
JOIN items ch ON ch.itemID = ia.itemID
LEFT JOIN deletedItems pdi ON pdi.itemID = p.itemID
LEFT JOIN deletedItems cdi ON cdi.itemID = ch.itemID
WHERE p.libraryID = ?
  AND p.key = ?
  AND pdi.itemID IS NULL
  AND cdi.itemID IS NULL
  AND (ia.contentType = 'application/pdf'
       OR (ia.path IS NOT NULL AND lower(ia.path) LIKE '%.pdf'))
ORDER BY ch.dateAdded
LIMIT 1
`
	var key, path, title string
	err := d.db.QueryRow(q, d.libraryID, parentKey).Scan(&key, &path, &title)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no PDF attachment for parent %s (parent may be missing, trashed, or have no PDF child)", parentKey)
		}
		return nil, fmt.Errorf("resolve PDF attachment for %s: %w", parentKey, err)
	}
	return &PDFAttachment{
		Key:      key,
		Filename: strings.TrimPrefix(path, "storage:"),
		Title:    title,
	}, nil
}

// PDFParent bundles a parent item key with its resolved best-PDF
// attachment metadata. Used by the bulk extract-lib flow to
// pre-resolve the whole library in a single query.
type PDFParent struct {
	ParentKey  string
	Attachment PDFAttachment
}

// ListAllPDFAttachments returns every non-trashed parent item that
// has at least one PDF child attachment, along with the best-match
// (oldest-added) PDF metadata for each parent. Standalone attachments
// (parentItemID NULL) are excluded.
//
// The query mirrors ResolvePDFAttachment's selection logic — same
// content-type / extension heuristic, same dateAdded ordering — so
// bulk results are consistent with per-item lookups.
func (d *DB) ListAllPDFAttachments() ([]PDFParent, error) {
	const q = `
SELECT p.key, ch.key, COALESCE(ia.path, ''),
       COALESCE((
         SELECT idv.value
         FROM itemData id
         JOIN fields f ON f.fieldID = id.fieldID
         JOIN itemDataValues idv ON idv.valueID = id.valueID
         WHERE id.itemID = p.itemID AND f.fieldName = 'title'
       ), '') AS title
FROM items p
JOIN itemAttachments ia ON ia.parentItemID = p.itemID
JOIN items ch ON ch.itemID = ia.itemID
LEFT JOIN deletedItems pdi ON pdi.itemID = p.itemID
LEFT JOIN deletedItems cdi ON cdi.itemID = ch.itemID
WHERE p.libraryID = ?
  AND pdi.itemID IS NULL
  AND cdi.itemID IS NULL
  AND (ia.contentType = 'application/pdf'
       OR (ia.path IS NOT NULL AND lower(ia.path) LIKE '%.pdf'))
GROUP BY p.itemID
HAVING ch.dateAdded = MIN(ch.dateAdded)
ORDER BY p.key
`
	rows, err := d.db.Query(q, d.libraryID)
	if err != nil {
		return nil, fmt.Errorf("list all PDF attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []PDFParent
	for rows.Next() {
		var parentKey, attKey, path, title string
		if err := rows.Scan(&parentKey, &attKey, &path, &title); err != nil {
			return nil, fmt.Errorf("scan PDF attachment row: %w", err)
		}
		out = append(out, PDFParent{
			ParentKey: parentKey,
			Attachment: PDFAttachment{
				Key:      attKey,
				Filename: strings.TrimPrefix(path, "storage:"),
				Title:    title,
			},
		})
	}
	return out, rows.Err()
}
