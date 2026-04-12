package zot

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/zot/hygiene"
)

// MissingResult wraps a hygiene.Report from the Missing check plus the
// rendering knobs the command layer picks up from flags.
type MissingResult struct {
	Report *hygiene.Report `json:"report"`
	Limit  int             `json:"-"` // 0 = show all findings
}

func (r MissingResult) JSON() any { return r.Report }

func (r MissingResult) Human() string {
	if r.Report == nil {
		return ""
	}
	var b strings.Builder

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render("Missing-field coverage"))
	fmt.Fprintf(&b, "  %s %d items scanned\n\n",
		ui.TUI.Dim().Render("·"), r.Report.Scanned)

	if stats, ok := r.Report.Stats.(hygiene.MissingStats); ok {
		for _, c := range stats.Coverage {
			bar := coverageBar(c.PercentPresent, 20)
			fmt.Fprintf(&b, "    %-10s %s  %5d / %-5d  %5.1f%%\n",
				c.Field, bar, c.Present, stats.Scanned, c.PercentPresent,
			)
		}
	}

	if len(r.Report.Findings) == 0 {
		fmt.Fprintf(&b, "\n  %s no missing fields\n", ui.SymOK)
		return b.String()
	}

	counts := r.Report.CountBySeverity()
	fmt.Fprintf(&b, "\n  %s %s  %s %s  %s %s\n",
		ui.SymFail, ui.TUI.Fail().Render(fmt.Sprintf("%d error", counts[hygiene.SevError])),
		ui.SymWarn, ui.TUI.Warn().Render(fmt.Sprintf("%d warn", counts[hygiene.SevWarn])),
		ui.TUI.Dim().Render("·"), ui.TUI.Dim().Render(fmt.Sprintf("%d info", counts[hygiene.SevInfo])),
	)

	// Sort findings by severity desc so errors lead. Stable-sort preserves
	// the existing (ItemKey, Kind) secondary order from Missing().
	sorted := make([]hygiene.Finding, len(r.Report.Findings))
	copy(sorted, r.Report.Findings)
	stableSortBySeverity(sorted)

	show := sorted
	truncated := 0
	if r.Limit > 0 && len(show) > r.Limit {
		truncated = len(show) - r.Limit
		show = show[:r.Limit]
	}

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Dim().Render("findings:"))
	for _, f := range show {
		title := f.Title
		if title == "" {
			title = ui.TUI.Dim().Render("(untitled)")
		}
		fmt.Fprintf(&b, "    %s  %s %-9s %s\n",
			ui.TUI.Accent().Render(f.ItemKey),
			severityIcon(f.Severity),
			styleSeverity(f.Severity, f.Kind),
			title,
		)
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "    %s %d more (use --limit 0 or --json for all)\n",
			ui.TUI.Dim().Render("…"), truncated)
	}
	fmt.Fprintf(&b, "\n  %s %d finding(s)\n", ui.SymArrow, len(r.Report.Findings))
	return b.String()
}

func severityIcon(s hygiene.Severity) string {
	switch s {
	case hygiene.SevError:
		return ui.SymFail
	case hygiene.SevWarn:
		return ui.SymWarn
	default:
		return ui.TUI.Dim().Render("·")
	}
}

func styleSeverity(s hygiene.Severity, text string) string {
	switch s {
	case hygiene.SevError:
		return ui.TUI.Fail().Render(text)
	case hygiene.SevWarn:
		return ui.TUI.Warn().Render(text)
	default:
		return ui.TUI.Dim().Render(text)
	}
}

// stableSortBySeverity reorders findings so SevError leads, then SevWarn,
// then SevInfo. Ties keep the input order (the Missing() check already
// sorts by ItemKey/Kind).
func stableSortBySeverity(fs []hygiene.Finding) {
	slices.SortStableFunc(fs, func(a, b hygiene.Finding) int {
		return cmp.Compare(b.Severity, a.Severity) // descending
	})
}

// InvalidResult wraps a hygiene.Report from the Invalid check. Limit
// caps the human-mode findings list; JSON always returns everything.
type InvalidResult struct {
	Report *hygiene.Report `json:"report"`
	Limit  int             `json:"-"`
}

func (r InvalidResult) JSON() any { return r.Report }

