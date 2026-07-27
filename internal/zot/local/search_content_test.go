package local

import (
	"errors"
	"testing"
)

// contentHook returns a SearchOptions.Content stub that records the text
// it was handed and resolves the given keys to item IDs.
func contentHook(db *DB, seen *string, keys ...string) func(string) ([]int64, error) {
	return func(text string) ([]int64, error) {
		*seen = text
		return db.ItemIDsForKeys(keys)
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
		Content: contentHook(db, &seen, "BBBB2222"),
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
		Content: contentHook(db, &seen),
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
		Content: func(string) ([]int64, error) {
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
		Content: func(string) ([]int64, error) {
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
		Content: func(string) ([]int64, error) { return nil, sentinel },
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
		Content: contentHook(db, &seen, "GGGG7777"),
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
