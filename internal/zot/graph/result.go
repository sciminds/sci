package graph

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
)

// compactAuthorLimit caps the number of authors emitted per neighbor in the
// default (non-verbose) JSON shape. A handful of cited papers in the wild
// carry 100+ author lists (large collaborations, consortium papers); the
// full list is useless for lit-review skimming and expensive for LLM
// agents processing the output. 3 is "enough to disambiguate", callers
// who need the full list pass --verbose.
const compactAuthorLimit = 3

// CmdResult wraps a *Result so it can satisfy cmdutil.Result. Lives in the
// graph package (not internal/zot) to avoid an import cycle: internal/zot
// is imported transitively by internal/zot/api which graph depends on.
//
// Verbose controls JSON author-list truncation (see compactAuthorLimit).
// The CLI plumbs --verbose through this field; the Result itself always
// holds the full author lists as parsed from OpenAlex.
type CmdResult struct {
	*Result
	Verbose bool `json:"-"`
}

// NeighborCompact is the trimmed per-neighbor shape emitted when Verbose is
// false. Identical to Neighbor except Authors is capped and the original
// length is preserved in AuthorsTotal so agents can tell trimming happened.
type NeighborCompact struct {
	Key          string   `json:"key,omitempty"`
	OpenAlex     string   `json:"openalex,omitempty"`
	DOI          string   `json:"doi,omitempty"`
	Title        string   `json:"title"`
	Year         int      `json:"year,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	AuthorsTotal int      `json:"authors_total,omitempty"`
	CitedByCount int      `json:"cited_by_count,omitempty"`
	OAStatus     string   `json:"oa_status,omitempty"`
}

// compactResult mirrors Result with NeighborCompact in place of Neighbor.
type compactResult struct {
	Item           Source            `json:"item"`
	Direction      string            `json:"direction"`
	InLibrary      []NeighborCompact `json:"in_library"`
	OutsideLibrary []NeighborCompact `json:"outside_library"`
	Stats          Stats             `json:"stats"`
}

// JSON implements cmdutil.Result. Emits a compact shape (authors trimmed to
// compactAuthorLimit) unless Verbose is set.
func (r CmdResult) JSON() any {
	if r.Result == nil {
		return nil
	}
	if r.Verbose {
		return r.Result
	}
	return compactResult{
		Item:           r.Item,
		Direction:      r.Direction,
		InLibrary:      lo.Map(r.InLibrary, func(n Neighbor, _ int) NeighborCompact { return compactNeighbor(n) }),
		OutsideLibrary: lo.Map(r.OutsideLibrary, func(n Neighbor, _ int) NeighborCompact { return compactNeighbor(n) }),
		Stats:          r.Stats,
	}
}

func compactNeighbor(n Neighbor) NeighborCompact {
	trimmed := n.Authors
	total := 0
	if len(n.Authors) > compactAuthorLimit {
		trimmed = n.Authors[:compactAuthorLimit]
		total = len(n.Authors)
	}
	return NeighborCompact{
		Key:          n.Key,
		OpenAlex:     n.OpenAlex,
		DOI:          n.DOI,
		Title:        n.Title,
		Year:         n.Year,
		Authors:      trimmed,
		AuthorsTotal: total,
		CitedByCount: n.CitedByCount,
		OAStatus:     n.OAStatus,
	}
}

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
