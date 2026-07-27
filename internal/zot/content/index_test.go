package content

import (
	"path/filepath"
	"strings"
	"testing"
)

// openTestIndex returns an index backed by a temp file (not :memory:, so
// the reopen-persistence path is exercised too).
func openTestIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := Open(filepath.Join(t.TempDir(), "content.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix
}

func mustUpsert(t *testing.T, ix *Index, docs ...Doc) {
	t.Helper()
	if err := ix.Upsert(docs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestSearchFindsAndReportsSource(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix,
		Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 1,
			Body: "Reward prediction error signals in the striatum."},
		Doc{ItemKey: "BBBB2222", Source: SourceZotero, Version: 1,
			Body: "Gossip and reputation in large social groups."},
	)

	hits, err := ix.Search(Query{Text: "prediction error", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].ItemKey != "AAAA1111" {
		t.Errorf("ItemKey = %q, want AAAA1111", hits[0].ItemKey)
	}
	if hits[0].Source != SourceDocling {
		t.Errorf("Source = %q, want %q", hits[0].Source, SourceDocling)
	}
}

// The whole reason to have an index rather than a substring scan: a
// quoted phrase must not match documents that contain the words apart.
func TestPhraseQueryDoesNotMatchScatteredWords(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix,
		Doc{ItemKey: "TOGETHER", Source: SourceDocling, Version: 1,
			Body: "the prediction error term dominates"},
		Doc{ItemKey: "SCATTERD", Source: SourceDocling, Version: 1,
			Body: "prediction of behavior; a measurement error crept in"},
	)

	hits, err := ix.Search(Query{Text: `"prediction error"`, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ItemKey != "TOGETHER" {
		t.Fatalf("phrase query matched %+v, want only TOGETHER", hits)
	}
}

// Stemming is why "representation" finds "representations" — the
// substring scan this replaces could only do it by accident.
func TestPorterStemming(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "STEMTEST", Source: SourceDocling, Version: 1,
		Body: "neural representations of value"})

	hits, err := ix.Search(Query{Text: "representation", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1 (porter stemmer should match the plural)", len(hits))
	}
}

// Substring matching was the old behavior's precision bug: "div" matched
// "individual". Token matching must not.
func TestNoWordInteriorMatches(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "INTERIOR", Source: SourceDocling, Version: 1,
		Body: "individual differences in behavior"})

	hits, err := ix.Search(Query{Text: "div", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits, want 0 — 'div' must not match inside 'individual'", len(hits))
	}
}

func TestSnippetHighlightsTheMatch(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "SNIPTEST", Source: SourceDocling, Version: 1,
		Body: strings.Repeat("filler words here. ", 40) +
			"the crucial gossip mechanism follows. " +
			strings.Repeat("more filler. ", 40)})

	hits, err := ix.Search(Query{Text: "gossip", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	snip := hits[0].Snippet
	if !strings.Contains(snip, "gossip") {
		t.Errorf("snippet %q does not contain the matched term", snip)
	}
	if len(snip) >= len(hits[0].ItemKey)+2000 {
		t.Errorf("snippet is not bounded: %d chars", len(snip))
	}
}

// bm25 ranking: the doc that is *about* the term should outrank the one
// that mentions it once in passing.
func TestRankingPutsDenserMatchFirst(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix,
		Doc{ItemKey: "PASSING1", Source: SourceDocling, Version: 1,
			Body: "gossip " + strings.Repeat("unrelated content here. ", 200)},
		Doc{ItemKey: "ABOUTGOS", Source: SourceDocling, Version: 1,
			Body: strings.Repeat("gossip drives reputation and gossip spreads. ", 20)},
	)

	hits, err := ix.Search(Query{Text: "gossip", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].ItemKey != "ABOUTGOS" {
		t.Errorf("ranked %q first, want ABOUTGOS (denser match)", hits[0].ItemKey)
	}
}

func TestLimitCapsResults(t *testing.T) {
	ix := openTestIndex(t)
	docs := []Doc{}
	for _, k := range []string{"AAAA0001", "AAAA0002", "AAAA0003", "AAAA0004"} {
		docs = append(docs, Doc{ItemKey: k, Source: SourceDocling, Version: 1, Body: "gossip"})
	}
	mustUpsert(t, ix, docs...)

	hits, err := ix.Search(Query{Text: "gossip", Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits, want 2", len(hits))
	}
}

// An empty query must be an error, not a full-table dump.
func TestSearchEmptyQuery(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 1, Body: "text"})

	if _, err := ix.Search(Query{Text: "  ", Limit: 10}); err == nil {
		t.Error("Search with a termless query returned nil error, want one")
	}
}

