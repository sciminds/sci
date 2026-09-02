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
	"regexp"
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

// oldArxivRe matches the pre-2007 arXiv id form, `arXiv:archive.CC/YYMMNNN`
// with an optional version — hep-th/9901001v2, math.AG/0601001. bib.ScanText
// only knows the new NNNN.NNNNN form, and the physics and maths preprints
// that carry these are exactly the PDFs with no DOI to fall back on. The
// archive part is letters and dashes, so a new-form id (digits before the
// dot) can never match here.
var oldArxivRe = regexp.MustCompile(`(?i)\barxiv:\s*([a-z-]+(?:\.[a-z]{2})?/\d{7})(?:v\d+)?`)

// positioned pairs a reference with its byte offset so the two scans can
// be merged into one document-ordered list.
type positioned struct {
	ref bib.Ref
	pos int
}

// FromText returns the paper identifiers in text — DOIs and arXiv ids —
// in DOCUMENT ORDER, deduplicated. Order is the whole contract: the first
// identifier on the page is the paper's own in practice, and an arXiv
// preprint whose first pages cite a DOI (a footnote, "see doi:…", an early
// reference) would otherwise resolve to the CITED paper. A DOI that names
// a publisher subobject (a figure, table or supplement — see
// doi.IsSubobject) is dropped, because it is a component of the paper,
// not the paper. When the first pages carry several identifiers (a
// reference list bleeding into page 3) every one is returned in order and
// the caller decides.
func FromText(text string) []bib.Ref {
	// bib.ScanText already answers in document order; its position is
	// recovered from the first occurrence of the raw match, which is the
	// occurrence dedup kept.
	scanned := lo.FilterMap(bib.ScanText(text), func(r bib.Ref, _ int) (positioned, bool) {
		switch r.Kind {
		case bib.KindDOI:
			return positioned{ref: r, pos: strings.Index(text, r.Raw)}, !doi.IsSubobject(r.Value)
		case bib.KindArxiv:
			return positioned{ref: r, pos: strings.Index(text, r.Raw)}, true
		default:
			return positioned{}, false
		}
	})
	oldStyle := lo.Map(oldArxivRe.FindAllStringSubmatchIndex(text, -1), func(m []int, _ int) positioned {
		return positioned{
			ref: bib.Ref{Raw: text[m[0]:m[1]], Kind: bib.KindArxiv, Value: text[m[2]:m[3]]},
			pos: m[0],
		}
	})

	found := slices.Concat(scanned, oldStyle)
	slices.SortStableFunc(found, func(a, b positioned) int { return a.pos - b.pos })
	deduped := lo.UniqBy(found, func(p positioned) string {
		return string(p.ref.Kind) + "\x00" + strings.ToLower(p.ref.Value)
	})
	return lo.Map(deduped, func(p positioned, _ int) bib.Ref { return p.ref })
}
