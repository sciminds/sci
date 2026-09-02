// Package pdfid finds a paper's identifier inside its PDF: the DOI or
// arXiv id printed on the first pages. It shells out to poppler's
// pdftotext for the text and reuses bib.ScanText for the recognition, so
// there is exactly one DOI grammar in the tree.
//
// The package never touches the network — resolving what it finds is the
// caller's job (internal/zot/doiorg).
package pdfid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/doi"
	"github.com/sciminds/sci/internal/zot/bib"
)

// ErrNoTool means pdftotext is not installed. Callers attach the brew fix.
var ErrNoTool = errors.New("pdftotext not found")

// ErrNoIdentifier means the first pages carried neither a DOI nor an
// arXiv id — a scan, a preprint without a stamp, or a book chapter.
var ErrNoIdentifier = errors.New("no DOI or arXiv id found")

// toolName is the poppler binary Sniff runs.
const toolName = "pdftotext"

// firstPages is how many pages Sniff reads. A paper's own DOI sits on the
// first page (journal header, footer, or a preprint stamp); reading
// further only pulls reference-list DOIs in.
const firstPages = "3"

// lookPath and runPDFToText are the two seams tests replace so the
// package is testable without poppler.
var (
	lookPath     = exec.LookPath
	runPDFToText = func(ctx context.Context, path string) (string, error) {
		cmd := exec.CommandContext(ctx, toolName, "-l", firstPages, "-q", path, "-")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return "", fmt.Errorf("%s %s: %w: %s", toolName, path, err, msg)
			}
			return "", fmt.Errorf("%s %s: %w", toolName, path, err)
		}
		return string(out), nil
	}
)

// Sniff extracts the text of the PDF's first pages with pdftotext and
// returns the identifiers found in it — see [FromText] for the order.
// [ErrNoTool] when pdftotext is not installed; [ErrNoIdentifier] when the
// pages carried none.
func Sniff(ctx context.Context, path string) ([]bib.Ref, error) {
	if _, err := lookPath(toolName); err != nil {
		return nil, ErrNoTool
	}
	text, err := runPDFToText(ctx, path)
	if err != nil {
		return nil, err
	}
	refs := FromText(text)
	if len(refs) == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNoIdentifier)
	}
	return refs, nil
}

// FromText returns the paper identifiers in text: DOIs first, then arXiv
// ids, each group in document order and deduplicated. A DOI that names a
// publisher subobject (a figure, table or supplement — see doi.IsSubobject)
// is dropped, because it is a component of the paper, not the paper. When
// the first pages carry several distinct DOIs (a reference list bleeding
// into page 3) every one is returned in order and the caller decides; the
// first is the paper's own in practice.
func FromText(text string) []bib.Ref {
	refs := bib.ScanText(text)
	dois := lo.Filter(refs, func(r bib.Ref, _ int) bool {
		return r.Kind == bib.KindDOI && !doi.IsSubobject(r.Value)
	})
	arxiv := lo.Filter(refs, func(r bib.Ref, _ int) bool { return r.Kind == bib.KindArxiv })
	return slices.Concat(dois, arxiv)
}
