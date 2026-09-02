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

func TestFromText_DOIsBeforeArxivInDocumentOrder(t *testing.T) {
	t.Parallel()
	text := "arXiv:2101.00001\nPreprint of doi:10.1000/first\nReferences\n10.1000/second\n10.1000/first"
	got := FromText(text)
	want := []bib.Ref{
		{Raw: "10.1000/first", Kind: bib.KindDOI, Value: "10.1000/first"},
		{Raw: "10.1000/second", Kind: bib.KindDOI, Value: "10.1000/second"},
		{Raw: "arXiv:2101.00001", Kind: bib.KindArxiv, Value: "2101.00001"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromText =\n%+v\nwant\n%+v", got, want)
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
