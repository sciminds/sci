package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot/bib"
)

// BibResult is returned by `zot bib` — a document-driven bibliography.
// It wraps the shared library-export result with citation coverage: how
// many references the document(s) contained, how many resolved, and the
// full list of unresolved references. Unresolved references are always
// surfaced (never silently dropped) so a generated .bib can't quietly
// omit citations.
//
// The unresolved list is the whole answer to "what didn't resolve". It is a
// statement about THIS library, never about the literature: classifying a
// reference as real-but-missing or as resolving nowhere takes an upstream
// index, and that lookup lives on the zot side.
type BibResult struct {
	Export     LibraryExportResult `json:"export"`
	Files      []string            `json:"files"`
	References int                 `json:"references"`
	Resolved   int                 `json:"resolved"`
	Unresolved []bib.Unresolved    `json:"unresolved,omitempty"`
}

// JSON implements cmdutil.Result.
func (r BibResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r BibResult) Human() string {
	var b strings.Builder
	b.WriteString(r.Export.Human())

	fmt.Fprintf(&b, "    %s %d reference(s) in %d file(s): %d resolved, %d unresolved\n",
		uikit.TUI.Dim().Render("·"),
		r.References, len(r.Files), r.Resolved, len(r.Unresolved))

	if len(r.Unresolved) > 0 {
		fmt.Fprintf(&b, "\n  %s unresolved (%d):\n", uikit.SymWarn, len(r.Unresolved))
		for _, u := range r.Unresolved {
			fmt.Fprintf(&b, "    %s %s %s\n",
				uikit.TUI.Dim().Render("-"),
				u.Raw,
				uikit.TUI.Dim().Render(fmt.Sprintf("(%s, %s)", u.Kind, u.Reason)))
		}
	}
	return b.String()
}
