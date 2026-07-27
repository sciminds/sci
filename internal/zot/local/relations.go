package local

import (
	"fmt"
	"regexp"
)

// RelatedPredicate is Zotero's predicate for user-facing "related items" —
// the only one sci writes. Kept in sync with api.RelatedPredicate; the two
// packages don't import each other, and the string is Zotero's, not ours.
const RelatedPredicate = "dc:relation"

// ItemRelationSet is the relations stored on one item, split by who owns
// them.
//
// The split matters for more than display: Related is the set a user can
// add to and remove, while Other holds Zotero's own bookkeeping —
// owl:sameAs linking a group copy to its personal twin, dc:replaces from a
// merge. Presenting them as one list would invite `link rm` to delete
// something Zotero maintains.
type ItemRelationSet struct {
	// Related is the dc:relation object keys.
	Related []string `json:"related,omitempty"`
	// Other is every remaining predicate → object keys.
	Other map[string][]string `json:"other,omitempty"`
}

// relationURIRe extracts the item key from a Zotero item URI. The library
// segment stays loose: a relation legitimately points into another library
// (that is what owl:sameAs is for), and the local DB stores the URI
// verbatim regardless of which library it names.
var relationURIRe = regexp.MustCompile(`^https?://zotero\.org/(?:users|groups)/\d+/items/([A-Z0-9]+)$`)

// ItemRelations returns the relations stored on itemKey.
//
// Objects that aren't Zotero item URIs are skipped rather than erroring —
// the column is free-form text and a library can hold relations pointing at
// arbitrary URIs.
//
// This reads the local mirror, so a relation written through the Web API
// won't appear until Zotero desktop syncs it back. Callers that need
// ground truth immediately after a write should go through
// api.Client.ItemRelations instead.
func (d *DB) ItemRelations(itemKey string) (ItemRelationSet, error) {
	const q = `
SELECT p.predicate, r.object
FROM items i
JOIN itemRelations r ON r.itemID = i.itemID
JOIN relationPredicates p ON p.predicateID = r.predicateID
WHERE i.libraryID = ?
  AND i.key = ?
ORDER BY p.predicate, r.object
`
	rows, err := d.db.Query(q, d.libraryID, itemKey)
	if err != nil {
		return ItemRelationSet{}, fmt.Errorf("item relations for %s: %w", itemKey, err)
	}
	defer func() { _ = rows.Close() }()

	out := ItemRelationSet{}
	for rows.Next() {
		var predicate, object string
		if err := rows.Scan(&predicate, &object); err != nil {
			return ItemRelationSet{}, fmt.Errorf("scan relation: %w", err)
		}
		m := relationURIRe.FindStringSubmatch(object)
		if m == nil {
			continue
		}
		key := m[1]
		if predicate == RelatedPredicate {
			out.Related = append(out.Related, key)
			continue
		}
		if out.Other == nil {
			out.Other = map[string][]string{}
		}
		out.Other[predicate] = append(out.Other[predicate], key)
	}
	return out, rows.Err()
}
