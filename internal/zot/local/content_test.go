package local

import (
	"strings"
	"testing"

	"github.com/samber/lo"
)

// Fixture shape that matters here (fixture_test.go):
//
//	item 10 AAAA1111 — docling note 90 (v6) AND indexed attachment 40 (v3)
//	item 20 BBBB2222 — plain child note 91 only, no indexed attachment
//	item 80 GGGG7777 — indexed attachment 81 (v4), no extraction
//	item 60 ORPHANATT — standalone attachment (not a top-level item)
//	item 70 ORPHNNOTE — standalone note (not a top-level item)
func TestContentSources(t *testing.T) {
	db := openFixture(t)

	rows, err := db.ContentSources()
	if err != nil {
		t.Fatalf("ContentSources: %v", err)
	}
	byKey := lo.KeyBy(rows, func(r ContentSource) string { return r.ItemKey })

	// Both sources present — the item that will pick docling.
	got, ok := byKey["AAAA1111"]
	if !ok {
		t.Fatalf("AAAA1111 missing from candidates; got %v", lo.Keys(byKey))
	}
	want := ContentSource{
		ItemKey: "AAAA1111", DoclingNoteID: 90, DoclingVersion: 6,
		AttachmentKey: "DDDD4444", ZoteroVersion: 3,
	}
	if got != want {
		t.Errorf("AAAA1111 = %+v, want %+v", got, want)
	}

	// Zotero-only — the fallback case this whole design exists for.
	got, ok = byKey["GGGG7777"]
	if !ok {
		t.Fatalf("GGGG7777 missing from candidates; got %v", lo.Keys(byKey))
	}
	want = ContentSource{
		ItemKey: "GGGG7777", DoclingNoteID: 0, DoclingVersion: 0,
		AttachmentKey: "HHHH8888", ZoteroVersion: 4,
	}
	if got != want {
		t.Errorf("GGGG7777 = %+v, want %+v", got, want)
	}
}

// An item with a plain (non-docling) child note and no indexed PDF has
// no content. Returning it would create an index row that can never
// match and would inflate coverage stats.
func TestContentSourcesExcludesItemsWithNoText(t *testing.T) {
	db := openFixture(t)

	rows, err := db.ContentSources()
	if err != nil {
		t.Fatalf("ContentSources: %v", err)
	}
	for _, r := range rows {
		if r.DoclingNoteID == 0 && r.AttachmentKey == "" {
			t.Errorf("%s has neither source but was returned", r.ItemKey)
		}
	}
	if _, ok := lo.Find(rows, func(r ContentSource) bool { return r.ItemKey == "BBBB2222" }); ok {
		t.Error("BBBB2222 has only a plain child note — it is not a content candidate")
	}
}

// Notes and attachments are not items that have content of their own;
// they are where content comes from.
func TestContentSourcesExcludesNotesAndAttachments(t *testing.T) {
	db := openFixture(t)

	rows, err := db.ContentSources()
	if err != nil {
		t.Fatalf("ContentSources: %v", err)
	}
	keys := lo.Map(rows, func(r ContentSource, _ int) string { return r.ItemKey })
	for _, bad := range []string{"ORPHNNOTE", "NOTECH10", "NOTECH11", "ORPHANATT", "DDDD4444", "HHHH8888"} {
		if lo.Contains(keys, bad) {
			t.Errorf("%s is a note or attachment, not a content-bearing item", bad)
		}
	}
}

// Library scoping is load-bearing everywhere else in this package; the
// content index must not blend a group library into the personal one.
func TestContentSourcesIsLibraryScoped(t *testing.T) {
	db := openFixture(t)

	rows, err := db.ContentSources()
	if err != nil {
		t.Fatalf("ContentSources: %v", err)
	}
	for _, r := range rows {
		if strings.HasPrefix(r.ItemKey, "GRP") {
			t.Errorf("group-library item %s leaked into the personal candidates", r.ItemKey)
		}
	}
}

func TestNoteBodyByID(t *testing.T) {
	db := openFixture(t)

	body, err := db.NoteBodyByID(90)
	if err != nil {
		t.Fatalf("NoteBodyByID: %v", err)
	}
	if !strings.Contains(body, "docling") {
		t.Errorf("body = %q, want the fixture's extraction text", body)
	}
}

func TestNoteBodyByIDMissing(t *testing.T) {
	db := openFixture(t)

	if _, err := db.NoteBodyByID(999999); err == nil {
		t.Error("NoteBodyByID for a nonexistent id returned nil error, want one")
	}
}
