package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/content"
)

// ContentBuildResult is returned by `zot content build`.
type ContentBuildResult struct {
	Path     string            `json:"path"`
	DryRun   bool              `json:"dry_run,omitempty"`
	Planned  int               `json:"planned"`
	Added    int               `json:"added"`
	Updated  int               `json:"updated"`
	Deleted  int               `json:"deleted"`
	Skipped  int               `json:"skipped"`
	BySource map[string]int    `json:"by_source,omitempty"`
	Failed   map[string]string `json:"failed,omitempty"`
	Duration time.Duration     `json:"duration_ns,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ContentBuildResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ContentBuildResult) Human() string {
	var b strings.Builder
	if r.DryRun {
		if r.Planned == 0 {
			return fmt.Sprintf("  %s content index is up to date\n", uikit.SymOK)
		}
		fmt.Fprintf(&b, "  %s %d item(s) would be indexed: +%d new, ~%d changed, -%d dropped\n",
			uikit.SymArrow, r.Planned, r.Added, r.Updated, r.Deleted)
		return b.String()
	}
	if r.Planned == 0 {
		return fmt.Sprintf("  %s content index is up to date\n", uikit.SymOK)
	}
	fmt.Fprintf(&b, "  %s indexed %d item(s) — +%d new, ~%d changed, -%d dropped",
		uikit.SymOK, r.Added+r.Updated, r.Added, r.Updated, r.Deleted)
	if r.Duration > 0 {
		fmt.Fprintf(&b, " in %s", r.Duration.Truncate(time.Second))
	}
	b.WriteString("\n")
	if len(r.BySource) > 0 {
		fmt.Fprintf(&b, "      %s\n", uikit.TUI.Dim().Render(sourceBreakdown(r.BySource)))
	}
	if r.Skipped > 0 {
		fmt.Fprintf(&b, "      %s\n", uikit.TUI.Dim().Render(
			fmt.Sprintf("%d skipped (no extractable text)", r.Skipped)))
	}
	if len(r.Failed) > 0 {
		fmt.Fprintf(&b, "  %s %d item(s) failed to load:\n", uikit.SymWarn, len(r.Failed))
		for _, key := range slices.Sorted(maps.Keys(r.Failed)) {
			fmt.Fprintf(&b, "      %s  %s\n", key, uikit.TUI.Dim().Render(r.Failed[key]))
		}
	}
	return b.String()
}

// ContentStatsResult is returned by `zot content stats`.
type ContentStatsResult struct {
	Path     string         `json:"path"`
	Indexed  int            `json:"indexed"`
	BySource map[string]int `json:"by_source,omitempty"`
	Bytes    int64          `json:"bytes"`
	// Pending is how many items a build would touch — 0 means the index
	// matches the library.
	Pending int `json:"pending"`
	// Candidates is how many items in the library have any text at all.
	Candidates int `json:"candidates"`
}

// JSON implements cmdutil.Result.
func (r ContentStatsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ContentStatsResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.Dim().Render("content index"))
	fmt.Fprintf(&b, "  %-14s %d of %d item(s) with text\n", "indexed", r.Indexed, r.Candidates)
	if len(r.BySource) > 0 {
		fmt.Fprintf(&b, "  %-14s %s\n", "sources", sourceBreakdown(r.BySource))
	}
	fmt.Fprintf(&b, "  %-14s %.1f MB\n", "size", float64(r.Bytes)/(1<<20))
	fmt.Fprintf(&b, "  %-14s %s\n", "path", uikit.TUI.Dim().Render(r.Path))
	if r.Pending > 0 {
		fmt.Fprintf(&b, "\n  %s %d item(s) out of date — run `sci zot content build`\n",
			uikit.SymWarn, r.Pending)
	} else if r.Indexed > 0 {
		fmt.Fprintf(&b, "\n  %s up to date\n", uikit.SymOK)
	}
	return b.String()
}

// ContentReadResult is returned by `zot content read <item-key>` — the
// indexed text of a paper, whatever source it came from.
type ContentReadResult struct {
	ItemKey string `json:"item_key"`
	Source  string `json:"source"`
	Chars   int    `json:"chars"`
	Body    string `json:"body"`
}

// JSON implements cmdutil.Result.
func (r ContentReadResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ContentReadResult) Human() string {
	return fmt.Sprintf("\n  %s %s  %s\n\n%s\n",
		uikit.TUI.TextBlue().Render(r.ItemKey),
		uikit.TUI.Dim().Render("("+r.Source+")"),
		uikit.TUI.Dim().Render(fmt.Sprintf("%d chars", r.Chars)),
		r.Body)
}

// sourceBreakdown renders a by-source count map in a stable order, with
// the higher-fidelity source first so the reader sees coverage quality
// at a glance.
func sourceBreakdown(bySource map[string]int) string {
	order := []string{string(content.SourceDocling), string(content.SourceZotero)}
	parts := lo.FilterMap(order, func(s string, _ int) (string, bool) {
		n, ok := bySource[s]
		if !ok {
			return "", false
		}
		return fmt.Sprintf("%d %s", n, s), true
	})
	return strings.Join(parts, " · ")
}
