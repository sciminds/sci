package cli

import (
	"fmt"
	"slices"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/oacache"
	"github.com/sciminds/cli/pkg/openalex"
)

func TestWantFromSkipsWhatIsNotAWork(t *testing.T) {
	items := []local.Item{
		{Key: "A", Type: "journalArticle", DOI: "10.1/a", Title: "A"},
		{Key: "B", Type: "annotation", Title: ""},
		{Key: "C", Type: "note", Title: "a note"},
		{Key: "D", Type: "attachment", Title: "paper.pdf"},
	}
	w, scanned := wantFrom(items, true)
	// Annotations, notes and attachments are Zotero objects, not works. A
	// title search for "paper.pdf" burns a metered request and returns
	// noise that then has to be resolved against.
	if scanned != 1 {
		t.Errorf("scanned %d items, want only the journalArticle", scanned)
	}
	if len(w.DOIs) != 1 || len(w.Titles) != 0 {
		t.Errorf("want = %+v, want one DOI and no titles", w)
	}
}

func TestAnItemWithADOIIsNotAlsoLookedUpByTitle(t *testing.T) {
	items := []local.Item{{Key: "A", Type: "journalArticle", DOI: "10.1/a", Title: "A paper"}}
	w, _ := wantFrom(items, true)
	// The DOI is the stronger identifier; the title request would cost a
	// request and buy nothing.
	if len(w.Titles) != 0 {
		t.Errorf("titles = %v, want none for an item that has a DOI", w.Titles)
	}
}

func TestCrossLibraryDuplicatesCostOneLookupNotTwo(t *testing.T) {
	items := []local.Item{
		{Key: "A", Library: "personal", Type: "journalArticle", DOI: "10.1/Same", Title: "T"},
		{Key: "B", Library: "shared", Type: "journalArticle", DOI: "10.1/same", Title: "T"},
		{Key: "C", Library: "personal", Type: "book", Title: "Untitled Elsewhere"},
		{Key: "D", Library: "shared", Type: "book", Title: "untitled elsewhere"},
	}
	w, scanned := wantFrom(items, true)
	if scanned != 4 {
		t.Errorf("scanned = %d, want all four", scanned)
	}
	// The library holds 189 deliberate cross-library duplicate pairs. Not
	// deduplicating the plan pays for every one of them twice, against an
	// API that now bills per request.
	if len(w.DOIs) != 1 {
		t.Errorf("dois = %v, want one — the pair differs only in case", w.DOIs)
	}
	if len(w.Titles) != 1 {
		t.Errorf("titles = %v, want one", w.Titles)
	}
}

func TestTitleLookupsCanBeTurnedOff(t *testing.T) {
	items := []local.Item{{Key: "A", Type: "book", Title: "A book with no DOI"}}
	w, _ := wantFrom(items, false)
	if len(w.Titles) != 0 {
		t.Errorf("titles = %v, want none with --titles=false", w.Titles)
	}
}

// ---------------------------------------------------------------------------
// Targeting a delta sync
// ---------------------------------------------------------------------------

