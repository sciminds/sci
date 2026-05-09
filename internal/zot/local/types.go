package local

import "strconv"

// Item is a denormalized snapshot of a Zotero item for list/search/read views.
// Fields that may be absent are string-typed rather than pointers — empty
// string is the natural "unset" in Zotero's EAV storage.
//
// Extra carries Zotero's free-text "Extra" field (citation managers stash
// `Key: Value` lines there — OpenAlex IDs, BBT citation keys, ORCID, …).
// Surfaced as a typed field so agents don't need to grep Fields["extra"].
//
// Citekey is the resolved BibTeX cite-key (Zotero 7 native field, BBT
// `Citation Key:` line in Extra, or our synthesized fallback). Set by
// callers via citekey.Enrich after Read / ItemFromClient — the local
// package can't import citekey (cycle). Empty when not enriched.
type Item struct {
	ID           int64             `json:"id"`
	Key          string            `json:"key"`
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
type Collection struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
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
	Limit         int // 0 → default 50
	Offset        int
	OrderBy       OrderBy
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

// contentItemTypeFilter returns the SQL fragment excluding attachment/note
// rows from a query joined on itemTypes as alias "it". These are children
// of "real" items and should not appear in top-level listings.
const contentItemTypeFilter = " AND it.typeName NOT IN ('attachment','note') "

// isExcludedContentType reports whether t is one of the item types that
// contentItemTypeFilter would otherwise strip from listings. Used by
// List/ListAll to opt out of the blanket exclusion when the caller
// explicitly asked for notes or attachments via ListFilter.ItemType.
func isExcludedContentType(t string) bool {
	return t == "note" || t == "attachment"
}
