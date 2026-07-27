package bib

import (
	"reflect"
	"slices"
	"testing"
)

func refStrings(refs []Ref) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = string(r.Kind) + ":" + r.Value
	}
	return out
}

func TestScanText_PandocCitekeys(t *testing.T) {
	t.Parallel()
	refs := ScanText("As shown [@saxe2022-ment-ABC123XY; @jolly2019] and -@smith2020.")
	want := []string{
		"citekey:saxe2022-ment-ABC123XY",
		"citekey:jolly2019",
		"citekey:smith2020",
	}
	if !slices.Equal(refStrings(refs), want) {
		t.Errorf("refs = %v, want %v", refStrings(refs), want)
	}
}

func TestScanText_EmailIsNotCitekey(t *testing.T) {
	t.Parallel()
	refs := ScanText("mail me at foo@bar.com about it")
	if len(refs) != 0 {
		t.Errorf("refs = %v, want none", refStrings(refs))
	}
}

func TestScanText_Wikilinks(t *testing.T) {
	t.Parallel()
	refs := ScanText("See [[jolly2023-goss-AAAA1111]] and [[Some Paper Title|the paper]] but not ![[figure.png]].")
	want := []string{
		"wikilink:jolly2023-goss-AAAA1111",
		"wikilink:Some Paper Title",
	}
	if !slices.Equal(refStrings(refs), want) {
		t.Errorf("refs = %v, want %v", refStrings(refs), want)
	}
}

func TestScanText_DOIAndURL(t *testing.T) {
	t.Parallel()
	refs := ScanText("Via https://doi.org/10.1000/abc123 and DOI: 10.1000/def456. Also https://example.org/xyz.")
	want := []string{
		"doi:10.1000/abc123",
		"doi:10.1000/def456",
		"url:https://example.org/xyz",
	}
	if !slices.Equal(refStrings(refs), want) {
		t.Errorf("refs = %v, want %v", refStrings(refs), want)
	}
}

func TestScanText_Arxiv(t *testing.T) {
	t.Parallel()
	refs := ScanText("See arXiv:2401.12345 and https://arxiv.org/abs/1706.03762v5 for details.")
	want := []string{
		"arxiv:2401.12345",
		"arxiv:1706.03762",
	}
	if !slices.Equal(refStrings(refs), want) {
		t.Errorf("refs = %v, want %v", refStrings(refs), want)
	}
}

func TestScanText_DedupsWithinText(t *testing.T) {
	t.Parallel()
	refs := ScanText("[@saxe2022] then again [@saxe2022] and [[Same Link]] plus [[Same Link]]")
	want := []string{
		"citekey:saxe2022",
		"wikilink:Same Link",
	}
	if !slices.Equal(refStrings(refs), want) {
		t.Errorf("refs = %v, want %v", refStrings(refs), want)
	}
}

func TestScanText_DOIInsideURLNotDoubleCounted(t *testing.T) {
	t.Parallel()
	// The DOI appears once inside a doi.org URL — it must surface as one
	// doi ref, not a doi ref plus a url ref.
	refs := ScanText("(https://doi.org/10.1234/j.5678)")
	want := []string{"doi:10.1234/j.5678"}
	if !slices.Equal(refStrings(refs), want) {
		t.Errorf("refs = %v, want %v", refStrings(refs), want)
	}
}

// TestScanText_TrimsUnbalancedClosingParen — a DOI written inside prose
// parentheses, "(doi:10.1038/s41562-024-98765-4)", swallowed the closing
// paren. With --verify that turns a real DOI into a 404 and a real citation
// into an accusation, so the trim has to happen at scan time.
func TestScanText_TrimsUnbalancedClosingParen(t *testing.T) {
	t.Parallel()
	refs := ScanText("Compositional social prediction (doi:10.1038/s41562-024-98765-4).")
	if len(refs) != 1 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Value != "10.1038/s41562-024-98765-4" {
		t.Errorf("value = %q", refs[0].Value)
	}
}

