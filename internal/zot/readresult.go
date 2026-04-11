package zot

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/zot/local"
)

// ListResult wraps a slice of items for search/list/recent outputs.
type ListResult struct {
	Query   string       `json:"query,omitempty"`
	Count   int          `json:"count"`
	Items   []local.Item `json:"items"`
	Library int64        `json:"library_id"`
}

func (r ListResult) JSON() any { return r }
func (r ListResult) Human() string {
	if r.Count == 0 {
		if r.Query != "" {
			return fmt.Sprintf("  %s no results for %q\n", ui.TUI.Dim().Render("·"), r.Query)
		}
		return fmt.Sprintf("  %s no items\n", ui.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, it := range r.Items {
		writeItemLine(&b, it)
	}
	fmt.Fprintf(&b, "\n  %s %d item(s)\n", ui.SymArrow, r.Count)
	return b.String()
}

func writeItemLine(b *strings.Builder, it local.Item) {
	title := it.Title
	if title == "" {
		title = ui.TUI.Dim().Render("(untitled)")
	}
	year := ""
	if d := cleanDate(it.Date); len(d) >= 4 {
		year = " " + ui.TUI.Dim().Render("("+d[:4]+")")
	}
	fmt.Fprintf(b, "  %s  %s%s\n",
		ui.TUI.Accent().Render(it.Key),
		title,
		year,
	)
	meta := it.Type
	if it.Publication != "" {
		meta += " · " + it.Publication
	}
	if it.DOI != "" {
		meta += " · " + it.DOI
	}
	fmt.Fprintf(b, "    %s\n", ui.TUI.Dim().Render(meta))
}

// ItemResult is returned for `zot read <key>`.
type ItemResult struct {
	Item local.Item `json:"item"`
}

func (r ItemResult) JSON() any { return r.Item }
func (r ItemResult) Human() string {
	var b strings.Builder
	it := r.Item
	title := it.Title
	if title == "" {
		title = "(untitled)"
	}
	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render(title))
	fmt.Fprintf(&b, "  %s  %s\n\n",
		ui.TUI.Dim().Render(it.Key),
		ui.TUI.Dim().Render(it.Type),
	)
	if len(it.Creators) > 0 {
		parts := make([]string, 0, len(it.Creators))
		for _, c := range it.Creators {
			if c.Name != "" {
				parts = append(parts, c.Name)
			} else {
				parts = append(parts, strings.TrimSpace(c.First+" "+c.Last))
			}
		}
		fmt.Fprintf(&b, "  %s %s\n", ui.TUI.Dim().Render("authors:"), strings.Join(parts, ", "))
	}
	writeField(&b, "date", cleanDate(it.Date))
	writeField(&b, "publication", it.Publication)
	writeField(&b, "doi", it.DOI)
	writeField(&b, "url", it.URL)
	if it.Abstract != "" {
		fmt.Fprintf(&b, "\n  %s\n  %s\n", ui.TUI.Dim().Render("abstract:"), it.Abstract)
	}
	if len(it.Tags) > 0 {
		fmt.Fprintf(&b, "\n  %s %s\n", ui.TUI.Dim().Render("tags:"), strings.Join(it.Tags, ", "))
	}
	if len(it.Collections) > 0 {
		fmt.Fprintf(&b, "  %s %s\n", ui.TUI.Dim().Render("collections:"), strings.Join(it.Collections, ", "))
	}
	if len(it.Attachments) > 0 {
		fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Dim().Render("attachments:"))
		for _, a := range it.Attachments {
			fmt.Fprintf(&b, "    %s  %s\n", ui.TUI.Accent().Render(a.Key), a.Filename)
		}
	}
	return b.String() + "\n"
}

// cleanDate returns just the sortable portion of a Zotero date string.
// Zotero stores dates as "YYYY-MM-DD originalText"; we drop everything
// after the first whitespace. Empty and pre-normalized strings pass through.
func cleanDate(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func writeField(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "  %s %s\n", ui.TUI.Dim().Render(label+":"), value)
}

// CollectionListResult is returned for `zot collection list`.
type CollectionListResult struct {
	Count       int                `json:"count"`
	Collections []local.Collection `json:"collections"`
}

