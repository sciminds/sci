package cli

import (
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
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