func (r InvalidResult) Human() string {
	if r.Report == nil {
		return ""
	}
	var b strings.Builder

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render("Field-value validation"))
	fmt.Fprintf(&b, "  %s %d field values scanned\n\n",
		ui.TUI.Dim().Render("·"), r.Report.Scanned)

	if stats, ok := r.Report.Stats.(hygiene.InvalidStats); ok {
		for _, c := range stats.PerField {
			bar := coverageBar(c.PercentGood, 20)
			fmt.Fprintf(&b, "    %-10s %s  %5d / %-5d good  %5.1f%%  %s\n",
				c.Field, bar,
				c.Scanned-c.Bad, c.Scanned, c.PercentGood,
				ui.TUI.Dim().Render(fmt.Sprintf("(%d bad)", c.Bad)),
			)
		}
	}

	if len(r.Report.Findings) == 0 {
		fmt.Fprintf(&b, "\n  %s no invalid values\n", ui.SymOK)
		return b.String()
	}

	counts := r.Report.CountBySeverity()
	fmt.Fprintf(&b, "\n  %s %s  %s %s\n",
		ui.SymWarn, ui.TUI.Warn().Render(fmt.Sprintf("%d warn", counts[hygiene.SevWarn])),
		ui.TUI.Dim().Render("·"), ui.TUI.Dim().Render(fmt.Sprintf("%d info", counts[hygiene.SevInfo])),
	)

	show := r.Report.Findings
	truncated := 0
	if r.Limit > 0 && len(show) > r.Limit {
		truncated = len(show) - r.Limit
		show = show[:r.Limit]
	}

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Dim().Render("findings:"))
	for _, f := range show {
		title := f.Title
		if title == "" {
			title = ui.TUI.Dim().Render("(untitled)")
		}
		fmt.Fprintf(&b, "    %s  %s %s\n",
			ui.TUI.Accent().Render(f.ItemKey),
			styleSeverity(f.Severity, f.Message),
			title,
		)
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "    %s %d more (use --limit 0 or --json for all)\n",
			ui.TUI.Dim().Render("…"), truncated)
	}
	fmt.Fprintf(&b, "\n  %s %d finding(s)\n", ui.SymArrow, len(r.Report.Findings))
	return b.String()
}

// OrphansResult wraps a hygiene.Report from the Orphans check. Limit
// caps the human-mode findings list per sub-kind; JSON always returns
// every finding.
type OrphansResult struct {
	Report *hygiene.Report `json:"report"`
	Limit  int             `json:"-"`
}

func (r OrphansResult) JSON() any { return r.Report }

func (r OrphansResult) Human() string {
	if r.Report == nil {
		return ""
	}
	var b strings.Builder

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render("Orphan scan"))
	stats, _ := r.Report.Stats.(hygiene.OrphansStats)
	fmt.Fprintf(&b, "  %s %d total orphan(s) across %d kind(s)\n\n",
		ui.TUI.Dim().Render("·"),
		stats.Total,
		len(stats.CountsByKind),
	)

	// Summary table: one row per kind that was run, in AllOrphanKinds
	// order so the output is stable.
	for _, k := range hygiene.AllOrphanKinds {
		count, ran := stats.CountsByKind[string(k)]
		if !ran {
			continue
		}
		marker := ui.TUI.Dim().Render("·")
		if count > 0 {
			switch k {
			case hygiene.OrphanMissingFile:
				marker = ui.SymFail
			case hygiene.OrphanStandaloneAttachment:
				marker = ui.SymWarn
			default:
				marker = ui.TUI.Accent().Render("●")
			}
		}
		fmt.Fprintf(&b, "    %s  %-24s %d\n", marker, string(k), count)
	}

	if stats.Total == 0 {
		fmt.Fprintf(&b, "\n  %s no orphans found\n", ui.SymOK)
		return b.String()
	}

	// Group findings by kind for the detail section.
	groups := map[string][]hygiene.Finding{}
	for _, f := range r.Report.Findings {
		groups[f.Kind] = append(groups[f.Kind], f)
	}

	for _, k := range hygiene.AllOrphanKinds {
		gs, ok := groups[string(k)]
		if !ok || len(gs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n  %s %s\n",
			ui.TUI.Dim().Render("─"),
			styleSeverity(severityForOrphanKind(k), string(k)),
		)
		show := gs
		truncated := 0
		if r.Limit > 0 && len(show) > r.Limit {
			truncated = len(show) - r.Limit
			show = show[:r.Limit]
		}
		for _, f := range show {
			label := f.Title
			if label == "" {
				label = ui.TUI.Dim().Render("(none)")
			}
			if f.ItemKey != "" {
				fmt.Fprintf(&b, "    %s  %s\n",
					ui.TUI.Accent().Render(f.ItemKey), label)
			} else {
				fmt.Fprintf(&b, "    %s\n", label)
			}
		}
		if truncated > 0 {
			fmt.Fprintf(&b, "    %s %d more (use --limit 0 or --json for all)\n",
				ui.TUI.Dim().Render("…"), truncated)
		}
	}

	fmt.Fprintf(&b, "\n  %s %d finding(s)\n", ui.SymArrow, stats.Total)
	return b.String()
}