func (r CollectionListResult) JSON() any { return r }
func (r CollectionListResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no collections\n", ui.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, c := range r.Collections {
		fmt.Fprintf(&b, "  %s  %s %s\n",
			ui.TUI.Accent().Render(c.Key),
			c.Name,
			ui.TUI.Dim().Render(fmt.Sprintf("(%d)", c.ItemCount)),
		)
	}
	fmt.Fprintf(&b, "\n  %s %d collection(s)\n", ui.SymArrow, r.Count)
	return b.String()
}

// TagListResult is returned for `zot tags list`.
type TagListResult struct {
	Count int         `json:"count"`
	Tags  []local.Tag `json:"tags"`
}

func (r TagListResult) JSON() any { return r }
func (r TagListResult) Human() string {
	if r.Count == 0 {
		return fmt.Sprintf("  %s no tags\n", ui.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, t := range r.Tags {
		fmt.Fprintf(&b, "  %s  %s\n",
			ui.TUI.Dim().Render(fmt.Sprintf("%5d", t.Count)),
			t.Name,
		)
	}
	fmt.Fprintf(&b, "\n  %s %d tag(s)\n", ui.SymArrow, r.Count)
	return b.String()
}

// StatsResult is returned for `zot stats`.
type StatsResult struct {
	Stats   local.Stats `json:"stats"`
	DataDir string      `json:"data_dir"`
	Schema  int         `json:"schema_version"`
}

func (r StatsResult) JSON() any { return r }
func (r StatsResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n", ui.TUI.AccentBold().Render("Library stats"))
	fmt.Fprintf(&b, "  %s %s\n", ui.TUI.Dim().Render("data:"), r.DataDir)
	fmt.Fprintf(&b, "  %s schema v%d\n\n", ui.TUI.Dim().Render("  ·  "), r.Schema)
	fmt.Fprintf(&b, "  %s %d\n", ui.TUI.Dim().Render("items:         "), r.Stats.TotalItems)
	fmt.Fprintf(&b, "  %s %d\n", ui.TUI.Dim().Render("with DOI:      "), r.Stats.WithDOI)
	fmt.Fprintf(&b, "  %s %d\n", ui.TUI.Dim().Render("with abstract: "), r.Stats.WithAbstract)
	fmt.Fprintf(&b, "  %s %d\n", ui.TUI.Dim().Render("with PDF:      "), r.Stats.WithAttachment)
	fmt.Fprintf(&b, "  %s %d\n", ui.TUI.Dim().Render("collections:   "), r.Stats.Collections)
	fmt.Fprintf(&b, "  %s %d\n\n", ui.TUI.Dim().Render("tags:          "), r.Stats.Tags)
	if len(r.Stats.ByType) > 0 {
		fmt.Fprintf(&b, "  %s\n", ui.TUI.Dim().Render("by type:"))
		// Sort by count desc for readability.
		type kv struct {
			k string
			v int
		}
		list := make([]kv, 0, len(r.Stats.ByType))
		for k, v := range r.Stats.ByType {
			list = append(list, kv{k, v})
		}
		// Inline insertion sort — len is small.
		for i := 1; i < len(list); i++ {
			for j := i; j > 0 && list[j-1].v < list[j].v; j-- {
				list[j-1], list[j] = list[j], list[j-1]
			}
		}
		for _, kv := range list {
			fmt.Fprintf(&b, "    %-20s %d\n", kv.k, kv.v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ExportResult is returned for `zot export`.
type ExportResult struct {
	Key    string `json:"key"`
	Format string `json:"format"`
	Body   string `json:"body"`
}

func (r ExportResult) JSON() any     { return r }
func (r ExportResult) Human() string { return r.Body + "\n" }

// OpenResult is returned for `zot open`.
type OpenResult struct {
	Key      string `json:"key"`
	Path     string `json:"path"`
	Launched bool   `json:"launched"`
	Message  string `json:"message"`
}

func (r OpenResult) JSON() any { return r }
func (r OpenResult) Human() string {
	sym := ui.SymOK
	if !r.Launched {
		sym = ui.SymFail
	}
	return fmt.Sprintf("  %s %s\n    %s\n", sym, r.Message, ui.TUI.Dim().Render(r.Path))
}
