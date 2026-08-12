package local

// Tests for search-row enrichment: Search results carry Creators, Tags,
// and URL so downstream consumers (zen's result rows) render authorship
// without an N+1 `item read` per hit. Enrichment runs on the ≤limit
// ranked hits only — three batched IN queries, not per-item reads — so
// broad queries stay cheap. Abstract/Fields/attachments remain `item
// read` / `search --full` territory.

import (
	"slices"
	"testing"
)

func TestSearch_RowsCarryCreators(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Item 10 (AAAA1111) has two creators in the fixture, ordered.
	items, err := db.Search("@citekey: smith2024", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 hit, got %v", keysOf(items))
	}
	if len(items[0].Creators) != 2 {
		t.Fatalf("Creators = %+v, want the item's 2 creators in order", items[0].Creators)
	}
	if items[0].Creators[0].OrderIdx != 0 || items[0].Creators[1].OrderIdx != 1 {
		t.Errorf("creators out of order: %+v", items[0].Creators)
	}
}

func TestSearch_RowsCarryTags(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("@citekey: smith2024", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 hit, got %v", keysOf(items))
	}
	// Item 10 carries neuroimaging + deep-learning + has-markdown,
	// name-sorted like Read's itemTags.
	for _, want := range []string{"deep-learning", "has-markdown", "neuroimaging"} {
		if !slices.Contains(items[0].Tags, want) {
			t.Errorf("Tags = %v, missing %q", items[0].Tags, want)
		}
	}
	if !slices.IsSorted(items[0].Tags) {
		t.Errorf("Tags should be name-sorted to match Read: %v", items[0].Tags)
	}
}

func TestSearch_RowsCarryURL(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("@citekey: smith2024", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 hit, got %v", keysOf(items))
	}
	if items[0].URL != "https://example.org/abc" {
		t.Errorf("URL = %q, want the item's stored url", items[0].URL)
	}
}

func TestSearch_RowsWithoutExtrasStayLean(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// GGGG7777 has no creators, tags, or url — enrichment must leave the
	// zero values (omitempty keeps the JSON shape identical to before).
	items, err := db.Search("@citekey: gggg7777", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 hit, got %v", keysOf(items))
	}
	it := items[0]
	if len(it.Creators) != 0 || len(it.Tags) != 0 || it.URL != "" {
		t.Errorf("enrichment invented data: creators=%+v tags=%v url=%q", it.Creators, it.Tags, it.URL)
	}
	// Abstract and the full Fields map stay Read-only territory.
	if it.Abstract != "" || it.Fields != nil {
		t.Errorf("search rows must not haul abstract/fields: %+v", it)
	}
}
