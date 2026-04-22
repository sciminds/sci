package zot

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/citekey"
	"github.com/sciminds/cli/internal/zot/local"
)

// briefAuthorLimit caps authors per ItemBrief. Multi-author consortium
// papers (fMRI collaborations, survey papers) routinely exceed 50 authors;
// the full list is noise for lit-review triage. Callers who need the raw
// creator records use `item read`.
const briefAuthorLimit = 3

// ItemBrief is the skim-friendly per-item shape emitted by `search --full`
// and `llm catalog --full`. Everything needed to decide "do I want this
// paper?" (title, year, citekey, abstract, first-few authors) without the
// full Fields map / Creator list that `item read` returns. Compare with
// FindWorkCompact (OpenAlex-side equivalent) and graph.NeighborCompact.
type ItemBrief struct {
	Key          string   `json:"key"`
	Citekey      string   `json:"citekey,omitempty"`
	Title        string   `json:"title,omitempty"`
	Year         int      `json:"year,omitempty"`
	DOI          string   `json:"doi,omitempty"`
	Publication  string   `json:"publication,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	AuthorsTotal int      `json:"authors_total,omitempty"`
	Abstract     string   `json:"abstract,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// ToBrief projects a fully-hydrated local.Item into the brief shape.
// Runs citekey.Enrich as a side-effect on the item so the resolved key
// flows through without callers needing to know the two-step dance.
func ToBrief(it *local.Item) ItemBrief {
	if it == nil {
		return ItemBrief{}
	}
	citekey.Enrich(it)
	authors := lo.FilterMap(it.Creators, func(c local.Creator, _ int) (string, bool) {
		name := c.Name
		if name == "" {
			name = strings.TrimSpace(c.First + " " + c.Last)
		}
		return name, name != ""
	})
	trimmed := authors
	total := 0
	if len(authors) > briefAuthorLimit {
		trimmed = authors[:briefAuthorLimit]
		total = len(authors)
	}
	return ItemBrief{
		Key:          it.Key,
		Citekey:      it.Citekey,
		Title:        it.Title,
		Year:         it.Year,
		DOI:          it.DOI,
		Publication:  it.Publication,
		Authors:      trimmed,
		AuthorsTotal: total,
		Abstract:     it.Abstract,
		Tags:         it.Tags,
	}
}

// ListBriefResult is the `search --full` output shape. Mirrors ListResult
// but emits ItemBrief rows so LLM agents can triage candidates in a
// single call (abstract + citekey inlined) instead of N round-trips
// through `item read`.
type ListBriefResult struct {
	Query   string      `json:"query,omitempty"`
	Count   int         `json:"count"`
	Items   []ItemBrief `json:"items"`
	Library int64       `json:"library_id"`
	Scope   string      `json:"searched,omitempty"`
	Hint    string      `json:"hint,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ListBriefResult) JSON() any { return r }

// Human implements cmdutil.Result. Renders the same shape as ListResult
// with an extra abstract preview line — useful when a human eyeballs
// `search --full` output.
func (r ListBriefResult) Human() string {
	if r.Count == 0 {
		return renderEmptyListHuman(r.Query, r.Scope, r.Hint)
	}
	var b strings.Builder
	for _, it := range r.Items {
		writeBriefLine(&b, it)
	}
	fmt.Fprintf(&b, "\n  %s %d item(s)\n", uikit.SymArrow, r.Count)
	return b.String()
}

func writeBriefLine(b *strings.Builder, it ItemBrief) {
	title := it.Title
	if title == "" {
		title = uikit.TUI.Dim().Render("(untitled)")
	}
	year := ""
	if it.Year > 0 {
		year = " " + uikit.TUI.Dim().Render(fmt.Sprintf("(%d)", it.Year))
	}
	fmt.Fprintf(b, "  %s  %s%s\n", uikit.TUI.TextBlue().Render(it.Key), title, year)
	meta := []string{}
	if len(it.Authors) > 0 {
		first := it.Authors[0]
		if it.AuthorsTotal > 0 || len(it.Authors) > 1 {
			first += " et al."
		}
		meta = append(meta, first)
	}
	if it.Citekey != "" {
		meta = append(meta, "@"+it.Citekey)
	}
	if it.DOI != "" {
		meta = append(meta, it.DOI)
	}
	if len(meta) > 0 {
		fmt.Fprintf(b, "    %s\n", uikit.TUI.Dim().Render(strings.Join(meta, " · ")))
	}
	if it.Abstract != "" {
		preview := it.Abstract
		if len(preview) > 240 {
			preview = preview[:240] + "…"
		}
		fmt.Fprintf(b, "    %s\n", uikit.TUI.Dim().Render(preview))
	}
}