// TestScanText_KeepsBalancedParensInDOI — Wiley's old SICI DOIs genuinely
// contain parentheses; trimming them unconditionally would break real keys.
func TestScanText_KeepsBalancedParensInDOI(t *testing.T) {
	t.Parallel()
	refs := ScanText("See 10.1002/(SICI)1097-0258(19980815) for the method.")
	if len(refs) != 1 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Value != "10.1002/(SICI)1097-0258(19980815)" {
		t.Errorf("value = %q", refs[0].Value)
	}
}

// TestScanText_TrimsParenAfterSentencePunctuation covers the two trims
// composing: "(10.1000/abc.)" has both a period and an unbalanced paren.
func TestScanText_TrimsParenAfterSentencePunctuation(t *testing.T) {
	t.Parallel()
	refs := ScanText("As shown (10.1000/abc.)")
	if len(refs) != 1 || refs[0].Value != "10.1000/abc" {
		t.Fatalf("refs = %+v", refs)
	}
}

// ── zotero:// item URIs ──────────────────────────────────────────────

// A standalone note that discusses papers cites them the way Zotero's own
// UI writes links: `zotero://select/...`. Both the personal-library and
// group forms occur, bare and wrapped in a markdown link.
func TestScanText_ZoteroURIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want []Ref
	}{
		{
			name: "bare personal-library URI",
			text: "see zotero://select/library/items/ABCD1234 for the argument",
			want: []Ref{{Raw: "zotero://select/library/items/ABCD1234", Kind: KindZoteroKey, Value: "ABCD1234"}},
		},
		{
			name: "inside a markdown link",
			text: "[Ho 2022](zotero://select/library/items/ABCD1234) shows this",
			want: []Ref{{Raw: "zotero://select/library/items/ABCD1234", Kind: KindZoteroKey, Value: "ABCD1234"}},
		},
		{
			name: "group library form",
			text: "[Ho 2022](zotero://select/groups/6506098/items/WXYZ9876)",
			want: []Ref{{Raw: "zotero://select/groups/6506098/items/WXYZ9876", Kind: KindZoteroKey, Value: "WXYZ9876"}},
		},
		{
			name: "without the select segment",
			text: "zotero://library/items/ABCD1234",
			want: []Ref{{Raw: "zotero://library/items/ABCD1234", Kind: KindZoteroKey, Value: "ABCD1234"}},
		},
		{
			name: "several in one note, document order, deduped",
			text: "first zotero://select/library/items/AAAA1111, then " +
				"zotero://select/library/items/BBBB2222, then AAAA1111 again: " +
				"zotero://select/library/items/AAAA1111",
			want: []Ref{
				{Raw: "zotero://select/library/items/AAAA1111", Kind: KindZoteroKey, Value: "AAAA1111"},
				{Raw: "zotero://select/library/items/BBBB2222", Kind: KindZoteroKey, Value: "BBBB2222"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ScanText(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanText(%q) =\n  %+v\nwant\n  %+v", tt.text, got, tt.want)
			}
		})
	}
}

// A zotero:// URI claims its span before the URL, DOI and citekey passes
// run, so nothing inside it gets double-counted as another kind of ref.
func TestScanText_ZoteroURIClaimsItsSpan(t *testing.T) {
	t.Parallel()
	got := ScanText("[Ho 2022](zotero://select/groups/6506098/items/ABCD1234)")
	if len(got) != 1 {
		t.Fatalf("ScanText = %+v, want exactly one ref", got)
	}
	if got[0].Kind != KindZoteroKey {
		t.Errorf("Kind = %q, want %q", got[0].Kind, KindZoteroKey)
	}
}

// A wikilink wins over anything nested inside it, same as every other pass.
func TestScanText_WikilinkStillOutranksZoteroURI(t *testing.T) {
	t.Parallel()
	got := ScanText("[[zotero://select/library/items/ABCD1234]]")
	if len(got) != 1 || got[0].Kind != KindWikilink {
		t.Errorf("ScanText = %+v, want a single wikilink", got)
	}
}
