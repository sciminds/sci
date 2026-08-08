package backfill

// Result rendering lives here, not in the parent zot package: backfill
// imports api, and api imports zot, so a result type over there would
// recreate the very import cycle enrich and fix were split out to avoid.

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// CLIResult reports what `zot item update --from-json` wrote.
//
// Skipped is reported separately from Failed and separately from Applied
// because it means something specific and benign: the item gained a DOI
// from a better source since the plan was built, so the plan's premise no
// longer held and nothing was written. Folding it into failures would make
// a correct refusal look like an outage; folding it into applied would
// claim a write that did not happen.
type CLIResult struct {
	Plan    string  `json:"plan"`
	Planned int     `json:"planned"`
	Result  *Result `json:"result,omitempty"`
}

// JSON implements cmdutil.Result.
func (r CLIResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r CLIResult) Human() string {
	var b strings.Builder
	if r.Result == nil || r.Planned == 0 {
		fmt.Fprintf(&b, "  %s %s carries no patches — nothing to apply\n", uikit.SymOK, r.Plan)
		return b.String()
	}
	fmt.Fprintf(&b, "  %s applied %d of %d planned DOI writes\n",
		uikit.SymOK, r.Result.Applied, r.Planned)
	if r.Result.Skipped > 0 {
		fmt.Fprintf(&b, "    %s %d item(s) already had a DOI and were left alone\n",
			uikit.TUI.Dim().Render("skipped:"), r.Result.Skipped)
		fmt.Fprintf(&b, "      a DOI that arrived from anywhere else outranks an inferred one\n")
	}
	if r.Result.Failed > 0 {
		fmt.Fprintf(&b, "    %s %d item(s) failed\n",
			uikit.TUI.Dim().Render("failed:"), r.Result.Failed)
		for k, e := range r.Result.Errors {
			fmt.Fprintf(&b, "      %s: %s\n", k, e)
		}
	}
	return b.String()
}
