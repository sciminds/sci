package local

import "io"

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

	// Items
	List(f ListFilter) ([]Item, error)
	ListAll(f ListFilter) ([]Item, error)
	Search(query string, limit int) ([]Item, error)
	Read(key string) (*Item, error)
	Stats() (*Stats, error)

	// Collections & Tags
	ListCollections() ([]Collection, error)
	ListTags() ([]Tag, error)

	// Children
	ListChildren(parentKey string) ([]ChildItem, error)

	// Notes (docling extraction notes)
	ListDoclingNotes(parentKey string) ([]ChildItem, error)
	ListAllDoclingNotes() ([]DoclingNoteSummary, error)
	ReadNote(noteKey string) (*NoteDetail, error)

	// PDF Resolution
	ResolvePDFAttachment(parentKey string) (*PDFAttachment, error)
	ListAllPDFAttachments() ([]PDFParent, error)
	ParentsWithDoclingNotes() (map[string]bool, error)
	DoclingNoteKeys(parentKey string) ([]string, error)

	// View (denormalized reads for UI)
	ListViewRows() ([]ViewRow, error)
	CountViewRows() (int, error)
	DoclingNoteBodyByItemID() (map[int64]string, error)

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
