package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/local"
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
			// Notes are stored as HTML-wrapped markdown. Index the
			// rendered text so the wrapper's own markup ("div", "znv1")
			// never becomes a searchable term — the same reason
			// NoteText exists for note search.
			return local.NoteText(body), nil
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
	return res, RecordSignature(ix, lib)
}

// RecordSignature stamps the index with the library fingerprint it now
// reflects, so [Stale] can answer cheaply.
func RecordSignature(ix *Index, lib Library) error {
	sig, err := lib.ContentSignature()
	if err != nil {
		return err
	}
	return ix.SetMeta(MetaSignature, sig)
}

// Stale reports whether the library has changed since the index was
// built.
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
func Stale(ix *Index, lib Library) (bool, error) {
	recorded, err := ix.GetMeta(MetaSignature)
	if err != nil {
		return false, err
	}
	if recorded == "" {
		// Never built, or built before signatures existed. An empty
		// index is reported by its own emptiness, not as staleness.
		return false, nil
	}
	current, err := lib.ContentSignature()
	if err != nil {
		return false, err
	}
	return current != recorded, nil
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
	return NewPlan(cands, indexed), nil
}