// severityForOrphanKind mirrors hygiene.severityForOrphan but is
// duplicated here because the hygiene function is unexported. Keeping
// the switch local to the renderer means the severity mapping is in
// one file per concern.
func severityForOrphanKind(k hygiene.OrphanKind) hygiene.Severity {
	switch k {
	case hygiene.OrphanMissingFile:
		return hygiene.SevError
	case hygiene.OrphanStandaloneAttachment:
		return hygiene.SevWarn
	default:
		return hygiene.SevInfo
	}
}

// DuplicatesResult wraps a hygiene.Report from the Duplicates check.
// Limit caps the number of clusters printed by Human() — JSON always
// returns every cluster.
type DuplicatesResult struct {
	Report *hygiene.Report `json:"report"`
	Limit  int             `json:"-"`
}

func (r DuplicatesResult) JSON() any { return r.Report }

func (r DuplicatesResult) Human() string {
	if r.Report == nil {
		return ""
	}
	var b strings.Builder
	stats, _ := r.Report.Stats.(hygiene.DuplicatesStats)

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render("Duplicate clusters"))
	fuzzLabel := "fuzzy=off"
	if stats.Fuzzy {
		fuzzLabel = fmt.Sprintf("fuzzy=on threshold=%.2f", stats.Threshold)
	}
	fmt.Fprintf(&b, "  %s %d items scanned  %s strategy=%s  %s %s\n",
		ui.TUI.Dim().Render("·"),
		stats.Scanned,
		ui.TUI.Dim().Render("·"),
		stats.Strategy,
		ui.TUI.Dim().Render("·"),
		fuzzLabel,
	)

	if len(r.Report.Clusters) == 0 {
		fmt.Fprintf(&b, "\n  %s no duplicate clusters found\n", ui.SymOK)
		return b.String()
	}

	show := r.Report.Clusters
	truncated := 0
	if r.Limit > 0 && len(show) > r.Limit {
		truncated = len(show) - r.Limit
		show = show[:r.Limit]
	}

	b.WriteString("\n")
	for i, c := range show {
		if i > 0 {
			b.WriteString("\n")
		}
		scoreStr := fmt.Sprintf("%.2f", c.Score)
		fmt.Fprintf(&b, "  %s %s %s\n",
			matchTypeBadge(c.MatchType),
			ui.TUI.Dim().Render("score"),
			ui.TUI.Accent().Render(scoreStr),
		)
		for _, m := range c.Members {
			title := m.Title
			if title == "" {
				title = ui.TUI.Dim().Render("(untitled)")
			}
			pdfMarker := ""
			if m.PDFCount > 0 {
				pdfMarker = " " + ui.TUI.Dim().Render("[pdf]")
			}
			year := ""
			if d := cleanDate(m.Date); len(d) >= 4 {
				year = " " + ui.TUI.Dim().Render("("+d[:4]+")")
			}
			fmt.Fprintf(&b, "    %s  %s%s%s\n",
				ui.TUI.Accent().Render(m.Key),
				title,
				year,
				pdfMarker,
			)
			if m.DOI != "" {
				fmt.Fprintf(&b, "      %s %s\n", ui.TUI.Dim().Render("doi:"), m.DOI)
			}
		}
	}

	if truncated > 0 {
		fmt.Fprintf(&b, "\n    %s %d more cluster(s) (use --limit 0 or --json for all)\n",
			ui.TUI.Dim().Render("…"), truncated)
	}
	fmt.Fprintf(&b, "\n  %s %d cluster(s), %d item(s)\n",
		ui.SymArrow, stats.ClusterCount, stats.ItemsInGroups,
	)
	return b.String()
}

