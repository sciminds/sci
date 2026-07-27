package local

import (
	"cmp"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"
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
	// Titles maps every key named above to a display label, where one was
	// resolvable. A relation is only meaningful if you can tell what is on
	// the other end, and an 8-char key doesn't tell you. Keys pointing into
	// another library (what owl:sameAs is for) have no local row and are
	// simply absent.
	Titles map[string]string `json:"titles,omitempty"`
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

// relationLabelMax bounds a body-derived label. Long enough to recognize a
// note by its opening sentence, short enough to sit on one terminal line
// beside the key.
const relationLabelMax = 60

// ItemLabels resolves keys to display labels in ONE query.
//
// Both ends of a relation can be either a regular item or a note, and the
// two store their name differently — an item's title is an EAV field, a
// note's is the line Zotero derives from its first paragraph. This returns
// whichever applies, so callers don't have to try Read then ReadNote per
// key (which is O(n) round-trips and picks the wrong error to swallow).
//
// Keys with no row in this library are omitted rather than mapped to "":
// an absent label means "render the bare key", and a relation pointing at
// another library's item is normal, not an error.
func (d *DB) ItemLabels(keys []string) (map[string]string, error) {
	keys = lo.Uniq(lo.Filter(keys, func(k string, _ int) bool { return k != "" }))
	if len(keys) == 0 {
		return map[string]string{}, nil
	}

	q := `
SELECT i.key, ` + fieldValueSubquery + ` AS title, n.title, n.note
FROM items i
LEFT JOIN itemNotes n ON n.itemID = i.itemID
LEFT JOIN deletedItems di ON di.itemID = i.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL
  AND i.key IN (` + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + `)
`
	args := slices.Concat([]any{"title", d.libraryID}, lo.ToAnySlice(keys))
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("item labels: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var key string
		var itemTitle, noteTitle, noteBody sql.NullString
		if err := rows.Scan(&key, &itemTitle, &noteTitle, &noteBody); err != nil {
			return nil, fmt.Errorf("scan item label: %w", err)
		}
		if label := relationLabel(cmp.Or(itemTitle.String, noteTitle.String), noteBody.String); label != "" {
			out[key] = label
		}
	}
	return out, rows.Err()
}

// relationLabel picks one item's display label: its title when it has one,
// otherwise a snippet of the note body.
//
// The fallback matters because a standalone note is the far end sci links
// most often, and Zotero only derives a note title on save — a note posted
// through the Web API can arrive with none. The snippet is plain text via
// [NoteText], so a docling extraction's YAML provenance header would show
// through; that is accepted rather than fixed here, because stripping it
// lives in internal/zot/content, which imports this package.
func relationLabel(title, noteBody string) string {
	if title != "" {
		return title
	}
	s := NoteText(UnwrapZoteroDiv(noteBody))
	if len(s) > relationLabelMax {
		s = s[:relationLabelMax-3] + "..."
	}
	return s
}
