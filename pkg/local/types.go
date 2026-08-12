package local

import (
	"slices"
	"strconv"
)

// Item is a denormalized snapshot of a Zotero item for list/search/read views.
// Fields that may be absent are string-typed rather than pointers — empty
// string is the natural "unset" in Zotero's EAV storage.
//
// Extra carries Zotero's free-text "Extra" field (citation managers stash
// `Key: Value` lines there — OpenAlex IDs, BBT citation keys, ORCID, …).
// Surfaced as a typed field so agents don't need to grep Fields["extra"].
//
// Citekey is the BibTeX cite-key. List/Search/Read rows populate it with
// the native Zotero 7 citationKey field when one is stored (pinned keys);
// full resolution — BBT `Citation Key:` line in Extra, synthesized
// fallback — happens via citekey.Enrich, which callers apply after Read /
// ItemFromClient (the local package can't import citekey: cycle). Empty
// when the key is neither stored nor enriched.
type Item struct {
	ID  int64  `json:"id"`
	Key string `json:"key"`
	// Library names the library scope this row came from — "personal" or
	// "shared", the same vocabulary as the --library flag. Stamped by
	// every local read; load-bearing under --library all, a constant
	// otherwise. Items built from the Web API leave it empty (the caller
	// asked a single library by construction).
	Library      string            `json:"library,omitempty"`
	Type         string            `json:"type"`
	Version      int               `json:"version"`
	Title        string            `json:"title,omitempty"`
	Date         string            `json:"date,omitempty"`
	Year         int               `json:"year,omitempty"`
	DOI          string            `json:"doi,omitempty"`
	URL          string            `json:"url,omitempty"`
	Abstract     string            `json:"abstract,omitempty"`
	Publication  string            `json:"publication,omitempty"`
	Extra        string            `json:"extra,omitempty"`
	Citekey      string            `json:"citekey,omitempty"`
	Creators     []Creator         `json:"creators,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Collections  []string          `json:"collections,omitempty"`
	Attachments  []Attachment      `json:"attachments,omitempty"`
	Fields       map[string]string `json:"fields,omitempty"`
	DateAdded    string            `json:"date_added,omitempty"`
	DateModified string            `json:"date_modified,omitempty"`
	// NumChildren is the count of attachments+notes for this item, populated
	// when items come from the Zotero Web API (`meta.numChildren`). Local DB
	// queries leave it zero — the local path doesn't filter on it.
	NumChildren int `json:"num_children,omitempty"`
	// Relations is the item's related items (Zotero's dc:relation) plus the
	// predicates Zotero maintains itself. Intrinsic to the item, like Tags
	// and Attachments, and populated by the same reads that populate those
	// — Read and api.ItemFromClient — never by List/Search. A pointer so an
	// item with no relations omits the key entirely rather than emitting an
	// empty object.
	Relations *ItemRelationSet `json:"relations,omitempty"`
}

// Creator holds one author/editor/etc. fieldMode=1 indicates a single-name
// creator (institution); in that case Name is populated and First/Last are empty.
type Creator struct {
	Type     string `json:"type"`
	First    string `json:"first,omitempty"`
	Last     string `json:"last,omitempty"`
	Name     string `json:"name,omitempty"` // single-name mode (institutions)
	OrderIdx int    `json:"order_idx"`
}

// Attachment is a file or note attached to a parent item.
type Attachment struct {
	Key         string `json:"key"`
	ParentKey   string `json:"parent_key,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Filename    string `json:"filename,omitempty"`
	LinkMode    int    `json:"link_mode"`
}

// Collection is a user-defined organizational folder.
//
// Library names the library scope this row came from — "personal" or
// "shared", the same vocabulary as Item.Library and the --library flag.
// Load-bearing under --library all, where collection keys from two
// libraries land in one result set; a constant otherwise.
type Collection struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Library   string `json:"library,omitempty"`
	ParentKey string `json:"parent_key,omitempty"`
	ItemCount int    `json:"item_count"`
}

// Tag is a library tag with usage count.
type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Type  int    `json:"type"` // 0 = manual, 1 = automatic
}

// Stats is a library-wide summary.
type Stats struct {
	TotalItems     int            `json:"total_items"`
	ByType         map[string]int `json:"by_type"`
	WithDOI        int            `json:"with_doi"`
	WithAbstract   int            `json:"with_abstract"`
	WithAttachment int            `json:"with_attachment"`
	Collections    int            `json:"collections"`
	Tags           int            `json:"tags"`
}

