package zot

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/hygiene"
	"github.com/sciminds/cli/internal/zot/local"
)

// DoctorChecks is the full ordered list of checks Doctor knows how to
// run. Order is deliberate: cheap and structural first, slow (duplicates)
// last so the user sees the other three land immediately.
var DoctorChecks = []string{"invalid", "missing", "orphans", "duplicates", "citekeys"}

// DoctorOptions configures Doctor. Checks narrows the run to a subset
// (empty = all). Deep flips the slow/accurate paths inside the checks
// that support it: fuzzy title matching for duplicates and the opt-in
// noisy orphan kinds (uncollected-item). It does NOT enable --check-files
// for missing attachments — that remains a deliberate per-command opt-in
// because it stats every attachment on disk.
type DoctorOptions struct {
	Checks []string
	Deep   bool
}

// DoctorTotals is the summary tally rolled up from every report. Findings
// are counted by severity; Clusters is the distinct count of duplicate
// groups (not member items).
type DoctorTotals struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
	Clusters int `json:"clusters"`
}

// DoctorResult is the cmdutil.Result for `zot doctor`. It owns the
// aggregated per-check reports plus a pre-computed totals block so
// scripts can answer "did anything break?" with one field lookup.
type DoctorResult struct {
	Scanned int                        `json:"scanned"`
	Reports map[string]*hygiene.Report `json:"reports"`
	Order   []string                   `json:"-"`
	Totals  DoctorTotals               `json:"totals"`
	Deep    bool                       `json:"deep"`
}

// Doctor runs each selected check in DoctorChecks order and aggregates
// the results. Each check uses its default options, except that Deep=true
// enables fuzzy title matching for duplicates and the uncollected-item
// orphan sub-check.
//
// The caller owns both db and cfg because orphans needs cfg.DataDir to
// resolve attachment paths (even though --check-files stays off here).
func Doctor(db local.Reader, cfg *Config, opts DoctorOptions) (*DoctorResult, error) {
	run := selectedChecks(opts.Checks)
	out := &DoctorResult{
		Reports: map[string]*hygiene.Report{},
		Deep:    opts.Deep,
	}

	for _, check := range DoctorChecks {
		if !run[check] {
			continue
		}
		var (
			rep *hygiene.Report
			err error
		)
		switch check {
		case "invalid":
			rep, err = hygiene.Invalid(db, nil) // all fields
		case "missing":
			rep, err = hygiene.Missing(db, nil)
		case "orphans":
			kinds := defaultDoctorOrphanKinds()
			if opts.Deep {
				kinds = append(kinds, hygiene.OrphanUncollectedItem)
			}
			rep, err = hygiene.Orphans(db, hygiene.OrphansOptions{
				Kinds:   kinds,
				DataDir: cfg.DataDir,
				// CheckFiles stays off — too expensive for an aggregate run.
			})
		case "duplicates":
			rep, err = hygiene.Duplicates(db, hygiene.DuplicatesOptions{
				Strategy:  hygiene.StrategyBoth,
				Fuzzy:     opts.Deep,
				Threshold: 0.85,
			})
		case "citekeys":
			rep, err = hygiene.Citekeys(db)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", check, err)
		}
		out.Reports[check] = rep
		out.Order = append(out.Order, check)
		if rep.Scanned > out.Scanned {
			out.Scanned = rep.Scanned
		}
		counts := rep.CountBySeverity()
		out.Totals.Errors += counts[hygiene.SevError]
		out.Totals.Warnings += counts[hygiene.SevWarn]
		out.Totals.Info += counts[hygiene.SevInfo]
		out.Totals.Clusters += len(rep.Clusters)
	}

	return out, nil
}

// defaultDoctorOrphanKinds mirrors hygiene.defaultOrphanKinds — the safe,
// fast subset. Duplicated here rather than exported because doctor may
// want to extend this set independently later (Deep adds uncollected-item).
func defaultDoctorOrphanKinds() []hygiene.OrphanKind {
	return []hygiene.OrphanKind{
		hygiene.OrphanEmptyCollection,
		hygiene.OrphanStandaloneAttachment,
		hygiene.OrphanStandaloneNote,
		hygiene.OrphanUnusedTag,
	}
}

