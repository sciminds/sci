package content

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

// fakeLibrary stands in for local.Reader's content-source surface.
type fakeLibrary struct {
	sources   []local.ContentSource
	bodies    map[int64]string
	signature string
	err       error
}

func (f fakeLibrary) ContentSignature() (string, error) {
	return cmp.Or(f.signature, "v1:0:0:0"), nil
}

func (f fakeLibrary) ContentSources() ([]local.ContentSource, error) {
	return f.sources, f.err
}

func (f fakeLibrary) NoteBodyByID(noteID int64) (string, error) {
	body, ok := f.bodies[noteID]
	if !ok {
		return "", errors.New("no such note")
	}
	return body, nil
}

func TestCandidatesMapsLocalRows(t *testing.T) {
	lib := fakeLibrary{sources: []local.ContentSource{
		{ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 6, AttachmentKey: "DDDD4444", ZoteroVersion: 3},
		{ItemKey: "GGGG7777", AttachmentKey: "HHHH8888", ZoteroVersion: 4},
	}}

	got, err := Candidates(lib)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}
	want := Candidate{ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 6,
		AttachmentKey: "DDDD4444", ZoteroVersion: 3}
	if got[0] != want {
		t.Errorf("got[0] = %+v, want %+v", got[0], want)
	}
}

// Extraction notes are stored as HTML-wrapped markdown. The indexer must
// store rendered text, or the wrapper's own markup ("div", "znv1")
// becomes searchable content.
func TestLoaderStripsNoteMarkup(t *testing.T) {
	lib := fakeLibrary{bodies: map[int64]string{
		90: `<div class="zotero-note znv1"><h1>Successor Representations</h1>` +
			`<p>The map factorizes graph communicability &amp; predictive maps.</p></div>`,
	}}
	load := ZoteroLoader(lib, t.TempDir())

	body, err := load(Candidate{ItemKey: "AAAA1111", DoclingNoteID: 90}, SourceDocling)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, markup := range []string{"div", "znv1", "zotero-note", "<h1>"} {
		if strings.Contains(body, markup) {
			t.Errorf("loaded body still contains markup %q: %q", markup, body)
		}
	}
	if !strings.Contains(body, "Successor Representations") {
		t.Errorf("loaded body lost its text: %q", body)
	}
	// The entity must be decoded, not left as &amp;.
	if !strings.Contains(body, "communicability & predictive") {
		t.Errorf("HTML entity was not decoded: %q", body)
	}
}

// Heading text and the paragraph after it must not weld into one token,
// or a query matches a word that was never written.
func TestLoaderSeparatesAdjacentTags(t *testing.T) {
	lib := fakeLibrary{bodies: map[int64]string{90: `<h1>Alpha</h1><p>Beta</p>`}}
	load := ZoteroLoader(lib, t.TempDir())

	body, err := load(Candidate{DoclingNoteID: 90}, SourceDocling)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Contains(body, "AlphaBeta") {
		t.Errorf("adjacent tags welded into one token: %q", body)
	}
}

