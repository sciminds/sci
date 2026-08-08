package local

import (
	"testing"
)

func TestListAll_HydratesCreatorsAndFields(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.ListAll(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// 7 rows: 4 content items, the annotation, and the two PARENTLESS
	// objects (a standalone attachment and a standalone note). ListAll is
	// the mirror the NDJSON export is built from, so it keeps every real
	// Zotero object even where no listing shows one -- "staging is the
	// durable state" is only true if staging actually holds it.
	//
	// Child attachments and notes stay out: they ride nested inside their
	// parent, and emitting them here too would double-count them. Trashed
	// items stay out unconditionally.
	if len(items) != 7 {
		t.Fatalf("len = %d, want 7", len(items))
	}
	// Locate the journalArticle with two authors (item AAAA1111).
	var deep *Item
	for i := range items {
		if items[i].Key == "AAAA1111" {
			deep = &items[i]
			break
		}
	}
	if deep == nil {
		t.Fatal("missing AAAA1111")
	}
	if deep.Title != "Deep Learning for Neuroimaging" {
		t.Errorf("title = %q", deep.Title)
	}
	if len(deep.Creators) != 2 {
		t.Errorf("creators len = %d, want 2", len(deep.Creators))
	}
	if deep.Fields["publicationTitle"] != "NeuroImage" {
		t.Errorf("fields[publicationTitle] = %q", deep.Fields["publicationTitle"])
	}
	// DOI, URL, abstract should all be present on the fully-hydrated item.
	if deep.DOI == "" || deep.URL == "" || deep.Abstract == "" {
		t.Errorf("missing denormalized fields: doi=%q url=%q abstract=%q",
			deep.DOI, deep.URL, deep.Abstract)
	}
}

func TestListAll_PopulatesVersionFromDB(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.ListAll(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// Every content item in the fixture has a non-zero version; the
	// field must be populated so callers (e.g. UpdateItemsBatch) can
	// skip the per-item GET that fetches the version from the API.
	for _, it := range items {
		if it.Version == 0 {
			t.Errorf("item %s Version = 0, want non-zero", it.Key)
		}
	}
}

func TestListAll_RespectsCollectionFilter(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Collection COLLAAA1 ("Brain Papers") contains items 10 and 20.
	items, err := db.ListAll(ListFilter{CollectionKey: "COLLAAA1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("len = %d, want 2", len(items))
	}
}

func TestListAll_FilterByKeys(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.ListAll(ListFilter{Keys: []string{"AAAA1111", "GGGG7777"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(items), keysOf(items))
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Key] = true
	}
	if !got["AAAA1111"] || !got["GGGG7777"] {
		t.Errorf("keys filter = %v, want AAAA1111+GGGG7777", keysOf(items))
	}
	// Full hydration must still apply on the keys path.
	for _, it := range items {
		if it.Key == "AAAA1111" && len(it.Creators) != 2 {
			t.Errorf("AAAA1111 creators = %d, want 2", len(it.Creators))
		}
	}
}

func TestListAllKeepsParentlessAttachmentsAndNotes(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.ListAll(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Key] = true
	}

	// A parentless attachment has nowhere to ride. Child attachments are
	// nested inside their parent's Attachments array, so they reach a
	// consumer either way -- but a PDF with no parent item is simply
	// absent from the mirror, and the only thing in the toolkit that can
	// see it is `doctor orphans`. On the live library that is 51 real
	// papers, and DESIGN.md's claim that the database still builds if sci
	// vanishes quietly means "builds with a hole in it".
	for _, key := range []string{"ORPHANATT", "ORPHNNOTE"} {
		if !got[key] {
			t.Errorf("%s is absent from the mirror", key)
		}
	}
	// Child attachments and notes still must NOT appear at top level:
	// they already ride nested, and emitting them twice would make every
	// count of the mirror disagree with itself.
	for _, key := range []string{"DDDD4444", "NOTECH10", "NOTECH11"} {
		if got[key] {
			t.Errorf("%s appears at top level as well as nested", key)
		}
	}
}
