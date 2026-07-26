package zot

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/bib"
)

func TestBibResult_HumanSurfacesUnresolved(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "bibtex", OutPath: "refs.bib", Stats: ExportStats{Total: 2, Pinned: 1, Synthesized: 1}},
		Files:      []string{"draft.md"},
		References: 3,
		Resolved:   2,
		Unresolved: []bib.Unresolved{
			{Ref: bib.Ref{Raw: "@ghost2020", Kind: bib.KindCitekey, Value: "ghost2020"}, Reason: "no match"},
		},
	}
	h := r.Human()
	for _, want := range []string{"refs.bib", "3 reference(s)", "2 resolved, 1 unresolved", "@ghost2020", "no match"} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

func TestBibResult_HumanCleanWhenFullyResolved(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "bibtex", OutPath: "refs.bib", Stats: ExportStats{Total: 1, Pinned: 1}},
		Files:      []string{"draft.md"},
		References: 1,
		Resolved:   1,
	}
	if h := r.Human(); strings.Contains(h, "unresolved (") {
		t.Errorf("Human() shows unresolved block with none present:\n%s", h)
	}
}
