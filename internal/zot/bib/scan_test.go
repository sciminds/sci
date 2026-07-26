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
