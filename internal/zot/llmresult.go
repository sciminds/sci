package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// ---------------------------------------------------------------------------
// catalog
// ---------------------------------------------------------------------------

// LLMCatalogEntry is one paper in the catalog index. Citekey/Year/Authors/
// Abstract are populated only when `--full` is set — the default shape
// stays compact (title + DOI + date + tags) so the catalog command can
// still cheaply surface 100s of papers without blowing the LLM context.
type LLMCatalogEntry struct {
	Key          string   `json:"key"`
	Citekey      string   `json:"citekey,omitempty"`
	Title        string   `json:"title"`
	Year         int      `json:"year,omitempty"`
	DOI          string   `json:"doi,omitempty"`
	Date         string   `json:"date,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	AuthorsTotal int      `json:"authors_total,omitempty"`
	Abstract     string   `json:"abstract,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	NoteKey      string   `json:"note_key"`
	IsHTML       bool     `json:"is_html"`
}

// LLMCatalogResult is returned by `zot llm catalog`.
type LLMCatalogResult struct {
	Count   int               `json:"count"`
	Entries []LLMCatalogEntry `json:"entries"`
}

// JSON implements cmdutil.Result.
func (r LLMCatalogResult) JSON() any { return r }

// Human implements cmdutil.Result. Per-entry the renderer branches on
// whether `--full` enrichment ran: rich entries (any of Citekey / Year /
// Authors / Abstract populated) get the ItemBrief-style block so TTY users
// see what the flag bought them; plain entries stay one-line compact.
func (r LLMCatalogResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no docling notes in library\n", uikit.SymArrow)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.Dim().Render("docling note catalog"))
	for _, e := range r.Entries {
		if e.isRich() {
			writeBriefLine(&b, e.toBrief())
			if e.IsHTML {
				fmt.Fprintf(&b, "    %s\n", uikit.TUI.Dim().Render("[html]"))
			}
			continue
		}
		fmt.Fprintf(&b, "  %s  %s",
			uikit.TUI.TextBlue().Render(e.Key),
			e.Title,
		)
		if e.DOI != "" {
			fmt.Fprintf(&b, "  %s", uikit.TUI.Dim().Render(e.DOI))
		}
		if e.IsHTML {
			fmt.Fprintf(&b, "  %s", uikit.TUI.Dim().Render("[html]"))
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "\n  %s %d paper(s)\n", uikit.SymArrow, r.Count)
	return b.String()
}

// isRich reports whether this entry carries `--full` enrichment. Populated
// by llm_catalog.go only when the parent Item read succeeded; entries from
// pre-`--full` runs or lookup-miss fallbacks leave all four fields zero.
func (e LLMCatalogEntry) isRich() bool {
	return e.Citekey != "" || e.Year != 0 || len(e.Authors) > 0 || e.Abstract != ""
}

// toBrief projects a catalog entry into the ItemBrief shape that
// writeBriefLine knows how to render. Publication is not carried in the
// catalog entry (it's not part of the compact shape by design), so it
// stays empty — callers who want it run `item read`.
func (e LLMCatalogEntry) toBrief() ItemBrief {
	return ItemBrief{
		Key:          e.Key,
		Citekey:      e.Citekey,
		Title:        e.Title,
		Year:         e.Year,
		DOI:          e.DOI,
		Authors:      e.Authors,
		AuthorsTotal: e.AuthorsTotal,
		Abstract:     e.Abstract,
		Tags:         e.Tags,
	}
}

// ---------------------------------------------------------------------------
// read
// ---------------------------------------------------------------------------

// LLMReadEntry is one note in the read result.
type LLMReadEntry struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	DOI     string `json:"doi,omitempty"`
	NoteKey string `json:"note_key"`
	Body    string `json:"body"`
}

// LLMReadResult is returned by `zot llm read`.
type LLMReadResult struct {
	Count   int            `json:"count"`
	Entries []LLMReadEntry `json:"entries"`
}

// JSON implements cmdutil.Result.
func (r LLMReadResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LLMReadResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no notes found\n", uikit.SymArrow)
	}
	var b strings.Builder
	for i, e := range r.Entries {
		if i > 0 {
			fmt.Fprint(&b, "\n---\n\n")
		}
		fmt.Fprintf(&b, "=== %s | %s", e.Key, e.Title)
		if e.DOI != "" {
			fmt.Fprintf(&b, " | %s", e.DOI)
		}
		fmt.Fprintf(&b, " ===\n\n")
		fmt.Fprint(&b, e.Body)
		fmt.Fprintln(&b)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// query
// ---------------------------------------------------------------------------

// LLMQueryMatch is one paper's mq output.
type LLMQueryMatch struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Output string `json:"output"`
}

// LLMQueryResult is returned by `zot llm query`.
type LLMQueryResult struct {
	MqQuery string          `json:"mq_query"`
	Matched int             `json:"matched"`
	Skipped int             `json:"skipped_html,omitempty"`
	Results []LLMQueryMatch `json:"results"`
}

// JSON implements cmdutil.Result.
func (r LLMQueryResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LLMQueryResult) Human() string {
	if r.Matched == 0 {
		return fmt.Sprintf("  %s no matches\n", uikit.SymArrow)
	}
	var b strings.Builder
	for i, m := range r.Results {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		fmt.Fprintf(&b, "=== %s | %s ===\n", m.Key, m.Title)
		fmt.Fprintln(&b, m.Output)
	}
	if r.Skipped > 0 {
		fmt.Fprintf(&b, "\n  %s skipped %d HTML-mode note(s)\n", uikit.SymArrow, r.Skipped)
	}
	return b.String()
}
