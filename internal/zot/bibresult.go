package zot

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/bib"
)

// BibResult is returned by `zot bib` — a document-driven bibliography.
// It wraps the shared library-export result with citation coverage: how
// many references the document(s) contained, how many resolved, and the
// full list of unresolved references. Unresolved references are always
// surfaced (never silently dropped) so a generated .bib can't quietly
// omit citations.
//
// With `--verify`, Verified carries the same references classified against
// an upstream index — see [bib.VerifyStatus]. When it's populated the human
// rendering shows the partition instead of the flat Unresolved list; both
// stay in the JSON so an agent can read either.
type BibResult struct {
	Export     LibraryExportResult `json:"export"`
	Files      []string            `json:"files"`
	References int                 `json:"references"`
	Resolved   int                 `json:"resolved"`
	Unresolved []bib.Unresolved    `json:"unresolved,omitempty"`
	Verified   []bib.Verified      `json:"verified,omitempty"`
}

// JSON implements cmdutil.Result.
func (r BibResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r BibResult) Human() string {
	var b strings.Builder
	b.WriteString(r.Export.Human())

	unresolvedCount := len(r.Unresolved)
	if len(r.Verified) > 0 {
		unresolvedCount = len(r.Verified)
	}
	fmt.Fprintf(&b, "    %s %d reference(s) in %d file(s): %d resolved, %d unresolved\n",
		uikit.TUI.Dim().Render("·"),
		r.References, len(r.Files), r.Resolved, unresolvedCount)

	if len(r.Verified) > 0 {
		writeVerified(&b, r.Verified)
		return b.String()
	}
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

// verifyGroups orders the --verify partition by how much the reader should
// care: a citation that resolves nowhere is a probable fabrication and leads;
// "we couldn't check" trails.
var verifyGroups = []struct {
	status bib.VerifyStatus
	label  string
}{
	{bib.StatusNotFound, "resolves nowhere — no citation index or DOI registry has this (likely fabricated)"},
	{bib.StatusExternal, "not in library — real work, add it"},
	{bib.StatusAmbiguous, "ambiguous — more than one library item matches"},
	{bib.StatusError, "lookup failed — standing unknown, re-run"},
	{bib.StatusUnchecked, "unchecked — no identifier an index can resolve"},
}

func writeVerified(b *strings.Builder, verified []bib.Verified) {
	byStatus := lo.GroupBy(verified, func(v bib.Verified) bib.VerifyStatus { return v.Status })
	for _, g := range verifyGroups {
		group := byStatus[g.status]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(b, "\n  %s %s (%d):\n", uikit.SymWarn, g.label, len(group))
		for _, v := range group {
			fmt.Fprintf(b, "    %s %s %s\n",
				uikit.TUI.Dim().Render("-"),
				v.Raw,
				uikit.TUI.Dim().Render("("+string(v.Kind)+")"))
			if v.Match != nil {
				fmt.Fprintf(b, "      %s\n", uikit.TUI.Dim().Render(matchLine(v.Match)))
			}
			if v.Error != "" {
				fmt.Fprintf(b, "      %s\n", uikit.TUI.Dim().Render(v.Error))
			}
			if v.Fix != "" {
				fmt.Fprintf(b, "      %s %s\n", uikit.TUI.Dim().Render("fix:"), v.Fix)
			}
		}
	}
}

// matchLine renders the upstream evidence behind an external match — the
// reader needs enough to recognize the work without opening a browser.
func matchLine(m *bib.Match) string {
	parts := []string{m.Title}
	if m.Year > 0 {
		parts = append(parts, fmt.Sprintf("(%d)", m.Year))
	}
	if m.Venue != "" {
		parts = append(parts, "· "+m.Venue)
	}
	line := strings.TrimSpace(strings.Join(lo.Compact(parts), " "))
	if m.Retracted {
		line += " · RETRACTED"
	}
	return line
}