// Upsert must replace, not accumulate — and the FTS index has to follow
// the content table or stale text keeps matching forever.
func TestUpsertReplacesBodyAndPurgesOldTerms(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "AAAA1111", Source: SourceZotero, Version: 1,
		Body: "obsolete pdftotext garble"})
	mustUpsert(t, ix, Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 7,
		Body: "clean docling markdown"})

	hits, err := ix.Search(Query{Text: "obsolete", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("stale term still matches after upsert: %+v", hits)
	}

	hits, err = ix.Search(Query{Text: "docling", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits for the new body, want 1", len(hits))
	}
	if hits[0].Source != SourceDocling {
		t.Errorf("Source = %q, want the upgraded %q", hits[0].Source, SourceDocling)
	}

	st, err := ix.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st["AAAA1111"].Version != 7 {
		t.Errorf("State[AAAA1111].Version = %d, want 7", st["AAAA1111"].Version)
	}
}

func TestStateDrivesStalenessDiff(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix,
		Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 3, Body: "one"},
		Doc{ItemKey: "BBBB2222", Source: SourceZotero, Version: 5, Body: "two"},
	)

	got, err := ix.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	want := map[string]DocState{
		"AAAA1111": {Source: SourceDocling, Version: 3},
		"BBBB2222": {Source: SourceZotero, Version: 5},
	}
	if len(got) != len(want) {
		t.Fatalf("State returned %d entries, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("State[%s] = %+v, want %+v", k, got[k], v)
		}
	}
}

func TestDeleteRemovesFromBothTables(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix,
		Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 1, Body: "gossip here"},
		Doc{ItemKey: "BBBB2222", Source: SourceDocling, Version: 1, Body: "gossip there"},
	)

	if err := ix.Delete([]string{"AAAA1111"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	hits, err := ix.Search(Query{Text: "gossip", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ItemKey != "BBBB2222" {
		t.Errorf("after delete got %+v, want only BBBB2222", hits)
	}

	st, err := ix.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if _, ok := st["AAAA1111"]; ok {
		t.Error("deleted key still present in State")
	}
}

func TestStatsCountsBySource(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix,
		Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 1, Body: "aaa"},
		Doc{ItemKey: "BBBB2222", Source: SourceDocling, Version: 1, Body: "bbb"},
		Doc{ItemKey: "CCCC3333", Source: SourceZotero, Version: 1, Body: "ccc"},
	)

	st, err := ix.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 3 {
		t.Errorf("Total = %d, want 3", st.Total)
	}
	if st.BySource[SourceDocling] != 2 {
		t.Errorf("BySource[docling] = %d, want 2", st.BySource[SourceDocling])
	}
	if st.BySource[SourceZotero] != 1 {
		t.Errorf("BySource[zotero] = %d, want 1", st.BySource[SourceZotero])
	}
}

// The index lives in a cache dir a user may wipe; reopening must find
// the data and not re-create an empty schema over it.
func TestReopenPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustUpsert(t, ix, Doc{ItemKey: "AAAA1111", Source: SourceDocling, Version: 1, Body: "gossip"})
	if err := ix.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ix2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = ix2.Close() }()

	hits, err := ix2.Search(Query{Text: "gossip", Limit: 10})
	if err != nil {
		t.Fatalf("Search after reopen: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("got %d hits after reopen, want 1", len(hits))
	}
}

func TestUpsertEmptyIsNoop(t *testing.T) {
	ix := openTestIndex(t)
	if err := ix.Upsert(nil); err != nil {
		t.Errorf("Upsert(nil) = %v, want nil", err)
	}
	if err := ix.Delete(nil); err != nil {
		t.Errorf("Delete(nil) = %v, want nil", err)
	}
}
