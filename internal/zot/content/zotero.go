package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/samber/lo"
	"github.com/sciminds/sci/pkg/local"
)

// Library is the slice of [local.Reader] the content indexer needs.
// Narrowing it here keeps the indexer testable without a SQLite fixture
// and makes the read-only contract visible.
type Library interface {
	ContentSources() ([]local.ContentSource, error)
	NoteBodyByID(noteID int64) (string, error)
	ContentSignature() (string, error)
}

// ftCacheName is the file Zotero writes beside an attachment holding the
// plain text it extracted for its own search index.
const ftCacheName = ".zotero-ft-cache"

// DefaultPath returns the on-disk location of the content index for a
// library. Indexes are per-library because item keys are only unique
// within one, and because a user's personal and group libraries are
// searched separately.
func DefaultPath(libraryID int64) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "sci", "zot", "content",
		"lib"+strconv.FormatInt(libraryID, 10)+".db"), nil
}

// Candidates reads the library's available text sources.
func Candidates(lib Library) ([]Candidate, error) {
	sources, err := lib.ContentSources()
	if err != nil {
		return nil, err
	}
	return lo.Map(sources, func(s local.ContentSource, _ int) Candidate {
		return Candidate{
			ItemKey:        s.ItemKey,
			DoclingNoteID:  s.DoclingNoteID,
			DoclingVersion: s.DoclingVersion,
			AttachmentKey:  s.AttachmentKey,
			ZoteroVersion:  s.ZoteroVersion,
		}
	}), nil
}

// ZoteroLoader returns a [LoadFunc] that reads text from whichever
// source a candidate resolved to: the extraction note out of
// zotero.sqlite, or Zotero's own cache file under dataDir/storage.
func ZoteroLoader(lib Library, dataDir string) LoadFunc {
	return func(c Candidate, src Source) (string, error) {
		switch src {
		case SourceDocling:
			body, err := lib.NoteBodyByID(c.DoclingNoteID)
			if err != nil {
				return "", err
			}
			// Notes are stored as HTML-wrapped markdown, so three steps
			// in a fixed order:
			//
			//  1. UnwrapZoteroDiv drops Zotero's wrapper but keeps the
			//     markdown's line structure.
			//  2. StripProvenance drops sci's own header, which is
			//     metadata about the extraction rather than the paper.
			//     It has to run here, before step 3: NoteText joins on
			//     whitespace, which flattens the YAML block into one
			//     line and leaves nothing to recognize.
			//  3. NoteText renders to plain text, so the wrapper's own
			//     markup ("div", "znv1") never becomes a searchable term.
			return local.NoteText(StripProvenance(local.UnwrapZoteroDiv(body))), nil
		case SourceZotero:
			return readFTCache(dataDir, c.AttachmentKey)
		default:
			return "", fmt.Errorf("unknown content source %q for %s", src, c.ItemKey)
		}
	}
}

// readFTCache reads Zotero's extracted-text cache for an attachment.
//
// A missing file returns empty text and no error. Zotero indexes an
// attachment's words in SQLite but does not always keep the text cache
// beside it (linked files, purged storage), so absence is an ordinary
// state meaning "no text here" — [Build] skips those. Reserving errors
// for real I/O failures keeps a permissions problem visible instead of
// silently shrinking the index.
func readFTCache(dataDir, attachmentKey string) (string, error) {
	if attachmentKey == "" {
		return "", nil
	}
	path := filepath.Join(dataDir, "storage", attachmentKey, ftCacheName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read Zotero text cache for %s: %w", attachmentKey, err)
	}
	return string(data), nil
}

// Sync brings the index up to date with the library: it plans the diff,
// then indexes only what changed. Planning reads no bodies, so calling
// Sync when nothing has changed costs one query against each side.
func Sync(ix *Index, lib Library, dataDir string, opts Options) (Result, error) {
	plan, err := PlanSync(ix, lib)
	if err != nil {
		return Result{}, err
	}
	res, err := Build(ix, plan, ZoteroLoader(lib, dataDir), opts)
	if err != nil {
		return res, err
	}
	return res, RecordBuilt(ix, lib)
}

// RecordBuilt stamps the index with the library fingerprint and the
// document format it now reflects, so [Stale] and [PlanSync] can answer
// cheaply. Every path that finishes a build must call it — an index that
// never records its fingerprint reports itself fresh forever, because
// [Stale] reads a missing signature as "never built".
func RecordBuilt(ix *Index, lib Library) error {
	if err := ix.SetMeta(MetaFormat, strconv.Itoa(IndexFormat)); err != nil {
		return err
	}
	sig, err := lib.ContentSignature()
	if err != nil {
		return err
	}
	return ix.SetMeta(MetaSignature, sig)
}

// formatOutdated reports whether the index's text was normalized under a
// different [IndexFormat] than this code produces — including an index
// built before formats were recorded, which reads as format 0.
func formatOutdated(ix *Index) (bool, error) {
	recorded, err := ix.GetMeta(MetaFormat)
	if err != nil {
		return false, err
	}
	return recorded != strconv.Itoa(IndexFormat), nil
}

// StaleReason says why an index no longer reflects what a search would
// need — and, because the two causes call for different explanations to
// the user, which one it is.
type StaleReason string

const (
	// StaleFresh means the index is up to date. It is the zero value, so
	// `if reason != "" ` reads as "is it stale".
	StaleFresh StaleReason = ""
	// StaleLibrary means the library moved: papers were added, extracted
	// or removed since the build.
	StaleLibrary StaleReason = "library"
	// StaleFormat means sci's indexing rules changed, so the text on disk
	// is not what this version would produce. Nothing about the user's
	// library is wrong; the index is simply older than the code.
	StaleFormat StaleReason = "format"
)

// Stale reports whether the library has changed since the index was
// built.
//
// The returned [StaleReason] is [StaleFresh] when it has not.
//
// It compares fingerprints rather than computing a [Plan], because a
// plan has to enumerate every item's sources — around half a second on a
// real library, which is too much to spend on every search. The
// fingerprint is a few aggregate counts, so this costs milliseconds.
//
// The tradeoff is deliberate: a fingerprint can miss a change that
// leaves the counts and versions identical. This drives a warning, never
// a correctness decision — `zot content build` always computes the real
// diff.
func Stale(ix *Index, lib Library) (StaleReason, error) {
	// An indexer-format change is staleness the library fingerprint can
	// never show: the text on disk is not what this code would write.
	outdated, err := formatOutdated(ix)
	if err != nil {
		return StaleFresh, err
	}
	if outdated {
		return StaleFormat, nil
	}
	recorded, err := ix.GetMeta(MetaSignature)
	if err != nil {
		return StaleFresh, err
	}
	if recorded == "" {
		// Never built, or built before signatures existed. An empty
		// index is reported by its own emptiness, not as staleness.
		return StaleFresh, nil
	}
	current, err := lib.ContentSignature()
	if err != nil {
		return StaleFresh, err
	}
	if current != recorded {
		return StaleLibrary, nil
	}
	return StaleFresh, nil
}

// PlanSync computes what a [Sync] would do without doing it — the shape
// a --dry-run or a staleness check wants.
func PlanSync(ix *Index, lib Library) (Plan, error) {
	cands, err := Candidates(lib)
	if err != nil {
		return Plan{}, err
	}
	indexed, err := ix.State()
	if err != nil {
		return Plan{}, err
	}
	outdated, err := formatOutdated(ix)
	if err != nil {
		return Plan{}, err
	}
	if outdated {
		return NewRebuildPlan(cands, indexed), nil
	}
	return NewPlan(cands, indexed), nil
}
