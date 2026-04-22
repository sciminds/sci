package graph

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// CmdResult wraps a *Result so it can satisfy cmdutil.Result. Lives in the
// graph package (not internal/zot) to avoid an import cycle: internal/zot
// is imported transitively by internal/zot/api which graph depends on.
type CmdResult struct {
	*Result
}

// JSON implements cmdutil.Result.
func (r CmdResult) JSON() any { return r.Result }

// Human implements cmdutil.Result. Renders an agent + human-readable
// digest: source paper header, then in-library hits keyed by Zotero key,
// then outside-library suggestions ranked by citations with a copyable
// `item add --openalex` template at the end of each row.
func (r CmdResult) Human() string {
	if r.Result == nil {
		return ""
	}
	var b strings.Builder
	tui := uikit.TUI

	verb := map[string]string{
		"refs":  "cited references",
		"cites": "citing works",
	}[r.Direction]
	if verb == "" {
		verb = "neighbors"
	}

	if r.Item.Title != "" {
		fmt.Fprintf(&b, "\n  %s\n", tui.TextBlueBold().Render(r.Item.Title))
	}
	meta := []string{}
	if r.Item.Key != "" {
		meta = append(meta, r.Item.Key)
	}
	if r.Item.OpenAlex != "" {
		meta = append(meta, r.Item.OpenAlex)
	}
	if r.Item.DOI != "" {
		meta = append(meta, r.Item.DOI)
	}
	if len(meta) > 0 {
		fmt.Fprintf(&b, "  %s\n", tui.Dim().Render(strings.Join(meta, "  ·  ")))
	}

	fmt.Fprintf(&b, "\n  %s %d %s — %d in library, %d to discover\n",
		uikit.SymArrow, r.Stats.Total, verb, r.Stats.InLibrary, r.Stats.OutsideLibrary)
	if r.Stats.MissingMetadata > 0 {
		fmt.Fprintf(&b, "  %s %d neighbor(s) had no DOI — surfaced under outside_library\n",
			tui.Dim().Render("·"), r.Stats.MissingMetadata)
	}

	if len(r.InLibrary) > 0 {
		fmt.Fprintf(&b, "\n  %s\n", tui.TextBlueBold().Render("In library"))
		for _, n := range r.InLibrary {
			writeNeighborLine(&b, n, true)
		}
	}
	if len(r.OutsideLibrary) > 0 {
		fmt.Fprintf(&b, "\n  %s\n", tui.TextBlueBold().Render("Outside library"))
		for _, n := range r.OutsideLibrary {
			writeNeighborLine(&b, n, false)
		}
	}
	return b.String()
}

func writeNeighborLine(b *strings.Builder, n Neighbor, inLib bool) {
	tui := uikit.TUI
	id := n.OpenAlex
	if inLib && n.Key != "" {
		id = n.Key
	}
	year := ""
	if n.Year > 0 {
		year = " " + tui.Dim().Render(fmt.Sprintf("(%d)", n.Year))
	}
	fmt.Fprintf(b, "    %s  %s%s\n", tui.TextBlue().Render(id), n.Title, year)
	subline := []string{}
	if len(n.Authors) > 0 {
		first := n.Authors[0]
		if len(n.Authors) > 1 {
			first += " et al."
		}
		subline = append(subline, first)
	}
	if n.CitedByCount > 0 {
		subline = append(subline, fmt.Sprintf("cited %d×", n.CitedByCount))
	}
	if !inLib && n.OpenAlex != "" {
		subline = append(subline, fmt.Sprintf("add: zot item add --openalex %s", n.OpenAlex))
	}
	if len(subline) > 0 {
		fmt.Fprintf(b, "      %s\n", tui.Dim().Render(strings.Join(subline, " · ")))
	}
}