// ListFilter narrows a listing query. Zero-value fields are ignored.
type ListFilter struct {
	ItemType      string // e.g. "journalArticle"
	CollectionKey string
	Tag           string
	// Keys restricts results to these 8-char Zotero item keys. Used by
	// bulk hydration paths (search --export, zot bib) to avoid per-item
	// Read round-trips.
	Keys    []string
	Limit   int // 0 → default 50
	Offset  int
	OrderBy OrderBy
	// Mirror keeps PDF annotations in the result set. ListAll sets it
	// itself, because ListAll IS the lossless mirror the NDJSON export is
	// built from — leaving that to the caller means the mirror silently
	// loses a class of object the first time someone forgets.
	//
	// Every other listing leaves it false: an annotation has no title and
	// no authors, so a search or an item list that returns one is showing
	// a highlight where a paper should be.
	Mirror bool
}

// OrderBy selects the sort order for listings.
type OrderBy int

// Supported sort orders for item listings.
const (
	OrderDateAddedDesc OrderBy = iota
	OrderDateModifiedDesc
	OrderTitleAsc
)

// ParseYear extracts a publication year from a Zotero date string. Zotero
// stores dates as "YYYY-MM-DD originalText" with "00" padding for
// unspecified components (year-only is "1871-00-00 1871"). The first
// four bytes are always the year when present. Returns 0 for empty
// strings or strings that don't start with 4 digits.
func ParseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

// Two filters, because two different questions get asked of the same
// table, and one fragment answering both is how they came to be answered
// wrongly. "Which rows are PAPERS" and "which rows are real Zotero
// objects" differ on exactly the child/parentless boundary.

// mirrorItemTypeFilter excludes attachment/note rows that HAVE a parent,
// and keeps the ones that do not.
//
// The distinction is the difference between a lossless mirror and one with
// a silent hole. A child attachment rides nested inside its parent's
// Attachments array, so it reaches a consumer either way and emitting it
// at top level too would double-count it. A PARENTLESS attachment has
// nothing to ride: excluding it by type drops it from the mirror entirely,
// and on the live library that was 51 real PDFs plus 3 standalone notes
// that no consumer could see at all.
const mirrorItemTypeFilter = `
 AND ( it.typeName NOT IN ('attachment','note')
       OR (it.typeName = 'attachment'
           AND NOT EXISTS (SELECT 1 FROM itemAttachments ia
                           WHERE ia.itemID = i.itemID AND ia.parentItemID IS NOT NULL))
       OR (it.typeName = 'note'
           AND NOT EXISTS (SELECT 1 FROM itemNotes inn
                           WHERE inn.itemID = i.itemID AND inn.parentItemID IS NOT NULL)) ) `

// hygieneItemTypeFilter is for queries asking about PAPERS: listings,
// search, stats, and every health check. It excludes attachments, notes
// and annotations alike — all three are children of a real item, and none
// of them is a thing anyone cites.
//
// A PDF annotation is a row in the items table with no title, no creators
// and no DOI, so a health check that counts it reports three missing-field
// findings for a perfectly healthy highlight. On the live library that was
// 108 annotations manufacturing 109 untitled-item ERRORS over exactly one
// real one — enough noise to bury the finding that mattered.
const hygieneItemTypeFilter = " AND it.typeName NOT IN ('attachment','note','annotation') "

// nonBibliographicTypes is the same rule in Go, for callers holding rows
// rather than building a query. It must list exactly the types
// hygieneItemTypeFilter excludes — TestNonBibliographicTypesMatchSQL is
// what keeps the two from drifting apart.
var nonBibliographicTypes = []string{"attachment", "note", "annotation"}

// IsBibliographic reports whether an item of this Zotero type is a thing
// anyone cites. Annotations, notes and attachments are real Zotero items
// and belong in a lossless mirror, but none of them is a reference.
//
// The bibliography exporters need it because ListAll is deliberately
// lossless — it feeds the NDJSON mirror, so it returns standalone
// attachments, standalone notes and annotations. On the live library that
// put 39 non-references into the exported .bib, 36 of them titleless
// `@misc` entries that nothing downstream could render or verify.
func IsBibliographic(itemType string) bool {
	return !slices.Contains(nonBibliographicTypes, itemType)
}

// isExcludedContentType reports whether t is one of the item types that
// hygieneItemTypeFilter would otherwise strip from listings. Used by
// List/ListAll to opt out of the blanket exclusion when the caller
// explicitly asked for notes or attachments via ListFilter.ItemType.
func isExcludedContentType(t string) bool {
	return t == "note" || t == "attachment"
}
