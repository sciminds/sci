package bib

import (
	"slices"
	"testing"

	"github.com/sciminds/sci/pkg/local"
)

func testItems() []local.Item {
	return []local.Item{
		{
			Key:   "AAAA1111",
			Type:  "journalArticle",
			Title: "Deep Learning for Neuroimaging",
			Date:  "2024-03-15 March 15, 2024",
			Year:  2024,
			DOI:   "10.1000/abc123",
			URL:   "https://example.org/abc",
			Fields: map[string]string{
				"citationKey": "smith2024-deeplearneur-AAAA1111",
			},
			Creators: []local.Creator{
				{Type: "author", First: "Alice", Last: "Smith", OrderIdx: 0},
				{Type: "author", First: "Bob", Last: "Jones", OrderIdx: 1},
			},
		},
		{
			Key:   "BBBB2222",
			Type:  "journalArticle",
			Title: "Transformers in fMRI Analysis",
			Date:  "2024-03-15 March 15, 2024",
			Year:  2024,
			URL:   "https://arxiv.org/abs/1706.03762",
			Creators: []local.Creator{
				{Type: "author", Name: "NASA", OrderIdx: 0},
			},
		},
		{
			Key:   "CCCC3333",
			Type:  "book",
			Title: "A Book About Cats",
			Date:  "2023",
			Year:  2023,
			Fields: map[string]string{
				"extra": "tldr: loose note\nCitation Key: legacyBookKey1900\n",
			},
			Creators: []local.Creator{
				{Type: "author", First: "Alice", Last: "Smith", OrderIdx: 0},
			},
		},
	}
}

func resolveOne(t *testing.T, ref Ref) *local.Item {
	t.Helper()
	resolved, unresolved := Resolve([]Ref{ref}, testItems())
	if len(unresolved) > 0 {
		t.Fatalf("unresolved: %+v", unresolved)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d items, want 1", len(resolved))
	}
	return &resolved[0]
}

func TestResolve_PinnedCitekey(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindCitekey, Value: "smith2024-deeplearneur-AAAA1111", Raw: "@smith2024-deeplearneur-AAAA1111"})
	if it.Key != "AAAA1111" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_ExtraFieldCitekey(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindCitekey, Value: "legacyBookKey1900"})
	if it.Key != "CCCC3333" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_SynthesizedCitekeyViaZotKeySuffix(t *testing.T) {
	t.Parallel()
	// BBBB2222 has no stored key; any v2-shaped key ending in its Zotero
	// key resolves — the manuscript may carry a synthesized key from an
	// earlier export whose prefix has since drifted.
	it := resolveOne(t, Ref{Kind: KindCitekey, Value: "nasa2024-tranfmrianal-BBBB2222"})
	if it.Key != "BBBB2222" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_WikilinkByCitekey(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindWikilink, Value: "smith2024-deeplearneur-AAAA1111"})
	if it.Key != "AAAA1111" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_WikilinkByTitle(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindWikilink, Value: "A Book About Cats"})
	if it.Key != "CCCC3333" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_WikilinkByAuthorYear(t *testing.T) {
	t.Parallel()
	// Smith authors AAAA1111 (2024) and CCCC3333 (2023) — the year makes
	// each unambiguous.
	it := resolveOne(t, Ref{Kind: KindWikilink, Value: "Smith 2023"})
	if it.Key != "CCCC3333" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_DOICaseInsensitive(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindDOI, Value: "10.1000/ABC123"})
	if it.Key != "AAAA1111" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_URLExact(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindURL, Value: "https://example.org/abc"})
	if it.Key != "AAAA1111" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_ArxivID(t *testing.T) {
	t.Parallel()
	it := resolveOne(t, Ref{Kind: KindArxiv, Value: "1706.03762"})
	if it.Key != "BBBB2222" {
		t.Errorf("key = %s", it.Key)
	}
}

func TestResolve_NoMatchIsUnresolved(t *testing.T) {
	t.Parallel()
	resolved, unresolved := Resolve([]Ref{{Kind: KindCitekey, Value: "nope2020", Raw: "@nope2020"}}, testItems())
	if len(resolved) != 0 {
		t.Errorf("resolved = %d, want 0", len(resolved))
	}
	if len(unresolved) != 1 || unresolved[0].Raw != "@nope2020" {
		t.Errorf("unresolved = %+v", unresolved)
	}
}

func TestResolve_AmbiguousIsUnresolved(t *testing.T) {
	t.Parallel()
	// Two distinct items with the same title: a wikilink by title must
	// refuse to guess.
	items := testItems()
	items = append(items, local.Item{Key: "DDDD4444", Title: "A Book About Cats", Year: 2020})
	resolved, unresolved := Resolve([]Ref{{Kind: KindWikilink, Value: "A Book About Cats"}}, items)
	if len(resolved) != 0 {
		t.Errorf("resolved = %v, want none", resolved)
	}
	if len(unresolved) != 1 {
		t.Fatalf("unresolved = %+v, want 1", unresolved)
	}
}