func TestLoaderReadsZoteroCache(t *testing.T) {
	dataDir := t.TempDir()
	attDir := filepath.Join(dataDir, "storage", "HHHH8888")
	if err := os.MkdirAll(attDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const text = "Extracted by Zotero's own indexer.\nSecond line."
	if err := os.WriteFile(filepath.Join(attDir, ".zotero-ft-cache"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	load := ZoteroLoader(fakeLibrary{}, dataDir)
	body, err := load(Candidate{ItemKey: "GGGG7777", AttachmentKey: "HHHH8888"}, SourceZotero)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if body != text {
		t.Errorf("body = %q, want %q", body, text)
	}
}

// Zotero indexes a PDF's words but does not always keep the text cache
// (linked files, purged storage). That is an absence of text, not a
// failure — Build should skip the item, so the loader returns empty
// rather than an error.
func TestLoaderTreatsMissingCacheAsNoText(t *testing.T) {
	load := ZoteroLoader(fakeLibrary{}, t.TempDir())

	body, err := load(Candidate{ItemKey: "GGGG7777", AttachmentKey: "NOSUCHAT"}, SourceZotero)
	if err != nil {
		t.Fatalf("missing cache returned an error, want a benign empty body: %v", err)
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestLoaderPropagatesNoteReadErrors(t *testing.T) {
	load := ZoteroLoader(fakeLibrary{bodies: map[int64]string{}}, t.TempDir())

	if _, err := load(Candidate{ItemKey: "AAAA1111", DoclingNoteID: 90}, SourceDocling); err == nil {
		t.Error("load returned nil error for an unreadable note")
	}
}

func TestSyncIndexesFromLibrary(t *testing.T) {
	dataDir := t.TempDir()
	attDir := filepath.Join(dataDir, "storage", "HHHH8888")
	if err := os.MkdirAll(attDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attDir, ".zotero-ft-cache"),
		[]byte("cortical attention dynamics"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib := fakeLibrary{
		sources: []local.ContentSource{
			{ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 6},
			{ItemKey: "GGGG7777", AttachmentKey: "HHHH8888", ZoteroVersion: 4},
		},
		bodies: map[int64]string{90: `<p>reward prediction error signals</p>`},
	}
	ix := openTestIndex(t)

	res, err := Sync(ix, lib, dataDir, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Added != 2 {
		t.Fatalf("Added = %d, want 2 (%+v)", res.Added, res)
	}

	// Both sources are queryable through one path, and each hit knows
	// where its text came from.
	hits, err := ix.Search(Query{Text: "prediction", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Source != SourceDocling {
		t.Errorf("docling hit = %+v, want one docling-sourced hit", hits)
	}
	hits, err = ix.Search(Query{Text: "cortical", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Source != SourceZotero {
		t.Errorf("zotero hit = %+v, want one zotero-sourced hit", hits)
	}
}

// A second Sync with nothing changed must do no work — this is what
// makes it affordable to call before every search.
func TestSyncIsIncremental(t *testing.T) {
	lib := fakeLibrary{
		sources: []local.ContentSource{{ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 6}},
		bodies:  map[int64]string{90: "<p>stable text</p>"},
	}
	ix := openTestIndex(t)

	if _, err := Sync(ix, lib, t.TempDir(), Options{}); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	res, err := Sync(ix, lib, t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if res.Added != 0 || res.Updated != 0 || res.Deleted != 0 {
		t.Errorf("second Sync did work: %+v", res)
	}
}

// Re-extracting a paper bumps the note version; the index must follow.
func TestSyncReindexesOnVersionBump(t *testing.T) {
	lib := fakeLibrary{
		sources: []local.ContentSource{{ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 6}},
		bodies:  map[int64]string{90: "<p>original wording</p>"},
	}
	ix := openTestIndex(t)
	if _, err := Sync(ix, lib, t.TempDir(), Options{}); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	lib.sources = []local.ContentSource{{ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 7}}
	lib.bodies = map[int64]string{90: "<p>revised wording</p>"}

	res, err := Sync(ix, lib, t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if res.Updated != 1 {
		t.Errorf("Updated = %d, want 1", res.Updated)
	}
	if hits, _ := ix.Search(Query{Text: "original", Limit: 10}); len(hits) != 0 {
		t.Error("superseded text still matches")
	}
	if hits, _ := ix.Search(Query{Text: "revised", Limit: 10}); len(hits) != 1 {
		t.Error("revised text is not searchable")
	}
}

func TestDefaultPathIsPerLibrary(t *testing.T) {
	a, err := DefaultPath(1)
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	b, err := DefaultPath(2)
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if a == b {
		t.Errorf("two libraries share an index path: %s", a)
	}
	if filepath.Ext(a) != ".db" {
		t.Errorf("index path %q does not look like a database file", a)
	}
}
