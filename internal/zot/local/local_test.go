package local

import (
	"testing"
)

func openFixture(t *testing.T) *DB {
	t.Helper()
	dir := buildFixture(t)
	sanityCheckFixture(t, dir)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen_Meta(t *testing.T) {
	db := openFixture(t)
	if db.LibraryID() != 1 {
		t.Errorf("LibraryID = %d, want 1", db.LibraryID())
	}
	if db.SchemaVersion() != 125 {
		t.Errorf("SchemaVersion = %d, want 125", db.SchemaVersion())
	}
	if db.SchemaOutOfRange() {
		t.Error("SchemaOutOfRange = true for 125")
	}
}

func TestList_ExcludesTrashedAndAttachments(t *testing.T) {
	db := openFixture(t)
	items, err := db.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// Items 10, 20, 30 are content in the user library.
	// Item 40 is an attachment (excluded by type).
	// Item 50 is trashed (excluded by deletedItems).
	if len(items) != 3 {
		t.Errorf("len = %d, want 3: %+v", len(items), items)
	}
	keys := map[string]bool{}
	for _, it := range items {
		keys[it.Key] = true
	}
	for _, want := range []string{"AAAA1111", "BBBB2222", "CCCC3333"} {
		if !keys[want] {
			t.Errorf("missing key %s", want)
		}
	}
}

func TestList_OrderDateAddedDesc(t *testing.T) {
	db := openFixture(t)
	items, err := db.List(ListFilter{OrderBy: OrderDateAddedDesc})
	if err != nil {
		t.Fatal(err)
	}
	// dateAdded order desc: CCCC3333 (mar), BBBB2222 (feb), AAAA1111 (jan).
	if items[0].Key != "CCCC3333" || items[2].Key != "AAAA1111" {
		t.Errorf("order wrong: %v", keysOf(items))
	}
}

func TestList_FilterByType(t *testing.T) {
	db := openFixture(t)
	items, err := db.List(ListFilter{ItemType: "book"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "CCCC3333" {
		t.Errorf("type=book = %v", keysOf(items))
	}
}

func TestList_FilterByCollection(t *testing.T) {
	db := openFixture(t)
	items, err := db.List(ListFilter{CollectionKey: "COLLAAA1"})
	if err != nil {
		t.Fatal(err)
	}
	// Collection 100 contains items 10 and 20.
	if len(items) != 2 {
		t.Errorf("collection items = %v", keysOf(items))
	}
}

func TestList_FilterByTag(t *testing.T) {
	db := openFixture(t)
	items, err := db.List(ListFilter{Tag: "neuroimaging"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("tag=neuroimaging = %v", keysOf(items))
	}
}

func TestSearch_TitleMatch(t *testing.T) {
	db := openFixture(t)
	items, err := db.Search("neuroimaging", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("search = %v", keysOf(items))
	}
}

func TestSearch_DOIMatch(t *testing.T) {
	db := openFixture(t)
	items, err := db.Search("abc123", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("doi search = %v", keysOf(items))
	}
}

func TestSearch_NoResults(t *testing.T) {
	db := openFixture(t)
	items, err := db.Search("nonexistent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}
}

func TestRead_FullHydration(t *testing.T) {
	db := openFixture(t)
	it, err := db.Read("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if it.Type != "journalArticle" {
		t.Errorf("Type = %q", it.Type)
	}
	if it.Title != "Deep Learning for Neuroimaging" {
		t.Errorf("Title = %q", it.Title)
	}
	if it.DOI != "10.1000/abc123" {
		t.Errorf("DOI = %q", it.DOI)
	}
	if it.Publication != "NeuroImage" {
		t.Errorf("Publication = %q", it.Publication)
	}
	if it.URL != "https://example.org/abc" {
		t.Errorf("URL = %q", it.URL)
	}
	if it.Abstract != "Abstract about brains." {
		t.Errorf("Abstract = %q", it.Abstract)
	}
	// Zotero stores dates as "YYYY-MM-DD originalText" — verify the raw
	// form passes through local/ unchanged. Display-layer trimming lives
	// in zot.cleanDate (readresult.go).
	if it.Date != "2024-03-15 March 15, 2024" {
		t.Errorf("Date = %q, want raw Zotero dual-encoding", it.Date)
	}
	if len(it.Creators) != 2 {
		t.Fatalf("creators = %v", it.Creators)
	}
	if it.Creators[0].Last != "Smith" || it.Creators[1].Last != "Jones" {
		t.Errorf("creator order: %+v", it.Creators)
	}
	if len(it.Tags) != 2 {
		t.Errorf("tags = %v", it.Tags)
	}
	if len(it.Collections) != 2 {
		t.Errorf("collections = %v", it.Collections)
	}
	if len(it.Attachments) != 1 {
		t.Fatalf("attachments = %v", it.Attachments)
	}
	att := it.Attachments[0]
	if att.Filename != "deeplearning.pdf" || att.ContentType != "application/pdf" {
		t.Errorf("attachment = %+v", att)
	}
}

func TestRead_SingleNameCreator(t *testing.T) {
	db := openFixture(t)
	it, err := db.Read("BBBB2222")
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Creators) != 1 {
		t.Fatalf("creators = %v", it.Creators)
	}
	if it.Creators[0].Name != "NASA" || it.Creators[0].First != "" || it.Creators[0].Last != "" {
		t.Errorf("single-name creator not detected: %+v", it.Creators[0])
	}
}

func TestRead_NotFound(t *testing.T) {
	db := openFixture(t)
	if _, err := db.Read("NOSUCHKEY"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestRead_TrashedExcluded(t *testing.T) {
	db := openFixture(t)
	if _, err := db.Read("EEEE5555"); err == nil {
		t.Error("expected trashed item to be invisible to Read")
	}
}

func TestStats(t *testing.T) {
	db := openFixture(t)
	s, err := db.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalItems != 3 {
		t.Errorf("TotalItems = %d, want 3", s.TotalItems)
	}
	if s.ByType["journalArticle"] != 2 || s.ByType["book"] != 1 {
		t.Errorf("ByType = %+v", s.ByType)
	}
	if s.WithDOI != 1 {
		t.Errorf("WithDOI = %d, want 1", s.WithDOI)
	}
	if s.WithAbstract != 1 {
		t.Errorf("WithAbstract = %d, want 1", s.WithAbstract)
	}
	if s.WithAttachment != 1 {
		t.Errorf("WithAttachment = %d, want 1", s.WithAttachment)
	}
	if s.Collections != 2 {
		t.Errorf("Collections = %d, want 2", s.Collections)
	}
	if s.Tags != 3 {
		t.Errorf("Tags = %d, want 3", s.Tags)
	}
}

func TestListCollections(t *testing.T) {
	db := openFixture(t)
	cs, err := db.ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("len = %d", len(cs))
	}
	byKey := map[string]Collection{}
	for _, c := range cs {
		byKey[c.Key] = c
	}
	bp := byKey["COLLAAA1"]
	if bp.Name != "Brain Papers" || bp.ItemCount != 2 || bp.ParentKey != "" {
		t.Errorf("Brain Papers: %+v", bp)
	}
	fav := byKey["COLLBBB2"]
	if fav.ParentKey != "COLLAAA1" || fav.ItemCount != 1 {
		t.Errorf("Favorites: %+v", fav)
	}
}

func TestListTags(t *testing.T) {
	db := openFixture(t)
	tags, err := db.ListTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("len = %d", len(tags))
	}
	// Sorted by count desc — all have count 1, so then name asc.
	// cats (1), deep-learning (1), neuroimaging (1) — but the collation is
	// NOCASE so order is cats, deep-learning, neuroimaging.
	if tags[0].Name != "cats" {
		t.Errorf("first tag = %q, want cats", tags[0].Name)
	}
}

func keysOf(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Key
	}
	return out
}