// baseWith plants a works cache holding exactly these DOIs — plus the ones
// a previous run recorded as absent from OpenAlex — so --missing has
// something real to diff against.
func baseWith(t *testing.T, notFound []string, dois ...string) oacache.Base {
	t.Helper()
	dir := t.TempDir()
	works := lo.Map(dois, func(d string, i int) openalex.Work {
		doi, name := "https://doi.org/"+d, "cached"
		return openalex.Work{ID: fmt.Sprintf("https://openalex.org/W%d", i+1), DOI: &doi, DisplayName: &name}
	})
	if _, err := oacache.Write(dir, "all", oacache.Result{Works: works, NotFound: notFound}); err != nil {
		t.Fatal(err)
	}
	base, err := oacache.LoadBase(dir, oacache.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func TestKeysTargetOnlyTheItemsTheyName(t *testing.T) {
	items := []local.Item{
		{Key: "AAAA1111", Type: "journalArticle", DOI: "10.1/a", Title: "A"},
		{Key: "BBBB2222", Type: "journalArticle", DOI: "10.1/b", Title: "B"},
		{Key: "CCCC3333", Type: "book", Title: "C"},
	}
	// The whole point of a targeted sync: the library has 6,600 items and
	// the run may spend fifty requests.
	got := targetItems(items, []string{"bbbb2222"}, false, oacache.Base{})
	if len(got.Items) != 1 || got.Items[0].Key != "BBBB2222" {
		t.Fatalf("targeted %+v, want only BBBB2222", got.Items)
	}
	if len(got.Unmatched) != 0 {
		t.Errorf("unmatched = %v", got.Unmatched)
	}
}

func TestAKeyThatNamesNothingIsReportedNotIgnored(t *testing.T) {
	items := []local.Item{
		{Key: "AAAA1111", Type: "journalArticle", DOI: "10.1/a", Title: "A"},
		{Key: "NOTE0001", Type: "note", Title: "a note"},
	}
	got := targetItems(items, []string{"AAAA1111", "ZZZZ9999", "NOTE0001"}, false, oacache.Base{})
	// A key the library does not hold, and a key naming something that is
	// not a work, are both "this run will not do what you asked". Dropping
	// them silently would report a successful sync of nothing.
	if len(got.Items) != 1 {
		t.Fatalf("targeted %d items, want the one bibliographic key", len(got.Items))
	}
	if !slices.Equal(got.Unmatched, []string{"ZZZZ9999", "NOTE0001"}) {
		t.Errorf("unmatched = %v, want both the absent key and the note", got.Unmatched)
	}
}

func TestMissingTargetsOnlyTheDOIsTheCacheDoesNotHold(t *testing.T) {
	base := baseWith(t, nil, "10.1/cached")
	items := []local.Item{
		{Key: "AAAA1111", Type: "journalArticle", DOI: "https://doi.org/10.1/CACHED", Title: "Cached"},
		{Key: "BBBB2222", Type: "journalArticle", DOI: "10.1/fresh", Title: "Fresh"},
		{Key: "CCCC3333", Type: "book", Title: "No DOI at all"},
	}
	got := targetItems(items, nil, true, base)
	if len(got.Items) != 1 || got.Items[0].Key != "BBBB2222" {
		t.Fatalf("targeted %+v, want only the uncached DOI", got.Items)
	}
	// A DOI-less item cannot be answered here. Deciding whether the cache
	// already holds a candidate for its TITLE is resolution, and title_norm
	// is defined exactly once, in zot. So the item is counted and reported
	// rather than guessed at — an unattended run that quietly skipped it
	// would look identical to one that had nothing to skip.
	if got.WithoutDOI != 1 {
		t.Errorf("without_doi = %d, want the DOI-less book counted", got.WithoutDOI)
	}
}

func TestAnItemBothNamedAndMissingIsFetchedOnce(t *testing.T) {
	base := baseWith(t, nil)
	items := []local.Item{{Key: "AAAA1111", Type: "journalArticle", DOI: "10.1/fresh", Title: "Fresh"}}
	got := targetItems(items, []string{"AAAA1111"}, true, base)
	if len(got.Items) != 1 {
		t.Errorf("targeted %d, want one — the two selectors overlap, they do not add up", len(got.Items))
	}
}

func TestKeysArriveEitherRepeatedOrCommaSeparated(t *testing.T) {
	got := splitKeys([]string{"AAAA1111,BBBB2222", " cccc3333 "})
	if !slices.Equal(got, []string{"AAAA1111", "BBBB2222", "CCCC3333"}) {
		t.Errorf("splitKeys = %v", got)
	}
}

func TestMissingDoesNotReAskForDOIsOpenAlexAlreadyAnsweredFor(t *testing.T) {
	base := baseWith(t, []string{"10.1/monograph"}, "10.1/cached")
	items := []local.Item{
		{Key: "AAAA1111", Type: "book", DOI: "10.1/monograph", Title: "A monograph"},
		{Key: "BBBB2222", Type: "journalArticle", DOI: "10.1/fresh", Title: "Fresh"},
	}
	got := targetItems(items, nil, true, base)
	// OpenAlex 404s on monographs, chapters and plenty of preprints, so
	// those DOIs are missing from the cache permanently. Targeting them
	// because they are absent means paying the same ~45 requests on every
	// unattended run, forever, for an answer already recorded.
	if len(got.Items) != 1 || got.Items[0].Key != "BBBB2222" {
		t.Fatalf("targeted %+v, want only the DOI nobody has asked about", got.Items)
	}
	if got.KnownAbsent != 1 {
		t.Errorf("known_absent = %d, want the monograph counted, not silently skipped", got.KnownAbsent)
	}
	// Naming it outright still asks: that is the way back in for a work
	// OpenAlex has since indexed.
	if named := targetItems(items, []string{"AAAA1111"}, true, base); len(named.Items) != 2 {
		t.Errorf("--keys targeted %d items, want the named monograph asked again", len(named.Items))
	}
}
