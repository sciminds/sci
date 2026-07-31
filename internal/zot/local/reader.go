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
// in the type system: local reads go through Reader, writes go through
// the Web API (internal/zot/api).
//
// Do NOT add write methods to this interface. If you need to mutate the
// library, use the Zotero Web API — see internal/zot/api.
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
	Search(query string, limit int) ([]Item, error)
	SearchWith(query string, limit int, opts SearchOptions) ([]Item, error)
	SearchWithTotal(query string, limit int, opts SearchOptions) ([]Item, int, error)
	Read(key string) (*Item, error)
	GetItemsByKeys(keys []string) ([]Item, error)
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

	// Notes (docling extraction notes)
	ListDoclingNotes(parentKey string) ([]ChildItem, error)
	ListAllDoclingNotes() ([]DoclingNoteSummary, error)
	ListNotes() ([]NoteSummary, error)
	ReadNote(noteKey string) (*NoteDetail, error)
	ItemRelations(itemKey string) (ItemRelationSet, error)
	ItemLabels(keys []string) (map[string]string, error)

	// PDF Resolution
	ResolvePDFAttachment(parentKey string) (*PDFAttachment, error)
	ListAllPDFAttachments() ([]PDFParent, error)
	ParentsWithDoclingNotes() (map[string]bool, error)
	ParentsWithDoclingNotesMissingTag(tag string) ([]string, error)
	DoclingNoteKeys(parentKey string) ([]string, error)

	// View (denormalized reads for UI)
	ListViewRows() ([]ViewRow, error)
	CountViewRows() (int, error)
	DoclingNoteBodyByItemID() (map[int64]string, error)

	// Fulltext search (Zotero's word-level PDF content index)
	SearchFulltext(words []string, exact bool) ([]int64, error)

	// Content index sources (see internal/zot/content)
	ContentSources() ([]ContentSource, error)
	NoteBodyByID(noteID int64) (string, error)
	ContentSignature() (string, error)
	ItemIDsForKeys(keys []string) ([]int64, error)

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
