package local

import (
	"testing"
)

// Fixture state relevant to orient queries (libraryID=1, content items 10/20/30/80):
//   - tag itemTags rows (excluding child items 90):
//       neuroimaging  -> item 10
//       deep-learning -> item 10
//       cats          -> item 30
//       has-markdown  -> item 10
//   - collection items:
//       Brain Papers (COLLAAA1)  -> 10, 20  (2 items)
//       Favorites    (COLLBBB2)  -> 10      (1 item)
//       Empty Box    (EMPTYCOL)  -> 0
//   - dateAdded (descending): 80 (2024-06-01), 30 (2024-03-01), 20 (2024-02-01), 10 (2024-01-01)

func TestTopTags_FiltersHasMarkdown(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.TopTags(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range got {
		if tg.Name == HasMarkdownTag {
			t.Errorf("TopTags should exclude %q (capability signal, surfaced via ExtractionCoverage)", HasMarkdownTag)
		}
	}
	// 4 user tags remain after filtering: cats, deep-learning, docling, neuroimaging.
	if len(got) != 4 {
		t.Errorf("len = %d, want 4 (excluding has-markdown): %+v", len(got), got)
	}
}

func TestTopTags_OrderedByCountDesc_ThenName(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.TopTags(10)
	if err != nil {
		t.Fatal(err)
	}
	// All have count 1; ties → name asc (case-insensitive).
	want := []string{"cats", "deep-learning", "docling", "neuroimaging"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("[%d] = %q, want %q (full: %+v)", i, got[i].Name, w, got)
		}
		if got[i].Count != 1 {
			t.Errorf("[%d] %q count = %d, want 1", i, got[i].Name, got[i].Count)
		}
	}
}

func TestTopTags_LimitRespected(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.TopTags(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (LIMIT)", len(got))
	}
}

func TestTopTags_ZeroLimit_ReturnsNil(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.TopTags(0)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestTopCollections_OrderedByCountDesc(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.TopCollections(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Key != "COLLAAA1" || got[0].Count != 2 {
		t.Errorf("[0] = %+v, want Brain Papers (2)", got[0])
	}
	if got[1].Key != "COLLBBB2" || got[1].Count != 1 {
		t.Errorf("[1] = %+v, want Favorites (1)", got[1])
	}
	if got[2].Key != "EMPTYCOL" || got[2].Count != 0 {
		t.Errorf("[2] = %+v, want Empty Box (0)", got[2])
	}
}

func TestRecentlyAdded_OrderedByDateDesc(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.RecentlyAdded(10)
	if err != nil {
		t.Fatal(err)
	}
	// Content items only (excludes notes/attachments/trashed).
	// dateAdded order: 80 (2024-06-01), 30 (2024-03-01), 20 (2024-02-01), 10 (2024-01-01).
	wantOrder := []string{"GGGG7777", "CCCC3333", "BBBB2222", "AAAA1111"}
	if len(got) != len(wantOrder) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(wantOrder), got)
	}
	for i, k := range wantOrder {
		if got[i].Key != k {
			t.Errorf("[%d] key = %q, want %q", i, got[i].Key, k)
		}
	}
}

func TestRecentlyAdded_TruncatesTimestamp(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.RecentlyAdded(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].DateAdded != "2024-06-01" {
		t.Errorf("DateAdded = %q, want 2024-06-01 (truncated)", got[0].DateAdded)
	}
}

func TestRecentlyAdded_ExcludesAttachmentsAndNotes(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.RecentlyAdded(20)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		switch r.Key {
		case "DDDD4444", "ORPHANATT", "HHHH8888": // attachments
			t.Errorf("attachment %q should not appear in recent items", r.Key)
		case "ORPHNNOTE", "NOTECH10": // notes
			t.Errorf("note %q should not appear in recent items", r.Key)
		case "EEEE5555": // trashed
			t.Errorf("trashed item %q should not appear", r.Key)
		}
	}
}

func TestExtractionCoverage_CountsHasMarkdownTag(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	cov, err := db.ExtractionCoverage()
	if err != nil {
		t.Fatal(err)
	}
	// Item 10 carries has-markdown; total content items = 4 (10, 20, 30, 80).
	if cov.WithExtraction != 1 {
		t.Errorf("WithExtraction = %d, want 1", cov.WithExtraction)
	}
	if cov.TotalItems != 4 {
		t.Errorf("TotalItems = %d, want 4", cov.TotalItems)
	}
	// 1/4 = 25% exactly.
	if cov.Percent != 25.0 {
		t.Errorf("Percent = %.1f, want 25.0", cov.Percent)
	}
}
