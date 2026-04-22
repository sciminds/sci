package local

import (
	"strings"
	"testing"
)

func openFixture(t *testing.T) *DB {
	t.Helper()
	dir := buildFixture(t)
	sanityCheckFixture(t, dir)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen_Meta(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// Items 10, 20, 30, 80 are content in the user library.
	// Item 40 is an attachment (excluded by type).
	// Item 50 is trashed (excluded by deletedItems).
	if len(items) != 4 {
		t.Errorf("len = %d, want 4: %+v", len(items), items)
	}
	keys := map[string]bool{}
	for _, it := range items {
		keys[it.Key] = true
	}
	for _, want := range []string{"AAAA1111", "BBBB2222", "CCCC3333", "GGGG7777"} {
		if !keys[want] {
			t.Errorf("missing key %s", want)
		}
	}
}

func TestList_OrderDateAddedDesc(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{OrderBy: OrderDateAddedDesc})
	if err != nil {
		t.Fatal(err)
	}
	// dateAdded order desc: GGGG7777 (jun), CCCC3333 (mar), BBBB2222 (feb), AAAA1111 (jan).
	if items[0].Key != "GGGG7777" || items[3].Key != "AAAA1111" {
		t.Errorf("order wrong: %v", keysOf(items))
	}
}

func TestList_FilterByType(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{ItemType: "book"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "CCCC3333" {
		t.Errorf("type=book = %v", keysOf(items))
	}
}

// TestList_FilterByType_Note guards against the bug where the hard-coded
// "exclude notes and attachments from listings" rule silently overrode an
// explicit --type note filter. Without the opt-out, asking the local DB
// for `ItemType:"note"` returned zero rows because the unconditional
// contentItemTypeFilter won.
func TestList_FilterByType_Note(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{ItemType: "note"})
	if err != nil {
		t.Fatal(err)
	}
	// Fixture has one standalone note (key ORPHNNOTE) and one child note
	// (key NOTECH10). Both should surface when the user asks for notes.
	got := keysOf(items)
	wantSet := map[string]bool{"ORPHNNOTE": true, "NOTECH10": true}
	for _, k := range got {
		if !wantSet[k] {
			t.Errorf("unexpected key in type=note results: %q", k)
		}
	}
	for k := range wantSet {
		found := false
		for _, g := range got {
			if g == k {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected note %q; got %v", k, got)
		}
	}
}

// TestList_FilterByType_Attachment: same logic as the note case but for
// attachments (the other type excluded by contentItemTypeFilter).
func TestList_FilterByType_Attachment(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{ItemType: "attachment"})
	if err != nil {
		t.Fatal(err)
	}
	// Fixture has one child attachment (DDDD4444) and one standalone (ORPHANATT).
	got := keysOf(items)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 attachments, got %v", got)
	}
}

// TestList_DefaultExcludesNotesAndAttachments pins the default behaviour —
// without an explicit --type, notes/attachments must still be filtered out
// of the listing (they're children of "real" items; surfacing them by
// default would spam the top-level view).
func TestList_DefaultExcludesNotesAndAttachments(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Type == "note" || it.Type == "attachment" {
			t.Errorf("default listing surfaced a %s (key %s) — the hard-coded exclusion must still apply when no --type is set", it.Type, it.Key)
		}
	}
}

func TestList_FilterByCollection(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("abc123", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("doi search = %v", keysOf(items))
	}
}

func TestSearch_CreatorMatch(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// "Smith" authors items 10 (AAAA1111) and 30 (CCCC3333).
	items, err := db.Search("smith", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Key] = true
	}
	if !got["AAAA1111"] || !got["CCCC3333"] || len(items) != 2 {
		t.Errorf("creator search = %v, want AAAA1111+CCCC3333", keysOf(items))
	}
}

