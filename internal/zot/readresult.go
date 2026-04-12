package zot

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
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

// JSON implements cmdutil.Result.
func (r ListResult) JSON() any { return r }

// Human implements cmdutil.Result.
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

// JSON implements cmdutil.Result.
func (r ItemResult) JSON() any { return r.Item }

// Human implements cmdutil.Result.
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
		parts := lo.Map(it.Creators, func(c local.Creator, _ int) string {
			if c.Name != "" {
				return c.Name
			}
			return strings.TrimSpace(c.First + " " + c.Last)
		})
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

// ChildItemView is the zot-package-facing view of a child item as
// returned by `zot item children`. Mirrors local.ChildItem verbatim —
// duplicated instead of aliased because local → zot would cycle.
// The CLI layer converts from local.ChildItem at the call site.
type ChildItemView struct {
	Key         string   `json:"key"`
	ItemType    string   `json:"item_type"`
	Title       string   `json:"title,omitempty"`
	Note        string   `json:"note,omitempty"`
	ContentType string   `json:"content_type,omitempty"`
	Filename    string   `json:"filename,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ChildrenListResult is returned for `zot item children <KEY>`:
// a flat listing of a parent item's child items (attachments + notes),
// as reported by the Zotero Web API. Used both by humans and by
// scripts that want to feed note or attachment keys into other zot
// commands (e.g. `zot item delete`).
type ChildrenListResult struct {
	ParentKey string          `json:"parent_key"`
	Count     int             `json:"count"`
	Children  []ChildItemView `json:"children"`
}

// JSON implements cmdutil.Result.
func (r ChildrenListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ChildrenListResult) Human() string {
	var b strings.Builder
	if r.Count == 0 {
		fmt.Fprintf(&b, "  %s %s has no children\n", ui.SymArrow, r.ParentKey)
		return b.String()
	}
	fmt.Fprintf(&b, "\n  %s %s\n\n",
		ui.TUI.Dim().Render("children of"),
		ui.TUI.Accent().Render(r.ParentKey),
	)
	for _, ch := range r.Children {
		fmt.Fprintf(&b, "    %s  %s",
			ui.TUI.Accent().Render(ch.Key),
			ui.TUI.Dim().Render(childTypeLabel(ch.ItemType)),
		)
		// One-line descriptor varies by type:
		// attachment → contentType + filename
		// note       → first-line snippet of the HTML body, or tags
		switch ch.ItemType {
		case "attachment":
			meta := ch.ContentType
			if meta != "" && ch.Filename != "" {
				meta += "  "
			}
			meta += ch.Filename
			fmt.Fprintf(&b, "  %s", meta)
		case "note":
			snippet := noteSnippet(ch.Note)
			if snippet == "" && len(ch.Tags) > 0 {
				snippet = "[" + strings.Join(ch.Tags, ", ") + "]"
			}
			if snippet != "" {
				fmt.Fprintf(&b, "  %s", ui.TUI.Dim().Render(snippet))
			}
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "\n  %s %d child item(s)\n", ui.SymArrow, r.Count)
	return b.String()
}

func childTypeLabel(t string) string {
	if t == "" {
		return "item"
	}
	return t
}

// noteSnippet returns a ~60-char preview of a note body with HTML
// tags stripped. Good enough for CLI display — full parsing lives
// in MarkdownToNoteHTML's inverse, which we don't need here.
func noteSnippet(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case inTag:
			// skip
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
		if b.Len() >= 80 {
			break
		}
	}
	s := strings.TrimSpace(b.String())
	// Collapse runs of whitespace
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

// CollectionListResult is returned for `zot collection list`.
type CollectionListResult struct {
	Count       int                `json:"count"`
	Collections []local.Collection `json:"collections"`
}

// JSON implements cmdutil.Result.
func (r CollectionListResult) JSON() any { return r }

// Human implements cmdutil.Result.
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

// JSON implements cmdutil.Result.
func (r TagListResult) JSON() any { return r }

// Human implements cmdutil.Result.
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

// StatsResult is returned for `zot info`.
type StatsResult struct {
	Stats   local.Stats `json:"stats"`
	DataDir string      `json:"data_dir"`
	Schema  int         `json:"schema_version"`
}

// JSON implements cmdutil.Result.
func (r StatsResult) JSON() any { return r }

// Human implements cmdutil.Result.
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

// ExportResult is returned for `zot item export` (single-item).
type ExportResult struct {
	Key    string `json:"key"`
	Format string `json:"format"`
	Body   string `json:"body"`
}

// JSON implements cmdutil.Result.
func (r ExportResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ExportResult) Human() string { return r.Body + "\n" }

// LibraryExportResult is returned for `zot export` (full library) and for
// `zot search --export` (filtered subset). Body holds the emitted document
// when streaming to stdout; when the user passed -o, Body is empty and
// OutPath/KeymapPath point at the on-disk artifacts.
type LibraryExportResult struct {
	Format     string      `json:"format"`
	OutPath    string      `json:"out_path,omitempty"`
	KeymapPath string      `json:"keymap_path,omitempty"`
	Body       string      `json:"body,omitempty"`
	Stats      ExportStats `json:"stats"`
}

// JSON implements cmdutil.Result.
func (r LibraryExportResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LibraryExportResult) Human() string {
	var b strings.Builder
	if r.OutPath == "" {
		// Streaming to stdout — body IS the output. Footer goes through
		// the human renderer as a separate block so it lands on stderr
		// via the caller's renderer pipeline. We emit it as a trailing
		// comment-block here; callers that want clean stdout should
		// always pass -o.
		b.WriteString(r.Body)
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "  %s wrote %s to %s\n", ui.SymOK, r.Format, r.OutPath)
		if r.KeymapPath != "" {
			fmt.Fprintf(&b, "    %s %s\n", ui.TUI.Dim().Render("keymap:"), r.KeymapPath)
		}
	}
	fmt.Fprintf(&b, "    %s %d item(s): %d pinned, %d synthesized",
		ui.TUI.Dim().Render("·"),
		r.Stats.Total, r.Stats.Pinned, r.Stats.Synthesized)
	if r.Stats.Drifted > 0 {
		fmt.Fprintf(&b, ", %d drifted", r.Stats.Drifted)
	}
	b.WriteString("\n")
	return b.String()
}

// OpenResult is returned for `zot open`.
type OpenResult struct {
	Key      string `json:"key"`
	Path     string `json:"path"`
	Launched bool   `json:"launched"`
	Message  string `json:"message"`
}

// JSON implements cmdutil.Result.
func (r OpenResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r OpenResult) Human() string {
	sym := ui.SymOK
	if !r.Launched {
		sym = ui.SymFail
	}
	return fmt.Sprintf("  %s %s\n    %s\n", sym, r.Message, ui.TUI.Dim().Render(r.Path))
}