// selectedChecks resolves a --check list into a set. Empty input means
// "run everything in DoctorChecks."
func selectedChecks(names []string) map[string]bool {
	out := map[string]bool{}
	if len(names) == 0 {
		for _, c := range DoctorChecks {
			out[c] = true
		}
		return out
	}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// ParseDoctorCheck validates a --check value. Exported so the CLI parser
// can reject unknown names before opening the DB.
func ParseDoctorCheck(s string) (string, error) {
	s = strings.TrimSpace(s)
	c, found := lo.Find(DoctorChecks, func(c string) bool {
		return c == s
	})
	if found {
		return c, nil
	}
	return "", fmt.Errorf("unknown check %q (want one of: %s)", s, strings.Join(DoctorChecks, ", "))
}

// JSON satisfies cmdutil.Result.
func (r *DoctorResult) JSON() any { return r }

// Human renders the dashboard: per-check summary table, then a totals
// footer, then a pointer at the per-check commands for drilldown. Doctor
// does NOT dump individual findings — users run `zot <check>` to drill in.
func (r *DoctorResult) Human() string {
	if r == nil {
		return ""
	}
	var b strings.Builder

	fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.TextBlueBold().Render("Library Health"))
	deepLabel := "fast mode"
	if r.Deep {
		deepLabel = "deep mode"
	}
	fmt.Fprintf(&b, "  %s %d items scanned  %s %s\n\n",
		uikit.TUI.Dim().Render("·"),
		r.Scanned,
		uikit.TUI.Dim().Render("·"),
		uikit.TUI.Dim().Render(deepLabel),
	)

	for _, name := range r.Order {
		rep := r.Reports[name]
		if rep == nil {
			continue
		}
		line := doctorSummaryLine(name, rep)
		fmt.Fprintf(&b, "    %s  %-12s %s\n", doctorStatusGlyph(rep), name, line)
	}

	errSym, errText := uikit.SymFail, uikit.TUI.Fail().Render(fmt.Sprintf("%d error", r.Totals.Errors))
	if r.Totals.Errors == 0 {
		errSym, errText = uikit.SymOK, uikit.TUI.Pass().Render("0 error")
	}
	warnSym, warnText := uikit.SymWarn, uikit.TUI.Warn().Render(fmt.Sprintf("%d warn", r.Totals.Warnings))
	if r.Totals.Warnings == 0 {
		warnSym, warnText = uikit.SymOK, uikit.TUI.Pass().Render("0 warn")
	}
	fmt.Fprintf(&b, "\n  %s %s  %s %s  %s %s\n",
		errSym, errText,
		warnSym, warnText,
		uikit.TUI.Dim().Render("·"), uikit.TUI.Dim().Render(fmt.Sprintf("%d info", r.Totals.Info)),
	)

	if r.Totals.Errors == 0 && r.Totals.Warnings == 0 && r.Totals.Clusters == 0 {
		fmt.Fprintf(&b, "\n  %s library looks healthy\n", uikit.SymOK)
		return b.String()
	}

	fmt.Fprintf(&b, "\n  %s run %s for per-finding detail\n",
		uikit.SymArrow,
		uikit.TUI.Dim().Render("`zot doctor invalid`, `zot doctor missing`, `zot doctor orphans`, `zot doctor duplicates`, `zot doctor citekeys`"),
	)
	return b.String()
}

// doctorSummaryLine produces the one-line per-check summary ("2 errors,
// 17 warnings" / "8 clusters"). Keeps the per-check prose co-located
// with the check name so adding a new check only touches one switch.
func doctorSummaryLine(check string, rep *hygiene.Report) string {
	counts := rep.CountBySeverity()
	switch check {
	case "citekeys":
		// Show the per-bucket breakdown inline so the aggregate dashboard
		// doesn't require drilling into the sub-command to see what's
		// going on. A clean library has no findings; a BBT library has
		// plenty of non-canonical.
		stats, _ := rep.Stats.(hygiene.CitekeysStats)
		if stats.Stored == 0 && stats.Unstored > 0 {
			return uikit.TUI.Dim().Render(fmt.Sprintf("%d unstored (will synthesize)", stats.Unstored))
		}
		if stats.Invalid == 0 && stats.NonCanonical == 0 && stats.Collisions == 0 {
			return uikit.TUI.Dim().Render(fmt.Sprintf("%d canonical", stats.Valid))
		}
		parts := []string{}
		if stats.Invalid > 0 {
			parts = append(parts, uikit.TUI.Fail().Render(fmt.Sprintf("%d invalid", stats.Invalid)))
		}
		if stats.Collisions > 0 {
			parts = append(parts, uikit.TUI.Fail().Render(fmt.Sprintf("%d collision", stats.Collisions)))
		}
		if stats.NonCanonical > 0 {
			parts = append(parts, uikit.TUI.Warn().Render(fmt.Sprintf("%d non-canonical", stats.NonCanonical)))
		}
		return strings.Join(parts, "  ")
	case "duplicates":
		stats, _ := rep.Stats.(hygiene.DuplicatesStats)
		if stats.ClusterCount == 0 {
			return uikit.TUI.Dim().Render("no duplicate clusters")
		}
		label := fmt.Sprintf("%d cluster", stats.ClusterCount)
		if stats.ClusterCount != 1 {
			label += "s"
		}
		label += fmt.Sprintf(", %d item", stats.ItemsInGroups)
		if stats.ItemsInGroups != 1 {
			label += "s"
		}
		return label
	default:
		if counts[hygiene.SevError] == 0 && counts[hygiene.SevWarn] == 0 && counts[hygiene.SevInfo] == 0 {
			return uikit.TUI.Dim().Render("clean")
		}
		parts := []string{}
		if n := counts[hygiene.SevError]; n > 0 {
			parts = append(parts, uikit.TUI.Fail().Render(fmt.Sprintf("%d error", n)))
		}
		if n := counts[hygiene.SevWarn]; n > 0 {
			parts = append(parts, uikit.TUI.Warn().Render(fmt.Sprintf("%d warn", n)))
		}
		if n := counts[hygiene.SevInfo]; n > 0 {
			parts = append(parts, uikit.TUI.Dim().Render(fmt.Sprintf("%d info", n)))
		}
		return strings.Join(parts, "  ")
	}
}

// doctorStatusGlyph picks the leading marker for a per-check row: fail
// on any error, warn on any warning, dim dot otherwise.
func doctorStatusGlyph(rep *hygiene.Report) string {
	counts := rep.CountBySeverity()
	if counts[hygiene.SevError] > 0 {
		return uikit.SymFail
	}
	if counts[hygiene.SevWarn] > 0 || len(rep.Clusters) > 0 {
		return uikit.SymWarn
	}
	return uikit.SymOK
}
