package local

import "fmt"

// MissingPDFKeys returns the keys of bibliographic items that have no PDF
// attachment — the predicate `doctor pdfs` actually wants, computed from
// local data in one query instead of being outsourced to a saved search
// whose conditions cannot express it.
//
// "No PDF attachment" means no non-trashed child whose contentType is
// application/pdf or whose path ends in .pdf — the same two-sided test the
// hygiene scans use, because linked-file attachments predating content-type
// detection carry only the path. Bibliographic items are top-level by
// construction (only attachments, notes, and annotations can be children
// in Zotero), so the childless test needs no parent filter of its own.
// A parent whose only children are notes or non-PDF attachments IS
// missing its PDF — that is exactly the case a `numChildren == 0` reading
// gets wrong.
func (d *DB) MissingPDFKeys() ([]string, error) {
	libFrag, libArgs := d.libIn("i")
	q := fmt.Sprintf(`
SELECT i.key
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE %s AND di.itemID IS NULL
%s
  AND NOT EXISTS (
	SELECT 1 FROM itemAttachments ia
	JOIN items ch ON ia.itemID = ch.itemID
	LEFT JOIN deletedItems cdi ON ch.itemID = cdi.itemID
	WHERE ia.parentItemID = i.itemID
	  AND cdi.itemID IS NULL
	  AND (ia.contentType = 'application/pdf'
	       OR (ia.path IS NOT NULL AND lower(ia.path) LIKE '%%.pdf')))
ORDER BY i.dateAdded DESC
`, libFrag, hygieneItemTypeFilter)

	rows, err := d.db.Query(q, libArgs...)
	if err != nil {
		return nil, fmt.Errorf("scan items missing a PDF: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
