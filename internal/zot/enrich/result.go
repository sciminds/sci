package enrich

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// FromMissingResult is the cmdutil.Result shell around the outputs of
// `zot doctor missing --enrich`. Lives here (not in the parent zot package)
// because the orchestrator imports api, and zot imports api — wiring results
// through zot would reopen the cycle this subpackage exists to break.
//
// Covers both dry-run (Apply nil) and applied (Apply populated) — one shape,
// branch on Applied.
type FromMissingResult struct {
	Targets []Target     `json:"targets"`
	Skipped []Skipped    `json:"skipped,omitempty"`
	Apply   *ApplyResult `json:"apply,omitempty"`
	Applied bool         `json:"applied"` // true = --apply was set
}

func (r FromMissingResult) JSON() any { return r }

func (r FromMissingResult) Human() string {
	var b strings.Builder

	header := "Missing-field enrichment (dry-run)"
	if r.Applied {
		header = "Missing-field enrichment (applied)"
	}
	fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.TextBlueBold().Render(header))

	for _, t := range r.Targets {
		writeTargetBlock(&b, t)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\n  %s skipped %d item(s):\n", uikit.TUI.Dim().Render("·"), len(r.Skipped))
		for _, s := range r.Skipped {
			title := s.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(&b, "    %s  %s — %s\n",
				uikit.TUI.TextBlue().Render(s.ItemKey),
				uikit.TUI.Dim().Render(title),
				s.Reason,
			)
		}
	}

	switch {
	case !r.Applied:
		fmt.Fprintf(&b, "\n  %s %d fillable, %d skipped — rerun with --apply to write\n",
			uikit.SymArrow, len(r.Targets), len(r.Skipped))
	case r.Apply == nil:
		fmt.Fprintf(&b, "\n  %s no targets to apply\n", uikit.SymArrow)
	default:
		fmt.Fprintf(&b, "\n  %s applied %d, failed %d\n", uikit.SymArrow, r.Apply.Applied, r.Apply.Failed)
		if len(r.Apply.Errors) > 0 {
			for _, k := range sortedKeys(r.Apply.Errors) {
				fmt.Fprintf(&b, "    %s %s: %s\n",
					uikit.TUI.TextRed().Render("✗"),
					uikit.TUI.TextBlue().Render(k),
					r.Apply.Errors[k],
				)
			}
		}
	}
	return b.String()
}

func writeTargetBlock(b *strings.Builder, t Target) {
	title := t.Title
	if title == "" {
		title = "(untitled)"
	}
	fmt.Fprintf(b, "  %s  %s %s\n",
		uikit.TUI.TextBlue().Render(t.ItemKey),
		title,
		uikit.TUI.Dim().Render("← "+t.OpenAlexID),
	)
	for _, k := range sortedKeys(t.Fills) {
		fmt.Fprintf(b, "    %s %s: %s\n",
			uikit.TUI.TextGreen().Render("+"),
			k,
			uikit.TUI.Dim().Render(t.Fills[k]),
		)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
