package fix

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/ui"
)

// CitekeyFixResult is the cmdutil.Result shell around a
// *CitekeyResult. Lives in the fix package (not its parent `zot`)
// because the orchestrator is in fix and wiring results through zot
// would recreate the import cycle we split this package out to avoid
// (api imports zot). The CLI command surface imports fix directly.
type CitekeyFixResult struct {
	Result *CitekeyResult `json:"result"`
	Limit  int            `json:"-"`
}

// JSON satisfies cmdutil.Result.
func (r CitekeyFixResult) JSON() any { return r.Result }

// Human renders either a dry-run preview ("would patch N items, here's
// the diff") or an applied report ("wrote N items, here's the per-item
// outcome"). The shape is intentionally the same in both modes so
// diffing a dry-run against its apply is trivial for the user.
func (r CitekeyFixResult) Human() string {
	if r.Result == nil {
		return ""
	}
	var b strings.Builder

	header := "Cite-key fix (dry-run)"
	if r.Result.Applied {
		header = "Cite-key fix (applied)"
	}
	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render(header))

	total := len(r.Result.Targets)
	if total == 0 {
		fmt.Fprintf(&b, "  %s nothing to fix — every stored cite-key already matches the spec\n", ui.SymOK)
		return b.String()
	}

	// Per-bucket summary line. Fixed ordering (matches planner rank)
	// so the same library always renders identically.
	order := []string{"invalid", "collision", "non-canonical", "unstored"}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		n, ok := r.Result.Totals.PerReason[k]
		if !ok || n == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, k))
	}
	fmt.Fprintf(&b, "  %s %d target(s): %s\n\n",
		ui.TUI.Dim().Render("·"), total,
		strings.Join(parts, ", "),
	)

	// Index outcomes by item key so the per-row render can attach a
	// success/fail glyph + error message when applicable.
	outcomeByKey := lo.KeyBy(r.Result.Outcomes, func(oc CitekeyOutcome) string {
		return oc.ItemKey
	})

	show := r.Result.Targets
	truncated := 0
	if r.Limit > 0 && len(show) > r.Limit {
		truncated = len(show) - r.Limit
		show = show[:r.Limit]
	}

	fmt.Fprintf(&b, "  %s\n", ui.TUI.Dim().Render("targets:"))
	for _, tg := range show {
		icon := ui.TUI.Dim().Render("·")
		if r.Result.Applied {
			if oc, ok := outcomeByKey[tg.ItemKey]; ok {
				if oc.Applied {
					icon = ui.SymOK
				} else {
					icon = ui.SymFail
				}
			}
		}
		old := tg.OldKey
		if old == "" {
			old = ui.TUI.Dim().Render("(none)")
		}
		fmt.Fprintf(&b, "    %s  %s  %-13s %s %s %s\n",
			icon,
			ui.TUI.Accent().Render(tg.ItemKey),
			ui.TUI.Warn().Render(tg.Reason),
			ui.TUI.Dim().Render(old),
			ui.TUI.Dim().Render("→"),
			tg.NewKey,
		)
		if r.Result.Applied {
			if oc, ok := outcomeByKey[tg.ItemKey]; ok && !oc.Applied && oc.Error != "" {
				fmt.Fprintf(&b, "      %s %s\n",
					ui.SymFail, ui.TUI.Fail().Render(oc.Error))
			}
		}
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "    %s %d more (use --limit 0 or --json for all)\n",
			ui.TUI.Dim().Render("…"), truncated)
	}

	if r.Result.Applied {
		fmt.Fprintf(&b, "\n  %s %d succeeded  %s %d failed\n",
			ui.SymOK, r.Result.Totals.Succeeded,
			ui.SymFail, r.Result.Totals.Failed,
		)
	} else {
		fmt.Fprintf(&b, "\n  %s dry-run only — pass %s to write through the Zotero Web API\n",
			ui.SymArrow, ui.TUI.Accent().Render("--apply"),
		)
	}
	return b.String()
}
