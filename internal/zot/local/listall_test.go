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
	// 5 rows: 4 content items plus the annotation. ListAll is the mirror
	// the NDJSON export is built from, so it keeps annotations even though
	// no listing shows them -- an annotation is a real Zotero object, and
	// "staging is the durable state" is only true if staging holds it.
	// Attachments, notes, and trashed
	// items must be excluded just like List().
	if len(items) != 5 {
		t.Fatalf("len = %d, want 5", len(items))
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
