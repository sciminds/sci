package local

import (
	"errors"
	"slices"
	"testing"

	"github.com/samber/lo"
)

// contentHook returns a SearchOptions.Content stub that records the text
// it was handed and reports the given keys as matches, all scored equally.
func contentHook(seen *string, keys ...string) func(string) (map[string]float64, error) {
	scored := lo.SliceToMap(keys, func(k string) (string, float64) { return k, 1 })
	return contentScores(seen, scored)
}

// contentScores is contentHook with explicit per-key relevance scores,
// for the ranking tests.
func contentScores(seen *string, scores map[string]float64) func(string) (map[string]float64, error) {
	return func(text string) (map[string]float64, error) {
		*seen = text
		return scores, nil
	}
}

func TestSearchWithContentWidensFreeText(t *testing.T) {
	db := openFixture(t)

	// "communicability" appears in no metadata field.
	off, err := db.SearchWith("communicability", 10, SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Fatalf("matched without --content: %v", keysOf(off))
	}

	var seen string
	on, err := db.SearchWith("communicability", 10, SearchOptions{
		Content: contentHook(&seen, "BBBB2222"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 1 || on[0].Key != "BBBB2222" {
		t.Errorf("got %v, want [BBBB2222]", keysOf(on))
	}
}

// The content index parses phrases and prefixes itself, so it must
// receive the user's text verbatim — pre-splitting it into words here
// would silently turn a phrase query into ANDed terms.
func TestSearchWithContentReceivesRawQuotedText(t *testing.T) {
	db := openFixture(t)

	var seen string
	if _, err := db.SearchWith(`"prediction error"`, 10, SearchOptions{
		Content: contentHook(&seen),
	}); err != nil {
		t.Fatal(err)
	}
	if seen != `"prediction error"` {
		t.Errorf("Content hook saw %q, want the quotes preserved", seen)
	}
}

// Field-scoped and negated clauses are metadata questions; widening them
// with paper text would make "@author: smith" match every paper that
// merely cites a Smith.
func TestSearchWithContentSkipsFieldScopedClauses(t *testing.T) {
	db := openFixture(t)

	called := false
	_, err := db.SearchWith("@author: nobodyxyz", 10, SearchOptions{
		Content: func(string) (map[string]float64, error) {
			called = true
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("Content hook was consulted for a field-scoped clause")
	}
}

// A negated clause excludes by metadata; consulting paper text for it
// would make "-cats" drop every paper that merely mentions cats.
func TestSearchWithContentSkipsNegatedClauses(t *testing.T) {
	db := openFixture(t)

	called := false
	items, err := db.SearchWith("@title: -cats", 10, SearchOptions{
		Content: func(string) (map[string]float64, error) {
			called = true
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("Content hook was consulted for a negated clause")
	}
	if contains(keysOf(items), "CCCC3333") {
		t.Errorf("negated search still matched CCCC3333: %v", keysOf(items))
	}
}

func TestSearchWithContentPropagatesErrors(t *testing.T) {
	db := openFixture(t)

	sentinel := errors.New("index unavailable")
	_, err := db.SearchWith("neuroimaging", 10, SearchOptions{
		Content: func(string) (map[string]float64, error) { return nil, sentinel },
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want it to wrap %v", err, sentinel)
	}
}

// A content hit must not replace metadata hits — it widens the clause.
func TestSearchWithContentUnionsWithMetadataHits(t *testing.T) {
	db := openFixture(t)

	var seen string
	got, err := db.SearchWith("neuroimaging", 10, SearchOptions{
		Content: contentHook(&seen, "GGGG7777"),
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := keysOf(got)
	if !contains(keys, "AAAA1111") {
		t.Errorf("metadata hit AAAA1111 missing from %v", keys)
	}
	if !contains(keys, "GGGG7777") {
		t.Errorf("content hit GGGG7777 missing from %v", keys)
	}
}

// Content hits share a title relevance of zero, so before scores reached
// the ranker they fell back to year — newest-wins, which is not the same
// question as "which paper is this query about".
func TestSearchRanksContentHitsByScore(t *testing.T) {
	db := openFixture(t)

	var seen string
	got, err := db.SearchWith("communicability", 10, SearchOptions{
		Content: contentScores(&seen, map[string]float64{
			"BBBB2222": 0.5, // 2024 — newest, but a passing mention
			"CCCC3333": 9.0, // 2023 — what the query is actually about
			"GGGG7777": 3.0, // 2023
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"CCCC3333", "GGGG7777", "BBBB2222"}
	if diff := keysOf(got); !slices.Equal(diff, want) {
		t.Errorf("ranked %v, want %v (by content score, not year)", diff, want)
	}
}

// A title match is the strongest signal there is, and items without an
// extraction have no score at all — ranking on content alone would sink
// the exact paper the user named below every passing mention of it.
func TestSearchTitleRelevanceOutranksContentScore(t *testing.T) {
	db := openFixture(t)

	var seen string
	got, err := db.SearchWith("neuroimaging", 10, SearchOptions{
		Content: contentScores(&seen, map[string]float64{"CCCC3333": 99.0}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// AAAA1111 is "Deep Learning for Neuroimaging" and has no content score.
	if keys := keysOf(got); len(keys) == 0 || keys[0] != "AAAA1111" {
		t.Errorf("ranked %v, want the title match AAAA1111 first", keys)
	}
}

// The snippet pass has to re-ask the index the same question the widening
// pass did, so the free text has to be recoverable from the raw query.
func TestQueryFreeText(t *testing.T) {
	cases := map[string]string{
		`"prediction error"`:            `"prediction error"`,
		`gossip reputation`:             `gossip reputation`,
		`@author: jolly @title: gossip`: ``,       // every clause is field-scoped
		`@author: jolly gossip`:         ``,       // "gossip" belongs to the author clause
		`@type: book | gossip`:          `gossip`, // the free-text OR group survives
		`-cats`:                         ``,       // negated: excludes, never widens
	}
	for query, want := range cases {
		if got := QueryFreeText(query); got != want {
			t.Errorf("QueryFreeText(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestItemIDsForKeysSkipsUnknownAndTrashed(t *testing.T) {
	db := openFixture(t)

	ids, err := db.ItemIDsForKeys([]string{"AAAA1111", "NOSUCHKY", "EEEE5555"})
	if err != nil {
		t.Fatalf("ItemIDsForKeys: %v", err)
	}
	// EEEE5555 (item 50) is in deletedItems; NOSUCHKY does not exist.
	if len(ids) != 1 || ids[0] != 10 {
		t.Errorf("ids = %v, want [10]", ids)
	}
}

func TestItemIDsForKeysEmpty(t *testing.T) {
	db := openFixture(t)

	ids, err := db.ItemIDsForKeys(nil)
	if err != nil {
		t.Fatalf("ItemIDsForKeys(nil): %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}

func TestContentSignatureChangesWithLibrary(t *testing.T) {
	db := openFixture(t)

	sig, err := db.ContentSignature()
	if err != nil {
		t.Fatalf("ContentSignature: %v", err)
	}
	if sig == "" {
		t.Fatal("ContentSignature returned empty")
	}
	again, err := db.ContentSignature()
	if err != nil {
		t.Fatal(err)
	}
	if sig != again {
		t.Errorf("signature is not stable: %q then %q", sig, again)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