// CitekeysResult wraps a hygiene.Report from the Citekeys check. Limit
// caps the human-mode findings list; JSON always returns everything.
type CitekeysResult struct {
	Report *hygiene.Report `json:"report"`
	Limit  int             `json:"-"`
}

func (r CitekeysResult) JSON() any { return r.Report }

func (r CitekeysResult) Human() string {
	if r.Report == nil {
		return ""
	}
	var b strings.Builder

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render("Cite-key validation"))
	stats, _ := r.Report.Stats.(hygiene.CitekeysStats)
	fmt.Fprintf(&b, "  %s %d items scanned  %s %d stored  %s %d unstored\n\n",
		ui.TUI.Dim().Render("·"), stats.Scanned,
		ui.TUI.Dim().Render("·"), stats.Stored,
		ui.TUI.Dim().Render("·"), stats.Unstored,
	)

	// Coverage breakdown, scored against stored keys so Unstored items
	// don't pollute the denominator. If nothing is stored yet, show the
	// row but avoid a divide-by-zero.
	pctValid := 0.0
	if stats.Stored > 0 {
		pctValid = 100 * float64(stats.Valid) / float64(stats.Stored)
	}
	bar := coverageBar(pctValid, 20)
	fmt.Fprintf(&b, "    canonical  %s  %5d / %-5d  %5.1f%%\n",
		bar, stats.Valid, stats.Stored, pctValid,
	)
	fmt.Fprintf(&b, "    %-10s %s %d non-canonical  %s %d invalid  %s %d collisions\n",
		" ",
		ui.TUI.Warn().Render("·"), stats.NonCanonical,
		ui.TUI.Fail().Render("·"), stats.Invalid,
		ui.TUI.Fail().Render("·"), stats.Collisions,
	)

	if len(r.Report.Findings) == 0 {
		fmt.Fprintf(&b, "\n  %s every stored cite-key is canonical\n", ui.SymOK)
		return b.String()
	}

	counts := r.Report.CountBySeverity()
	fmt.Fprintf(&b, "\n  %s %s  %s %s\n",
		ui.SymFail, ui.TUI.Fail().Render(fmt.Sprintf("%d error", counts[hygiene.SevError])),
		ui.SymWarn, ui.TUI.Warn().Render(fmt.Sprintf("%d warn", counts[hygiene.SevWarn])),
	)

	sorted := make([]hygiene.Finding, len(r.Report.Findings))
	copy(sorted, r.Report.Findings)
	stableSortBySeverity(sorted)

	show := sorted
	truncated := 0
	if r.Limit > 0 && len(show) > r.Limit {
		truncated = len(show) - r.Limit
		show = show[:r.Limit]
	}

	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Dim().Render("findings:"))
	for _, f := range show {
		title := f.Title
		if title == "" {
			title = ui.TUI.Dim().Render("(untitled)")
		}
		fmt.Fprintf(&b, "    %s  %s %-13s %s\n",
			ui.TUI.Accent().Render(f.ItemKey),
			severityIcon(f.Severity),
			styleSeverity(f.Severity, f.Kind),
			title,
		)
		fmt.Fprintf(&b, "      %s\n", ui.TUI.Dim().Render(f.Message))
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "    %s %d more (use --limit 0 or --json for all)\n",
			ui.TUI.Dim().Render("…"), truncated)
	}
	fmt.Fprintf(&b, "\n  %s %d finding(s)\n", ui.SymArrow, len(r.Report.Findings))
	return b.String()
}

// matchTypeBadge colorizes the match-type label so DOI matches (strongest)
// pop visually above title-fuzzy matches (weakest).
func matchTypeBadge(kind string) string {
	switch kind {
	case "doi":
		return ui.TUI.Pass().Render("[" + kind + "]")
	case "title-exact":
		return ui.TUI.Accent().Render("[" + kind + "]")
	case "title-fuzzy":
		return ui.TUI.Warn().Render("[" + kind + "]")
	default:
		return ui.TUI.Dim().Render("[" + kind + "]")
	}
}

// coverageBar renders a unicode block-meter of width cells for a percentage
// in [0,100]. Used only by the human renderer — kept here so the result
// type is self-contained.
func coverageBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct/100*float64(width) + 0.5)
	var b strings.Builder
	b.WriteString(ui.TUI.Accent().Render(strings.Repeat("█", filled)))
	b.WriteString(ui.TUI.Dim().Render(strings.Repeat("░", width-filled)))
	return b.String()
}
