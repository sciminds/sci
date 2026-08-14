package local

import (
	"io"
	"time"
)

// Reader is the read-only contract for the local Zotero database.
//
// Every method is a pure query — the underlying connection enforces
// mode=ro, immutable=1, and _pragma=query_only(1) as defence-in-depth.
// Consumers that accept Reader instead of *DB make the firewall visible
// in the type system.
//
// Do NOT add write methods to this interface. Mutating a Zotero library is
// not this package's job: zotero.sqlite is Zotero desktop's private store,
// and writing to it behind the app's back corrupts its sync state.
type Reader interface {
	io.Closer

	// Metadata
	LibraryID() int64
	SchemaVersion() int
	SchemaOutOfRange() bool
	LastSync() (time.Time, bool)

	// PendingWAL sizes the committed Zotero changes this handle cannot see,
	// because Open always uses immutable mode. See the package doc.
	PendingWAL() (int64, bool)

	// Items
	List(f ListFilter) ([]Item, error)
	CountList(f ListFilter) (int, error)
	ListAll(f ListFilter) ([]Item, error)
	SearchWith(query string, limit int) ([]Item, error)
	SearchWithTotal(query string, limit int) ([]Item, int, error)
	Read(key string) (*Item, error)
	ItemKeysByDOI(dois []string) (map[string]string, error)
	Stats() (*Stats, error)

	// Collections & Tags
	ListCollections() ([]Collection, error)
	CollectionByKey(key string) (*Collection, error)
	ListTags() ([]Tag, error)

	// Orient (agent bootstrap signals; see orient.go)
	TopTags(n int) ([]TagCount, error)
	TopCollections(n int) ([]CollectionRef, error)
	RecentlyAdded(n int) ([]RecentItem, error)
	ExtractionCoverage() (*ExtractionCoverage, error)

	// Children
	ListChildren(parentKey string) ([]ChildItem, error)

	// Notes
	ListNotes() ([]NoteSummary, error)
	ReadNote(noteKey string) (*NoteDetail, error)
	ItemRelations(itemKey string) (ItemRelationSet, error)
	ItemLabels(keys []string) (map[string]string, error)

	// Bulk hydration (see hydrate.go) — the set-based counterparts to
	// what Read does per item. For callers that want every item's
	// tags/collections/attachments without N round trips.
	HydrateAll(items []Item) error

	// PDF Resolution
	ResolvePDFAttachment(parentKey string) (*PDFAttachment, error)
	ListAllPDFAttachments() ([]PDFParent, error)
	ParentsWithDoclingNotes() (map[string]bool, error)

	// View (denormalized reads for UI)
	ListViewRows() ([]ViewRow, error)
	CountViewRows() (int, error)
	DoclingNoteBodyByItemID() (map[int64]string, error)

	// Fulltext search (Zotero's word-level PDF content index)
	SearchFulltext(words []string, exact bool) ([]int64, error)

	// Hygiene Scans
	ScanFieldPresence() ([]ItemFieldPresence, error)
	ScanDuplicateCandidates() ([]DuplicateCandidate, error)
	ScanCiteKeys() ([]CiteKeyRow, error)
	ScanFieldValues(fields []string) ([]FieldValue, error)
	ScanEmptyCollections() ([]Collection, error)
	ScanStandaloneAttachments() ([]StandaloneAttachment, error)
	ScanStandaloneNotes() ([]StandaloneNote, error)
	ScanUncollectedItems() ([]Item, error)
	ScanAttachmentFiles() ([]StandaloneAttachment, error)
	ScanUnusedTags() ([]Tag, error)
}

// Compile-time assertion: *DB satisfies Reader.
var _ Reader = (*DB)(nil)
