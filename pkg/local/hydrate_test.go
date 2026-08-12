package local

// Tests for the set-based hydration helpers. The property that matters is
// equivalence with Read's per-item path plus correctness under ForAll,
// where the per-item queries were never scoped at all.

import (
	"slices"
	"testing"
)

func TestTagsByItem_MatchesReadPerItem(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	byItem, err := db.TagsByItem()
	if err != nil {
		t.Fatal(err)
	}
	// AAAA1111 (itemID 10) carries neuroimaging + deep-learning +
	// has-markdown; CCCC3333 (30) carries cats.
	got := byItem["AAAA1111"]
	for _, want := range []string{"neuroimaging", "deep-learning", "has-markdown"} {
		if !slices.Contains(got, want) {
			t.Errorf("AAAA1111 tags = %v, missing %q", got, want)
		}
	}
	if got := byItem["CCCC3333"]; !slices.Equal(got, []string{"cats"}) {
		t.Errorf("CCCC3333 tags = %v, want [cats]", got)
	}
	// An untagged item must be absent, not present-and-empty — the
	// consumer distinguishes "no tags" from "not scanned".
	if _, ok := byItem["BBBB2222"]; ok {
		t.Errorf("BBBB2222 has no tags but appears in the map: %v", byItem["BBBB2222"])
	}

	// Equivalence with the per-item path Read uses.
	it, err := db.Read("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(it.Tags, byItem["AAAA1111"]) {
		t.Errorf("bulk tags %v != Read tags %v", byItem["AAAA1111"], it.Tags)
	}
}

func TestCollectionsByItem_MatchesReadPerItem(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	byItem, err := db.CollectionsByItem()
	if err != nil {
		t.Fatal(err)
	}
	// collectionItems seeds (100,10),(100,20),(101,10): AAAA1111 is in
	// both Brain Papers and Favorites.
	got := byItem["AAAA1111"]
	for _, want := range []string{"COLLAAA1", "COLLBBB2"} {
		if !slices.Contains(got, want) {
			t.Errorf("AAAA1111 collections = %v, missing %q", got, want)
		}
	}

	it, err := db.Read("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Collections) != len(got) {
		t.Errorf("bulk collections %v != Read collections %v", got, it.Collections)
	}
}

func TestAttachmentsByItem_KeyedByParentWithParentKeySet(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	byItem, err := db.AttachmentsByItem()
	if err != nil {
		t.Fatal(err)
	}
	atts := byItem["AAAA1111"]
	if len(atts) != 1 {
		t.Fatalf("AAAA1111 attachments = %d, want 1", len(atts))
	}
	if atts[0].Filename != "deeplearning.pdf" {
		t.Errorf("filename = %q, want deeplearning.pdf (storage: prefix stripped)", atts[0].Filename)
	}
	// ParentKey must survive being lifted out of the map — a bare
	// Attachment with no parent is unjoinable downstream.
	if atts[0].ParentKey != "AAAA1111" {
		t.Errorf("ParentKey = %q, want AAAA1111", atts[0].ParentKey)
	}
	// The standalone attachment (parentItemID NULL) has no parent, so it
	// must not appear under any key.
	for parent, list := range byItem {
		for _, a := range list {
			if a.Filename == "standalone.pdf" {
				t.Errorf("standalone attachment surfaced under parent %q", parent)
			}
		}
	}
}

func TestHydrateAll_FillsWhatListAllLeavesEmpty(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	items, err := db.ListAll(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// The precondition this whole file exists for: ListAll returns bare
	// rows. If that ever changes, HydrateAll becomes redundant and this
	// test is the place that says so.
	for _, it := range items {
		if len(it.Tags) > 0 || len(it.Collections) > 0 || len(it.Attachments) > 0 {
			t.Fatalf("ListAll unexpectedly hydrated %s — HydrateAll may be redundant now", it.Key)
		}
	}

	if err := db.HydrateAll(items); err != nil {
		t.Fatal(err)
	}
	var found *Item
	for i := range items {
		if items[i].Key == "AAAA1111" {
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatal("AAAA1111 not in ListAll results")
	}
	if len(found.Tags) == 0 {
		t.Error("HydrateAll left Tags empty")
	}
	if len(found.Collections) == 0 {
		t.Error("HydrateAll left Collections empty")
	}
	if len(found.Attachments) == 0 {
		t.Error("HydrateAll left Attachments empty")
	}
}

func TestHydrateAll_ScopedUnderForAll(t *testing.T) {
	t.Parallel()
	db := openFixtureAll(t)

	atts, err := db.AttachmentsByItem()
	if err != nil {
		t.Fatal(err)
	}
	// The group item's attachment is the point: the per-item query these
	// replace bound no libraryID at all, and a personal-only scope would
	// have dropped it.
	if got := atts["GRPITEM01"]; len(got) != 1 || got[0].Filename != "shared.pdf" {
		t.Errorf("GRPITEM01 attachments = %+v, want one shared.pdf", got)
	}
	if got := atts["AAAA1111"]; len(got) != 1 {
		t.Errorf("personal item lost its attachment under ForAll: %+v", got)
	}
}
