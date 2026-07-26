package local

import (
	"slices"
	"testing"
)

// --- @citekey: field scope ---

func TestSearch_CitekeyScope(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Item 10 carries native citationKey "smith2024-deeplearneur-AAAA1111".
	items, err := db.Search("@citekey: smith2024", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("citekey search = %v, want AAAA1111", keysOf(items))
	}
}

func TestSearch_CitekeyScope_ZoteroKey(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// The 8-char Zotero key is part of every synthesized cite-key, so
	// @citekey: also matches against the item key itself (smartcase fold).
	items, err := db.Search("@citekey: gggg7777", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "GGGG7777" {
		t.Errorf("citekey key search = %v, want GGGG7777", keysOf(items))
	}
}

func TestSearch_CitekeyScope_FullSynthesizedKey(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Item 80 has no stored citationKey; its synthesized key ends in the
	// Zotero key. Pasting a whole synthesized key must resolve via the
	// -ZOTKEY suffix even though nothing is stored in the DB.
	items, err := db.Search("@citekey: anon2023-attemechcort-GGGG7777", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "GGGG7777" {
		t.Errorf("full synthesized key search = %v, want GGGG7777", keysOf(items))
	}
}

func TestSearch_CitekeyScope_Negate(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Negated citekey must not swallow items whose citationKey is NULL
	// (items 30 and 80 have no native citationKey row).
	items, err := db.Search("@citekey: -smith2024", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(items)
	if slices.Contains(got, "AAAA1111") {
		t.Errorf("negated citekey still matched AAAA1111: %v", got)
	}
	for _, want := range []string{"BBBB2222", "CCCC3333", "GGGG7777"} {
		if !slices.Contains(got, want) {
			t.Errorf("negated citekey dropped %s: %v", want, got)
		}
	}
}

func TestSearch_BareTermMatchesCitekey(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// "deeplearneur" appears only inside item 10's stored citationKey.
	items, err := db.Search("deeplearneur", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("bare citekey search = %v, want AAAA1111", keysOf(items))
	}
}

// --- relevance ranking ---

func TestSearch_RankYearTiebreak(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// "smith" matches items 10 (2024) and 30 (2023) via creators; neither
	// title contains the term, so the year tiebreak decides: newest first.
	// (The old dateAdded ordering put CCCC3333 first.)
	items, err := db.Search("smith", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AAAA1111", "CCCC3333"}
	if !slices.Equal(keysOf(items), want) {
		t.Errorf("year tiebreak order = %v, want %v", keysOf(items), want)
	}
}

func TestSearch_RankTitleRelevanceFirst(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// Both items match: item 10 by title ("…for Neuroimaging") and pub,
	// item 20 by pub only (NeuroImage). The title hit must outrank the
	// pub-only hit even though item 20 was added later.
	items, err := db.Search("@title: neuroimaging | @pub: neuroimage", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AAAA1111", "BBBB2222"}
	if !slices.Equal(keysOf(items), want) {
		t.Errorf("relevance order = %v, want %v", keysOf(items), want)
	}
}

func TestSearch_RankAppliesBeforeLimit(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// With the old SQL-side LIMIT, dateAdded DESC truncation would keep
	// BBBB2222 and drop the more relevant title hit entirely.
	items, err := db.Search("@title: neuroimaging | @pub: neuroimage", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("limit-after-rank = %v, want [AAAA1111]", keysOf(items))
	}
}

// --- fulltext-extended search ---

func TestSearchWith_FulltextExtendsBareTerms(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// "network" matches item 80 by title; item 10 only via the PDF
	// fulltext word index (attachment 40 carries "network").
	plain, err := db.Search("network", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 1 || plain[0].Key != "GGGG7777" {
		t.Errorf("plain search = %v, want GGGG7777 only", keysOf(plain))
	}

	ft, err := db.SearchWith("network", 10, SearchOptions{Fulltext: true})
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(ft)
	if len(ft) != 2 || !slices.Contains(got, "AAAA1111") || !slices.Contains(got, "GGGG7777") {
		t.Errorf("fulltext search = %v, want AAAA1111+GGGG7777", got)
	}
}

func TestSearchWith_FulltextMultiWordAND(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// "brain network" as a phrase matches no metadata, but both words
	// appear in item 10's PDF fulltext (AND semantics across words).
	// Item 80's PDF has "brain" but not "network" — excluded.
	items, err := db.SearchWith("brain network", 10, SearchOptions{Fulltext: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "AAAA1111" {
		t.Errorf("fulltext AND search = %v, want AAAA1111", keysOf(items))
	}
}

func TestSearchWith_FulltextIgnoresNegatedTerms(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// A negated bare term must not consult the fulltext index — "-cats"
	// excludes item 30 by metadata; items whose PDFs merely lack "cats"
	// are unaffected either way.
	items, err := db.SearchWith("@title: -cats", 10, SearchOptions{Fulltext: true})
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(items)
	if slices.Contains(got, "CCCC3333") {
		t.Errorf("negated search still matched CCCC3333: %v", got)
	}
	if len(got) != 3 {
		t.Errorf("negated search = %v, want 3 hits", got)
	}
}