func TestSearch_SingleNameCreatorMatch(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Single-name creator (fieldMode=1) "NASA" on BBBB2222.
	items, err := db.Search("nasa", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "BBBB2222" {
		t.Errorf("single-name search = %v, want BBBB2222", keysOf(items))
	}
}

func TestSearch_Smartcase(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// All-lowercase query → case-insensitive: matches "Neuroimaging" in title.
	items, err := db.Search("neuroimaging", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("lowercase smartcase = %v, want AAAA1111", keysOf(items))
	}
	// Mixed-case query → case-sensitive: "Smith" exists, matches.
	items, err = db.Search("Smith", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("mixed-case smartcase = %v, want 2 hits", keysOf(items))
	}
	// All-uppercase query → case-sensitive: no "SMITH" in fixture.
	items, err = db.Search("SMITH", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("uppercase smartcase = %v, want 0", keysOf(items))
	}
}

func TestSearch_TitleScope(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("@title: neuroimaging", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("title scope = %v", keysOf(items))
	}
	// "nasa" is in creators, not titles — title scope must NOT match.
	items, err = db.Search("@title: nasa", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("title scope leaked into creators: %v", keysOf(items))
	}
}

func TestSearch_AuthorScope(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("@author: smith", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("author scope = %v, want 2", keysOf(items))
	}
}

func TestSearch_TagScope(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("@tag: neuroimaging", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("tag scope = %v", keysOf(items))
	}
}

func TestSearch_TypeScope(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("@type: book", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "CCCC3333" {
		t.Errorf("type scope = %v", keysOf(items))
	}
}

func TestSearch_YearScope(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// AAAA1111 (date 2024-03-15) + BBBB2222 (also points at value 4 = 2024).
	items, err := db.Search("@year: 2024", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("year=2024 = %v, want 2", keysOf(items))
	}
	items, err = db.Search("@year: 2023", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("year=2023 = %v, want 2 (CCCC3333 + GGGG7777)", keysOf(items))
	}
}

func TestSearch_AndClauses(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Smith authors AAAA1111 + CCCC3333; only AAAA1111 has "neuroimaging".
	items, err := db.Search("@author: smith @title: neuroimaging", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("AND scope = %v", keysOf(items))
	}
	// Comma form should produce the same result.
	items, err = db.Search("@author: smith, @title: neuroimaging", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("AND comma form = %v", keysOf(items))
	}
}

func TestSearch_OrGroups(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// smith → AAAA1111+CCCC3333; nasa → BBBB2222 → all three items.
	items, err := db.Search("@author: smith | @author: nasa", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Errorf("OR groups = %v, want 3", keysOf(items))
	}
}

func TestSearch_Negate(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Exclude smith → BBBB2222 (NASA) + GGGG7777 (no creators) remain.
	items, err := db.Search("@author: -smith", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("negate = %v, want 2", keysOf(items))
	}
}

func TestSearch_UnknownField(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	if _, err := db.Search("@foo: bar", 10); err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestSearch_NoResults(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestRead_PopulatesExtra(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Item CCCC3333 (book) has the only Extra value in the fixture:
	// "tldr: loose note\nCitation Key: legacyBookKey1900\n". Read should
	// surface it as a typed field, not just leave it buried in Fields.
	it, err := db.Read("CCCC3333")
	if err != nil {
		t.Fatal(err)
	}
	if it.Extra == "" {
		t.Fatalf("Extra empty, want raw extra value; Fields[extra] = %q", it.Fields["extra"])
	}
	if !strings.Contains(it.Extra, "Citation Key: legacyBookKey1900") {
		t.Errorf("Extra = %q, want Citation Key line", it.Extra)
	}
}

func TestRead_NoExtraStaysEmpty(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// AAAA1111 has no extra row in itemData → Extra should be "".
	it, err := db.Read("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if it.Extra != "" {
		t.Errorf("Extra = %q, want empty", it.Extra)
	}
}

func TestRead_NotFound(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	if _, err := db.Read("NOSUCHKEY"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestRead_TrashedExcluded(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	if _, err := db.Read("EEEE5555"); err == nil {
		t.Error("expected trashed item to be invisible to Read")
	}
}

func TestStats(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	s, err := db.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalItems != 4 {
		t.Errorf("TotalItems = %d, want 4", s.TotalItems)
	}
	if s.ByType["journalArticle"] != 3 || s.ByType["book"] != 1 {
		t.Errorf("ByType = %+v", s.ByType)
	}
	if s.WithDOI != 1 {
		t.Errorf("WithDOI = %d, want 1", s.WithDOI)
	}
	if s.WithAbstract != 1 {
		t.Errorf("WithAbstract = %d, want 1", s.WithAbstract)
	}
	if s.WithAttachment != 2 {
		t.Errorf("WithAttachment = %d, want 2", s.WithAttachment)
	}
	if s.Collections != 3 {
		t.Errorf("Collections = %d, want 3", s.Collections)
	}
	if s.Tags != 4 {
		t.Errorf("Tags = %d, want 4", s.Tags)
	}
}

func TestListCollections(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	cs, err := db.ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
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
	empty := byKey["EMPTYCOL"]
	if empty.Name != "Empty Box" || empty.ItemCount != 0 {
		t.Errorf("Empty Box: %+v", empty)
	}
}

func TestItemKeysByDOI(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Fixture: AAAA1111 has DOI "10.1000/abc123" — only item with a DOI.
	// Hit case-insensitively against the stored value, plus a miss.
	hits, err := db.ItemKeysByDOI([]string{
		"10.1000/ABC123",
		"10.0000/missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := hits["10.1000/abc123"]; got != "AAAA1111" {
		t.Errorf("hit = %q (lower-cased key), want AAAA1111", got)
	}
	if _, ok := hits["10.0000/missing"]; ok {
		t.Errorf("missing DOI should not appear in result map")
	}
	if len(hits) != 1 {
		t.Errorf("len = %d, want 1 (%v)", len(hits), hits)
	}
}

func TestItemKeysByDOI_Empty(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	hits, err := db.ItemKeysByDOI(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("empty input should yield empty map, got %v", hits)
	}
}

func TestCollectionByKey(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	c, err := db.CollectionByKey("COLLAAA1")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Name != "Brain Papers" || c.ItemCount != 2 {
		t.Errorf("hit lookup = %+v", c)
	}
	c, err = db.CollectionByKey("COLLBBB2")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.ParentKey != "COLLAAA1" {
		t.Errorf("nested lookup = %+v", c)
	}
	c, err = db.CollectionByKey("NOSUCHCO")
	if err != nil {
		t.Fatalf("miss should be nil/nil, got err=%v", err)
	}
	if c != nil {
		t.Errorf("miss returned %+v, want nil", c)
	}
}

func TestListTags(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	tags, err := db.ListTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 4 {
		t.Fatalf("len = %d", len(tags))
	}
	// Sorted by count desc — all have count 1, so then name asc.
	// cats (1), deep-learning (1), docling (1), neuroimaging (1).
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
