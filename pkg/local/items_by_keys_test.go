package local

// Tests for GetItemsByKeys — the narrow bulk-fetch used by batch write paths
// (e.g. `zot collection add --from-file`) to populate Version + ItemType +
// Collections in one SQL round-trip so UpdateItemsBatch can skip per-item GETs.

import (
	"slices"
	"testing"
)

func TestGetItemsByKeys_ReturnsCoreFieldsAndCollections(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	items, err := db.GetItemsByKeys([]string{"AAAA1111", "BBBB2222"})
	if err != nil {
		t.Fatalf("GetItemsByKeys: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(items), items)
	}

	by := map[string]Item{}
	for _, it := range items {
		by[it.Key] = it
	}

	aaaa, ok := by["AAAA1111"]
	if !ok {
		t.Fatal("missing AAAA1111")
	}
	if aaaa.Version != 42 {
		t.Errorf("AAAA1111 Version = %d, want 42", aaaa.Version)
	}
	if aaaa.Type != "journalArticle" {
		t.Errorf("AAAA1111 Type = %q, want journalArticle", aaaa.Type)
	}
	sorted := slices.Clone(aaaa.Collections)
	slices.Sort(sorted)
	if !slices.Equal(sorted, []string{"COLLAAA1", "COLLBBB2"}) {
		t.Errorf("AAAA1111 Collections = %v, want [COLLAAA1 COLLBBB2]", aaaa.Collections)
	}

	bbbb := by["BBBB2222"]
	if bbbb.Version != 15 || bbbb.Type != "journalArticle" {
		t.Errorf("BBBB2222 Version/Type = %d/%q, want 15/journalArticle", bbbb.Version, bbbb.Type)
	}
	if !slices.Equal(bbbb.Collections, []string{"COLLAAA1"}) {
		t.Errorf("BBBB2222 Collections = %v, want [COLLAAA1]", bbbb.Collections)
	}
}

func TestGetItemsByKeys_ItemWithNoCollections(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	items, err := db.GetItemsByKeys([]string{"GGGG7777"})
	if err != nil {
		t.Fatalf("GetItemsByKeys: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if len(items[0].Collections) != 0 {
		t.Errorf("Collections = %v, want empty", items[0].Collections)
	}
}

func TestGetItemsByKeys_UnknownKeysSilentlyOmitted(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	items, err := db.GetItemsByKeys([]string{"AAAA1111", "NOTREAL1", "NOTREAL2"})
	if err != nil {
		t.Fatalf("GetItemsByKeys: %v", err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("got %+v, want exactly AAAA1111", items)
	}
}

func TestGetItemsByKeys_EmptyInput(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	items, err := db.GetItemsByKeys(nil)
	if err != nil {
		t.Fatalf("GetItemsByKeys(nil): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}

	items, err = db.GetItemsByKeys([]string{})
	if err != nil {
		t.Fatalf("GetItemsByKeys([]): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}
}

func TestGetItemsByKeys_ExcludesTrashed(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// EEEE5555 is in deletedItems; must not be returned.
	items, err := db.GetItemsByKeys([]string{"EEEE5555"})
	if err != nil {
		t.Fatalf("GetItemsByKeys: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("trashed item leaked: %+v", items)
	}
}

func TestGetItemsByKeys_ScopedToLibrary(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)

	// Personal scope: GRPITEM01 belongs to the group library, so it
	// must NOT be returned even when explicitly requested.
	dbUser, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("Open(ForPersonal): %v", err)
	}
	defer func() { _ = dbUser.Close() }()

	items, err := dbUser.GetItemsByKeys([]string{"AAAA1111", "GRPITEM01"})
	if err != nil {
		t.Fatalf("GetItemsByKeys(personal): %v", err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("personal got %+v, want only AAAA1111", items)
	}

	// Group scope: opposite — GRPITEM01 is visible, AAAA1111 is not.
	dbGroup, err := Open(dir, ForGroup(2))
	if err != nil {
		t.Fatalf("Open(ForGroup(2)): %v", err)
	}
	defer func() { _ = dbGroup.Close() }()

	gItems, err := dbGroup.GetItemsByKeys([]string{"AAAA1111", "GRPITEM01"})
	if err != nil {
		t.Fatalf("GetItemsByKeys(group): %v", err)
	}
	if len(gItems) != 1 || gItems[0].Key != "GRPITEM01" {
		t.Errorf("group got %+v, want only GRPITEM01", gItems)
	}
	if gItems[0].Version == 0 || gItems[0].Type == "" {
		t.Errorf("group item missing Version/Type: %+v", gItems[0])
	}
}