func TestResolve_DedupsByItem(t *testing.T) {
	t.Parallel()
	refs := []Ref{
		{Kind: KindCitekey, Value: "smith2024-deeplearneur-AAAA1111"},
		{Kind: KindDOI, Value: "10.1000/abc123"},
	}
	resolved, unresolved := Resolve(refs, testItems())
	if len(unresolved) != 0 {
		t.Fatalf("unresolved: %+v", unresolved)
	}
	if len(resolved) != 1 || resolved[0].Key != "AAAA1111" {
		t.Errorf("resolved = %v, want one AAAA1111", keysOf(resolved))
	}
}

func TestResolve_OrderIsFirstAppearance(t *testing.T) {
	t.Parallel()
	refs := []Ref{
		{Kind: KindWikilink, Value: "A Book About Cats"},
		{Kind: KindDOI, Value: "10.1000/abc123"},
	}
	resolved, _ := Resolve(refs, testItems())
	want := []string{"CCCC3333", "AAAA1111"}
	if !slices.Equal(keysOf(resolved), want) {
		t.Errorf("order = %v, want %v", keysOf(resolved), want)
	}
}

func keysOf(items []local.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Key
	}
	return out
}

// TestResolve_AmbiguousReportsCandidates pins the disambiguation payload:
// when a reference matches more than one item the resolver refuses to guess
// (existing contract) AND names the competing Zotero keys, so the caller can
// emit an actionable fix instead of "ambiguous (2 candidates)" and nothing
// to act on. Eshin's hand-written case: "Carey 2009" matching two records.
func TestResolve_AmbiguousReportsCandidates(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{
			Key: "DDDD4444", Type: "book", Title: "The Origin of Concepts",
			Year: 2009, Creators: []local.Creator{{Type: "author", Last: "Carey"}},
		},
		{
			Key: "EEEE5555", Type: "journalArticle", Title: "The Origin of Concepts (article)",
			Year: 2009, Creators: []local.Creator{{Type: "author", Last: "Carey"}},
		},
	}
	_, unresolved := Resolve([]Ref{{Kind: KindWikilink, Value: "Carey 2009"}}, items)
	if len(unresolved) != 1 {
		t.Fatalf("unresolved = %d, want 1", len(unresolved))
	}
	u := unresolved[0]
	if u.Reason != "ambiguous (2 candidates)" {
		t.Errorf("reason = %q", u.Reason)
	}
	want := []string{"DDDD4444", "EEEE5555"}
	if !slices.Equal(u.Candidates, want) {
		t.Errorf("candidates = %v, want %v", u.Candidates, want)
	}
}

// TestResolve_NoMatchHasNoCandidates keeps Candidates empty (and omitted
// from JSON) when nothing matched — an empty list would read as "we found
// something", which is the opposite of the honesty contract.
func TestResolve_NoMatchHasNoCandidates(t *testing.T) {
	t.Parallel()
	_, unresolved := Resolve([]Ref{{Kind: KindDOI, Value: "10.9999/nope"}}, testItems())
	if len(unresolved) != 1 {
		t.Fatalf("unresolved = %d, want 1", len(unresolved))
	}
	if unresolved[0].Candidates != nil {
		t.Errorf("candidates = %v, want nil", unresolved[0].Candidates)
	}
}

// ── zotero:// item keys ──────────────────────────────────────────────

func TestResolve_ZoteroKey(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "AAAA1111", Title: "Deep Learning for Neuroimaging"},
		{Key: "BBBB2222", Title: "Transformers in fMRI Analysis"},
	}
	refs := ScanText("as argued in [Ho 2022](zotero://select/library/items/BBBB2222)")

	resolved, unresolved := Resolve(refs, items)
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %+v, want none", unresolved)
	}
	if len(resolved) != 1 || resolved[0].Key != "BBBB2222" {
		t.Errorf("resolved = %+v, want just BBBB2222", resolved)
	}
}

// A key that no longer exists surfaces as unresolved rather than being
// trusted — the same honesty gate every other ref kind gets.
func TestResolve_DanglingZoteroKey(t *testing.T) {
	t.Parallel()
	items := []local.Item{{Key: "AAAA1111", Title: "Deep Learning for Neuroimaging"}}
	refs := ScanText("zotero://select/library/items/ZZZZ9999")

	resolved, unresolved := Resolve(refs, items)
	if len(resolved) != 0 {
		t.Errorf("resolved = %+v, want none", resolved)
	}
	if len(unresolved) != 1 || unresolved[0].Reason != "no match" {
		t.Fatalf("unresolved = %+v, want one 'no match'", unresolved)
	}
	if unresolved[0].Kind != KindZoteroKey || unresolved[0].Value != "ZZZZ9999" {
		t.Errorf("unresolved ref = %+v, want the zotero-key ZZZZ9999", unresolved[0].Ref)
	}
}

// The same paper cited both ways is ONE resolved item — Resolve dedupes by
// Zotero key, which is what lets `link suggest` merge a URI and a DOI.
func TestResolve_ZoteroKeyAndDOIDedupe(t *testing.T) {
	t.Parallel()
	items := []local.Item{{Key: "AAAA1111", Title: "Paper", DOI: "10.1000/abc123"}}
	refs := ScanText("zotero://select/library/items/AAAA1111 and also 10.1000/abc123")

	resolved, unresolved := Resolve(refs, items)
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %+v, want none", unresolved)
	}
	if len(resolved) != 1 {
		t.Errorf("resolved = %+v, want one deduplicated item", resolved)
	}
}
