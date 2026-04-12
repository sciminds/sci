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
