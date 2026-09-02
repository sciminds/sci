package pdfid

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sciminds/sci/internal/zot/bib"
)

// stubTool makes pdftotext "installed" and returns text for any path.
// Tests that use it are sequential: they swap package-level seams.
func stubTool(t *testing.T, text string, runErr error) {
	t.Helper()
	prevLook, prevRun := lookPath, runPDFToText
	lookPath = func(string) (string, error) { return "/usr/bin/pdftotext", nil }
	runPDFToText = func(context.Context, string) (string, error) { return text, runErr }
	t.Cleanup(func() { lookPath, runPDFToText = prevLook, prevRun })
}

func TestSniff_DOIFound(t *testing.T) {
	stubTool(t, "Journal of Things\nhttps://doi.org/10.1016/j.tics.2007.01.001\nAbstract …", nil)
	got, err := Sniff(context.Background(), "paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != bib.KindDOI || got[0].Value != "10.1016/j.tics.2007.01.001" {
		t.Errorf("Sniff = %+v, want the one DOI", got)
	}
}

func TestSniff_ArxivFound(t *testing.T) {
	stubTool(t, "arXiv:1706.03762v5 [cs.CL] 6 Dec 2017\nAttention Is All You Need", nil)
	got, err := Sniff(context.Background(), "paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != bib.KindArxiv || got[0].Value != "1706.03762" {
		t.Errorf("Sniff = %+v, want the one arXiv id, version stripped", got)
	}
}

func TestSniff_NoIdentifier(t *testing.T) {
	stubTool(t, "A scanned page with no identifiers at all.", nil)
	_, err := Sniff(context.Background(), "paper.pdf")
	if !errors.Is(err, ErrNoIdentifier) {
		t.Fatalf("err = %v, want ErrNoIdentifier", err)
	}
}

func TestSniff_ToolMissing(t *testing.T) {
	prev := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not in PATH") }
	t.Cleanup(func() { lookPath = prev })
	_, err := Sniff(context.Background(), "paper.pdf")
	if !errors.Is(err, ErrNoTool) {
		t.Fatalf("err = %v, want ErrNoTool", err)
	}
}

func TestSniff_ToolFailureIsNotNoIdentifier(t *testing.T) {
	stubTool(t, "", errors.New("pdftotext: Syntax Error: Couldn't read xref table"))
	_, err := Sniff(context.Background(), "paper.pdf")
	if err == nil || errors.Is(err, ErrNoIdentifier) || errors.Is(err, ErrNoTool) {
		t.Fatalf("err = %v, want the tool's own failure", err)
	}
}

// TestFromText_DocumentOrder — the first identifier on the page is the
// paper's own in practice. An arXiv preprint whose first pages cite a DOI
// (a footnote, "see doi:…", an early reference) must yield the arXiv id
// first, or the caller attaches the PDF to the CITED paper.
func TestFromText_DocumentOrder(t *testing.T) {
	t.Parallel()
	text := "arXiv:2101.00001v2 [cs.LG]\nExtends the model of doi:10.1000/cited\nReferences\n10.1000/second\n10.1000/cited"
	got := FromText(text)
	want := []bib.Ref{
		{Raw: "arXiv:2101.00001", Kind: bib.KindArxiv, Value: "2101.00001"},
		{Raw: "10.1000/cited", Kind: bib.KindDOI, Value: "10.1000/cited"},
		{Raw: "10.1000/second", Kind: bib.KindDOI, Value: "10.1000/second"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromText =\n%+v\nwant\n%+v", got, want)
	}
}

// TestFromText_OldStyleArxivIds — pre-2007 ids (archive/YYMMNNN) are what
// a physics or math preprint carries; bib.ScanText only knows the new
// form, so pdfid recognises them itself.
func TestFromText_OldStyleArxivIds(t *testing.T) {
	t.Parallel()
	text := "arXiv:hep-th/9901001v2  4 Jan 1999\nSee also arXiv:math.AG/0601001 and doi:10.1000/x\narXiv:HEP-TH/9901001"
	got := FromText(text)
	want := []bib.Ref{
		{Raw: "arXiv:hep-th/9901001v2", Kind: bib.KindArxiv, Value: "hep-th/9901001"},
		{Raw: "arXiv:math.AG/0601001", Kind: bib.KindArxiv, Value: "math.AG/0601001"},
		{Raw: "10.1000/x", Kind: bib.KindDOI, Value: "10.1000/x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromText =\n%+v\nwant\n%+v", got, want)
	}
}

// TestFromText_OldStyleDoesNotDoubleCountNewStyle pins that the two arXiv
// passes never both claim a new-form id.
func TestFromText_OldStyleDoesNotDoubleCountNewStyle(t *testing.T) {
	t.Parallel()
	got := FromText("arXiv:1706.03762v5 and arXiv: 1706.03762")
	if len(got) != 1 || got[0].Value != "1706.03762" {
		t.Errorf("FromText = %+v, want exactly one arXiv id", got)
	}
}

// TestFromText_SubobjectDOIsFiltered — a figure or supplement DOI names a
// component of the paper, never the paper.
func TestFromText_SubobjectDOIsFiltered(t *testing.T) {
	t.Parallel()
	got := FromText("Fig 1: 10.1371/journal.pone.0012345.g001\nCitation: 10.1371/journal.pone.0012345")
	if len(got) != 1 || got[0].Value != "10.1371/journal.pone.0012345" {
		t.Errorf("FromText = %+v, want only the parent DOI", got)
	}
}

func TestFromText_IgnoresOtherReferenceKinds(t *testing.T) {
	t.Parallel()
	got := FromText("See https://example.org/page and @smith2020 and [[a note]]")
	if len(got) != 0 {
		t.Errorf("FromText = %+v, want nothing (URLs, citekeys and wikilinks are not identifiers)", got)
	}
}
