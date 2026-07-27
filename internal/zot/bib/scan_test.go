package bib

import (
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
