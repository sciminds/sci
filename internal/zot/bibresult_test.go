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

// TestBibResult_HumanPartitionsVerified is the payoff of --verify: the flat
// "unresolved" list becomes a three-way partition where each group has a
// different action, and the fabricated-citation group is the loud one.
func TestBibResult_HumanPartitionsVerified(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "bibtex", Stats: ExportStats{Total: 1}},
		Files:      []string{"draft.md"},
		References: 5,
		Resolved:   1,
		Verified: []bib.Verified{
			{
				Unresolved: bib.Unresolved{Ref: bib.Ref{Raw: "10.1016/j.tics.2007.05.005", Kind: bib.KindDOI}, Reason: "no match"},
				Status:     bib.StatusExternal,
				Match:      &bib.Match{Title: "The proactive brain", Year: 2007, DOI: "10.1016/j.tics.2007.05.005"},
				Fix:        "sci zot --library personal item add --openalex 10.1016/j.tics.2007.05.005",
			},
			{
				Unresolved: bib.Unresolved{Ref: bib.Ref{Raw: "10.1234/invented", Kind: bib.KindDOI}, Reason: "no match"},
				Status:     bib.StatusNotFound,
			},
			{
				Unresolved: bib.Unresolved{
					Ref:        bib.Ref{Raw: "[[Carey 2009]]", Kind: bib.KindWikilink},
					Reason:     "ambiguous (2 candidates)",
					Candidates: []string{"DDDD4444", "EEEE5555"},
				},
				Status: bib.StatusAmbiguous,
				Fix:    `sci zot --library personal search "@key:DDDD4444 | @key:EEEE5555"`,
			},
			{
				Unresolved: bib.Unresolved{Ref: bib.Ref{Raw: "@ghost2020", Kind: bib.KindCitekey}, Reason: "no match"},
				Status:     bib.StatusUnchecked,
			},
		},
	}
	h := r.Human()
	for _, want := range []string{
		"resolves nowhere",    // the hallucination group is named
		"10.1234/invented",    // …and the offending ref shown
		"not in library",      // the addable group
		"The proactive brain", // …with the upstream title as evidence
		"item add --openalex", // …and a runnable fix
		"ambiguous",           // the disambiguation group
		"@key:DDDD4444",       // …with the OR-group fix
		"unchecked",           // the honest can't-tell group
		"@ghost2020",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

// TestBibResult_HumanPrefersVerifiedOverRawUnresolved — with --verify on, the
// flat list would just repeat the partition. Show one or the other.
func TestBibResult_HumanPrefersVerifiedOverRawUnresolved(t *testing.T) {
	t.Parallel()
	u := bib.Unresolved{Ref: bib.Ref{Raw: "@ghost2020", Kind: bib.KindCitekey}, Reason: "no match"}
	r := BibResult{
		Export:     LibraryExportResult{Format: "bibtex"},
		Files:      []string{"draft.md"},
		References: 1,
		Unresolved: []bib.Unresolved{u},
		Verified:   []bib.Verified{{Unresolved: u, Status: bib.StatusUnchecked}},
	}
	if h := r.Human(); strings.Contains(h, "unresolved (1)") {
		t.Errorf("Human() renders both the flat list and the partition:\n%s", h)
	}
}

// TestBibResult_HumanCountsRetraction — a retracted citation is a finding
// even though the reference is perfectly real.
func TestBibResult_HumanCountsRetraction(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export: LibraryExportResult{Format: "bibtex"},
		Files:  []string{"draft.md"},
		Verified: []bib.Verified{{
			Unresolved: bib.Unresolved{Ref: bib.Ref{Raw: "10.1016/bad", Kind: bib.KindDOI}},
			Status:     bib.StatusExternal,
			Match:      &bib.Match{Title: "Withdrawn Result", Retracted: true},
		}},
	}
	if h := r.Human(); !strings.Contains(strings.ToLower(h), "retracted") {
		t.Errorf("Human() hides a retraction:\n%s", h)
	}
}
