package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/content"
	"github.com/sciminds/cli/internal/zot/local"

	"github.com/sciminds/cli/internal/zot/oacache"
	"github.com/sciminds/cli/internal/zot/xrcache"
)

// ListResult wraps a slice of items for search/list/recent outputs.
//
// Scope is an optional descriptor shown on zero-hit results — e.g.
// "title, DOI, publication (local)" — so callers (especially LLM agents)
// can tell WHY a search missed and adjust. Hint is a free-form follow-up
// suggestion shown alongside Scope. Both are elided from JSON when empty.
type ListResult struct {
	Query     string       `json:"query,omitempty"`
	Count     int          `json:"count"`
	Total     int          `json:"total,omitempty"`     // pre-limit match count; 0 = unknown
	Truncated bool         `json:"truncated,omitempty"` // true when Count < Total
	Items     []local.Item `json:"items"`
	Library   int64        `json:"library_id"`
	Scope     string       `json:"searched,omitempty"`
	Hint      string       `json:"hint,omitempty"`
	// Snippets carries the matched excerpt from the paper's text, keyed
	// by item key, for the hits that `search --content` matched on
	// content rather than metadata. Absent for every other search — it
	// rides beside Items rather than inside local.Item so the item shape
	// stays the same one every other command emits.
	Snippets map[string]string `json:"snippets,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ListResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ListResult) Human() string {
	if r.Count == 0 {
		return renderEmptyListHuman(r.Query, r.Scope, r.Hint)
	}
	var b strings.Builder
	for _, it := range r.Items {
		writeItemLine(&b, it)
		writeSnippetLine(&b, r.Snippets[it.Key])
	}
	if r.Truncated {
		fmt.Fprintf(&b, "\n  %s showing %d of %d — raise --limit to see more\n", uikit.SymArrow, r.Count, r.Total)
	} else {
		fmt.Fprintf(&b, "\n  %s %d item(s)\n", uikit.SymArrow, r.Count)
	}
	return b.String()
}

// renderEmptyListHuman formats the "no hits" branch shared by ListResult
// and ListBriefResult — a query echo with optional `searched:` scope and
// `hint:` follow-up, both styled for the terminal.
func renderEmptyListHuman(query, scope, hint string) string {
	var b strings.Builder
	if query != "" {
		fmt.Fprintf(&b, "  %s no results for %q\n", uikit.TUI.Dim().Render("·"), query)
	} else {
		fmt.Fprintf(&b, "  %s no items\n", uikit.TUI.Dim().Render("·"))
	}
	if scope != "" {
		fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("searched:"), scope)
	}
	if hint != "" {
		fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("hint:"), hint)
	}
	return b.String()
}

// writeSnippetLine renders one hit's content excerpt under its metadata,
// or nothing when the hit matched on metadata alone. Shared by ListResult
// and ListBriefResult so `--full` doesn't lose the evidence.
func writeSnippetLine(b *strings.Builder, snippet string) {
	if snippet == "" {
		return
	}
	// FTS5 hands back the excerpt with the document's own newlines; a hit
	// list needs one line per hit.
	flat := strings.Join(strings.Fields(snippet), " ")
	fmt.Fprintf(b, "    %s\n", uikit.TUI.Dim().Render(flat))
}

func writeItemLine(b *strings.Builder, it local.Item) {
	title := it.Title
	if title == "" {
		title = uikit.TUI.Dim().Render("(untitled)")
	}
	year := ""
	if d := cleanDate(it.Date); len(d) >= 4 {
		year = " " + uikit.TUI.Dim().Render("("+d[:4]+")")
	}
	fmt.Fprintf(b, "  %s  %s%s\n",
		uikit.TUI.TextBlue().Render(it.Key),
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
	fmt.Fprintf(b, "    %s\n", uikit.TUI.Dim().Render(meta))
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
	fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.TextBlueBold().Render(title))
	fmt.Fprintf(&b, "  %s  %s\n\n",
		uikit.TUI.Dim().Render(it.Key),
		uikit.TUI.Dim().Render(it.Type),
	)
	if len(it.Creators) > 0 {
		parts := lo.Map(it.Creators, func(c local.Creator, _ int) string {
			if c.Name != "" {
				return c.Name
			}
			return strings.TrimSpace(c.First + " " + c.Last)
		})
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("authors:"), strings.Join(parts, ", "))
	}
	writeField(&b, "date", cleanDate(it.Date))
	writeField(&b, "publication", it.Publication)
	writeField(&b, "doi", it.DOI)
	writeField(&b, "url", it.URL)
	if it.Abstract != "" {
		fmt.Fprintf(&b, "\n  %s\n  %s\n", uikit.TUI.Dim().Render("abstract:"), it.Abstract)
	}
	if len(it.Tags) > 0 {
		fmt.Fprintf(&b, "\n  %s %s\n", uikit.TUI.Dim().Render("tags:"), strings.Join(it.Tags, ", "))
	}
	if len(it.Collections) > 0 {
		fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("collections:"), strings.Join(it.Collections, ", "))
	}
	if len(it.Attachments) > 0 {
		fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.Dim().Render("attachments:"))
		for _, a := range it.Attachments {
			fmt.Fprintf(&b, "    %s  %s\n", uikit.TUI.TextBlue().Render(a.Key), a.Filename)
		}
	}
	writeRelationsBlock(&b, it.Relations)
	return b.String() + "\n"
}

// ItemsResult is returned for `zot item read` with more than one key —
// every requested item, fully hydrated, in request order. The single-key
// form keeps emitting the bare item via ItemResult, so existing consumers
// see a byte-identical shape; the wrapper appears only when the request
// itself was plural.
type ItemsResult struct {
	Count int          `json:"count"`
	Items []local.Item `json:"items"`
	// Missing lists requested keys that resolved to nothing — populated
	// only under `item read --missing-ok`, where a partial result is the
	// contract and the misses are data (agents batch against this; a
	// warning alone could be dropped). Without the flag a missing key
	// fails the whole read instead.
	Missing []string `json:"missing,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ItemsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ItemsResult) Human() string {
	blocks := lo.Map(r.Items, func(it local.Item, _ int) string {
		return ItemResult{Item: it}.Human()
	})
	out := strings.Join(blocks, "")
	if len(r.Missing) > 0 {
		out += fmt.Sprintf("\n  %s not found: %s\n", uikit.SymFail, strings.Join(r.Missing, ", "))
	}
	return out
}

// writeRelationsBlock renders an item's related items under its
// attachments, so what a paper is linked to is visible where you already
// look for its tags — without having to know `zot link list` exists.
//
// Predicate handling mirrors LinkListResult.Human: dc:relation renders as
// "related" (it is the user's own link), and Zotero's own predicates render
// last, under their real names, so nothing invites `link rm` to touch them.
func writeRelationsBlock(b *strings.Builder, rels *local.ItemRelationSet) {
	if rels == nil {
		return
	}
	if len(rels.Related) > 0 {
		fmt.Fprintf(b, "\n  %s\n", uikit.TUI.Dim().Render("related:"))
		for _, k := range rels.Related {
			writeLinkRow(b, "    ", k, rels.Titles[k])
		}
	}
	for _, pred := range slices.Sorted(maps.Keys(rels.Other)) {
		fmt.Fprintf(b, "\n  %s %s\n", uikit.TUI.Dim().Render(pred+":"),
			uikit.TUI.Dim().Render("(Zotero-managed)"))
		for _, k := range rels.Other[pred] {
			writeLinkRow(b, "    ", k, rels.Titles[k])
		}
	}
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
	fmt.Fprintf(b, "  %s %s\n", uikit.TUI.Dim().Render(label+":"), value)
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
	Md5         string   `json:"md5,omitempty"`
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
		fmt.Fprintf(&b, "  %s %s has no children\n", uikit.SymArrow, r.ParentKey)
		return b.String()
	}
	fmt.Fprintf(&b, "\n  %s %s\n\n",
		uikit.TUI.Dim().Render("children of"),
		uikit.TUI.TextBlue().Render(r.ParentKey),
	)
	for _, ch := range r.Children {
		fmt.Fprintf(&b, "    %s  %s",
			uikit.TUI.TextBlue().Render(ch.Key),
			uikit.TUI.Dim().Render(childTypeLabel(ch.ItemType)),
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
				fmt.Fprintf(&b, "  %s", uikit.TUI.Dim().Render(snippet))
			}
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "\n  %s %d child item(s)\n", uikit.SymArrow, r.Count)
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
//
// sci's own provenance header comes off first, for the same reason the
// content index strips it: the block is metadata about the extraction, so
// a preview that keeps it shows `--- zotero_key: … title: "…" source…`
// instead of the paper's first sentence. Order matters — the tag-strip
// below collapses newlines to spaces, which flattens the YAML into one
// unrecognizable line. Detection is positive-evidence gated, so a note
// without a provenance block passes through untouched.
func noteSnippet(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range content.StripProvenance(local.UnwrapZoteroDiv(html)) {
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
		return fmt.Sprintf("  %s no collections\n", uikit.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, c := range r.Collections {
		fmt.Fprintf(&b, "  %s  %s %s\n",
			uikit.TUI.TextBlue().Render(c.Key),
			c.Name,
			uikit.TUI.Dim().Render(fmt.Sprintf("(%d)", c.ItemCount)),
		)
	}
	fmt.Fprintf(&b, "\n  %s %d collection(s)\n", uikit.SymArrow, r.Count)
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
		return fmt.Sprintf("  %s no tags\n", uikit.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, t := range r.Tags {
		fmt.Fprintf(&b, "  %s  %s\n",
			uikit.TUI.Dim().Render(fmt.Sprintf("%5d", t.Count)),
			t.Name,
		)
	}
	fmt.Fprintf(&b, "\n  %s %d tag(s)\n", uikit.SymArrow, r.Count)
	return b.String()
}

// StatsResult is returned for `zot info --library X` and as the building
// block of MultiStatsResult (one entry per library).
//
// Scope is the bare scope token ("personal" or "shared"); Library is the
// human label ("shared (sciminds)"). LibraryAPIID carries the numeric
// Zotero Web API id — UserID for personal, GroupID for shared — so agents
// can build `zotero://select/groups/{id}/...` deeplinks without having to
// grep the on-disk config.
type StatsResult struct {
	Library      string      `json:"library"`
	Scope        string      `json:"scope,omitempty"`
	LibraryAPIID string      `json:"library_api_id,omitempty"`
	Stats        local.Stats `json:"stats"`
	DataDir      string      `json:"data_dir"`
	Schema       int         `json:"schema_version"`

	// Orient fields — populated only when `info --orient` is set. Kept
	// optional via omitempty so the default `info` shape stays unchanged.
	// Source: internal/zot/local/orient.go.
	ExtractionCoverage *local.ExtractionCoverage `json:"extraction_coverage,omitempty"`
	TopTags            []local.TagCount          `json:"top_tags,omitempty"`
	TopCollections     []local.CollectionRef     `json:"top_collections,omitempty"`
	RecentAdded        []local.RecentItem        `json:"recent_added,omitempty"`
}

// JSON implements cmdutil.Result.
func (r StatsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r StatsResult) Human() string {
	var b strings.Builder
	header := "Library stats"
	if r.Library != "" {
		header = "Library stats — " + r.Library
	}
	fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.TextBlueBold().Render(header))
	fmt.Fprintf(&b, "  %s %s\n", uikit.TUI.Dim().Render("data:"), r.DataDir)
	fmt.Fprintf(&b, "  %s schema v%d\n\n", uikit.TUI.Dim().Render("  ·  "), r.Schema)
	fmt.Fprintf(&b, "  %s %d\n", uikit.TUI.Dim().Render("items:         "), r.Stats.TotalItems)
	fmt.Fprintf(&b, "  %s %d\n", uikit.TUI.Dim().Render("with DOI:      "), r.Stats.WithDOI)
	fmt.Fprintf(&b, "  %s %d\n", uikit.TUI.Dim().Render("with abstract: "), r.Stats.WithAbstract)
	fmt.Fprintf(&b, "  %s %d\n", uikit.TUI.Dim().Render("with PDF:      "), r.Stats.WithAttachment)
	fmt.Fprintf(&b, "  %s %d\n", uikit.TUI.Dim().Render("collections:   "), r.Stats.Collections)
	fmt.Fprintf(&b, "  %s %d\n\n", uikit.TUI.Dim().Render("tags:          "), r.Stats.Tags)
	if len(r.Stats.ByType) > 0 {
		fmt.Fprintf(&b, "  %s\n", uikit.TUI.Dim().Render("by type:"))
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
	r.writeOrientBlock(&b)
	return b.String()
}

// writeOrientBlock renders the optional orient fields when present. Each
// section is independent — partial population (e.g. only TopTags set)
// still renders cleanly. Skipped entirely when no orient field is set.
func (r StatsResult) writeOrientBlock(b *strings.Builder) {
	if r.ExtractionCoverage == nil && len(r.TopTags) == 0 && len(r.TopCollections) == 0 && len(r.RecentAdded) == 0 {
		return
	}

	if r.ExtractionCoverage != nil {
		cov := r.ExtractionCoverage
		fmt.Fprintf(b, "  %s\n", uikit.TUI.Dim().Render("full-text extractions (has-markdown):"))
		fmt.Fprintf(b, "    %d / %d items (%.1f%%) — query via `sci zot llm read|query`\n\n",
			cov.WithExtraction, cov.TotalItems, cov.Percent)
	}

	if len(r.TopTags) > 0 {
		fmt.Fprintf(b, "  %s\n", uikit.TUI.Dim().Render("top tags:"))
		for _, t := range r.TopTags {
			fmt.Fprintf(b, "    %-24s %d\n", t.Name, t.Count)
		}
		b.WriteString("\n")
	}

	if len(r.TopCollections) > 0 {
		fmt.Fprintf(b, "  %s\n", uikit.TUI.Dim().Render("top collections:"))
		for _, c := range r.TopCollections {
			fmt.Fprintf(b, "    %s  %-30s %d\n",
				uikit.TUI.TextBlue().Render(c.Key),
				c.Name, c.Count)
		}
		b.WriteString("\n")
	}

	if len(r.RecentAdded) > 0 {
		fmt.Fprintf(b, "  %s\n", uikit.TUI.Dim().Render("recently added:"))
		for _, ri := range r.RecentAdded {
			year := ""
			if ri.Year > 0 {
				year = fmt.Sprintf(" (%d)", ri.Year)
			}
			fmt.Fprintf(b, "    %s  %s%s  %s\n",
				uikit.TUI.TextBlue().Render(ri.Key),
				ri.Title, year,
				uikit.TUI.Dim().Render(ri.DateAdded))
		}
		b.WriteString("\n")
	}
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
		fmt.Fprintf(&b, "  %s wrote %s to %s\n", uikit.SymOK, r.Format, r.OutPath)
		if r.KeymapPath != "" {
			fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("keymap:"), r.KeymapPath)
		}
	}
	fmt.Fprintf(&b, "    %s %d item(s): %d pinned, %d synthesized",
		uikit.TUI.Dim().Render("·"),
		r.Stats.Total, r.Stats.Pinned, r.Stats.Synthesized)
	if r.Stats.Drifted > 0 {
		fmt.Fprintf(&b, ", %d drifted", r.Stats.Drifted)
	}
	b.WriteString("\n")
	// A bibliography carries references, so notes, attachments and
	// annotations are dropped on the way in. Say how many, or the count
	// above reads as the whole library and comes up short.
	if r.Stats.Skipped > 0 {
		fmt.Fprintf(&b, "    %s skipped %d non-bibliographic item(s) (notes, attachments, annotations)\n",
			uikit.TUI.Dim().Render("·"), r.Stats.Skipped)
	}
	return b.String()
}

// LibraryDumpResult is returned for `zot export --format ndjson` — the
// item-plane mirror. Distinct from LibraryExportResult because a dump has
// no cite-key stats and does carry a completeness sidecar.
type LibraryDumpResult struct {
	Scope    string    `json:"scope"`
	OutPath  string    `json:"out_path,omitempty"`
	MetaPath string    `json:"meta_path,omitempty"`
	Body     string    `json:"body,omitempty"`
	Stats    DumpStats `json:"stats"`
}

// JSON implements cmdutil.Result.
func (r LibraryDumpResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r LibraryDumpResult) Human() string {
	var b strings.Builder
	if r.OutPath == "" {
		b.WriteString(r.Body)
		if !strings.HasSuffix(r.Body, "\n") {
			b.WriteString("\n")
		}
	} else {
		fmt.Fprintf(&b, "  %s dumped %s library to %s\n", uikit.SymOK, r.Scope, r.OutPath)
		if r.MetaPath != "" {
			fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("meta:"), r.MetaPath)
		}
	}
	fmt.Fprintf(&b, "    %s %d item(s), %d collection(s)\n",
		uikit.TUI.Dim().Render("·"), r.Stats.Items, r.Stats.Collections)
	if r.OutPath == "" {
		fmt.Fprintf(&b, "    %s no --out: completeness sidecar not written\n",
			uikit.TUI.Dim().Render("!"))
	}
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
	sym := uikit.SymOK
	if !r.Launched {
		sym = uikit.SymFail
	}
	return fmt.Sprintf("  %s %s\n    %s\n", sym, r.Message, uikit.TUI.Dim().Render(r.Path))
}

// MultiStatsResult is returned for `zot info` without --library — summarizes
// every library the Zotero account has access to (personal + configured
// shared group). PerLibrary entries are rendered in order; errors are stashed
// so partial output still ships (e.g. shared group not synced yet).
type MultiStatsResult struct {
	PerLibrary []StatsResult `json:"per_library"`
	Errors     []string      `json:"errors,omitempty"`
}

// JSON implements cmdutil.Result.
func (r MultiStatsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r MultiStatsResult) Human() string {
	var b strings.Builder
	for _, s := range r.PerLibrary {
		b.WriteString(s.Human())
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "  %s %s\n", uikit.SymFail, e)
	}
	return b.String()
}

// OpenAlexSyncResult reports what `zot openalex sync` fetched and wrote.
//
// Every number that could hide a loss is on it: DOIs OpenAlex does not
// have, titles whose candidate list was capped, titles that matched
// nothing. A sync that reports only "wrote N works" cannot be audited,
// and its gaps become the consumer's invisible gaps.
type OpenAlexSyncResult struct {
	Scope        string        `json:"scope"`
	OutPath      string        `json:"out_path"`
	MetaPath     string        `json:"meta_path"`
	ItemsScanned int           `json:"items_scanned"`
	Stats        oacache.Stats `json:"stats"`
	NotFound     []string      `json:"not_found,omitempty"`
	// Cited describes the reference title pool written alongside the works.
	// Asked and Got are reported separately because their difference is the
	// share of the citation graph that stays unnameable, and a pool that
	// silently came back short would look identical to a complete one.
	CitedPath  string `json:"cited_path,omitempty"`
	CitedAsked int    `json:"cited_asked,omitempty"`
	CitedGot   int    `json:"cited_got,omitempty"`
	// Mode is "full" or "delta" — a full replace of the cache, or a
	// targeted fetch merged into the one already there. It is on the result
	// as well as in the sidecar because the two files answer to different
	// readers, and neither should have to infer this from record counts.
	Mode string `json:"mode,omitempty"`
	// Delta, Merged and CitedMerged are set only in delta mode: what the
	// run targeted, and what the merge did to each of the two bodies.
	Delta       *oacache.Delta `json:"delta,omitempty"`
	Merged      *oacache.Merge `json:"merged,omitempty"`
	CitedMerged *oacache.Merge `json:"cited_merged,omitempty"`
	// KeysUnmatched are --keys that named no bibliographic item. Reported
	// rather than dropped: a run that silently skipped the paper it was
	// asked for reports a successful sync of nothing.
	KeysUnmatched []string `json:"keys_unmatched,omitempty"`
	// MissingWithoutDOI counts items --missing could not judge because they
	// carry no DOI. See the CLI's targetItems.
	MissingWithoutDOI int `json:"missing_without_doi,omitempty"`
	// MissingKnownAbsent counts items --missing skipped because a previous
	// run already asked OpenAlex for their DOI and got nothing.
	MissingKnownAbsent int `json:"missing_known_absent,omitempty"`
}

// JSON implements cmdutil.Result.
func (r OpenAlexSyncResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r OpenAlexSyncResult) Human() string {
	var b strings.Builder
	if r.Mode == "delta" {
		return r.humanDelta()
	}
	fmt.Fprintf(&b, "  %s cached %d OpenAlex works for %d %s items in %d requests\n",
		uikit.SymOK, r.Stats.Works, r.ItemsScanned, r.Scope, r.Stats.Requests)
	if r.CitedAsked > 0 {
		fmt.Fprintf(&b, "    %s %d of %d cited works named -> %s\n",
			uikit.TUI.Dim().Render("pool:"), r.CitedGot, r.CitedAsked, r.CitedPath)
	}
	fmt.Fprintf(&b, "    %s %d of %d found; %d title lookups, %d with hits\n",
		uikit.TUI.Dim().Render("dois:"),
		r.Stats.DOIsFound, r.Stats.DOIsRequested,
		r.Stats.TitlesQueried, r.Stats.TitlesWithHits)
	// A capped candidate list is a truncation, and a truncation that is not
	// announced reads as a complete answer.
	if r.Stats.TitlesTruncated > 0 {
		fmt.Fprintf(&b, "    %s %d title lookups had more matches than the cap kept\n",
			uikit.TUI.Dim().Render("note:"), r.Stats.TitlesTruncated)
	}
	if n := len(r.NotFound); n > 0 {
		fmt.Fprintf(&b, "    %s %d DOIs are not in OpenAlex — listed in the sidecar.\n",
			uikit.TUI.Dim().Render("gap:"), n)
		fmt.Fprintf(&b, "      that is OpenAlex's coverage, not a claim the papers are not real\n")
	}
	// Reported apart from the DOI gap because it answers a different
	// question: not "what does OpenAlex lack" but "how much of this run
	// went on identifiers that did not work".
	if r.Stats.FallbackTitlesQueried > 0 {
		fmt.Fprintf(&b, "    %s %d of those fell back to a title lookup, %d recovered\n",
			uikit.TUI.Dim().Render("recovered:"),
			r.Stats.FallbackTitlesQueried, r.Stats.FallbackTitlesWithHits)
	}
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("out:"), r.OutPath)
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("meta:"), r.MetaPath)
	return b.String()
}

// humanDelta renders a targeted run. It leads with what MOVED rather than
// with what the file holds: a delta's headline number is small on purpose,
// and printing the cache's total first would read as a collapse.
func (r OpenAlexSyncResult) humanDelta() string {
	var b strings.Builder
	if r.Merged == nil {
		fmt.Fprintf(&b, "  %s nothing to fetch — every targeted item is already in the cache\n", uikit.SymOK)
	} else {
		fmt.Fprintf(&b, "  %s merged %d new and %d updated works into %d cached, in %d requests\n",
			uikit.SymOK, r.Merged.Added, r.Merged.Replaced, r.Merged.Total, r.Delta.Requests)
	}
	fmt.Fprintf(&b, "    %s %d items; %d of %d DOIs found, %d title lookups\n",
		uikit.TUI.Dim().Render("targeted:"), r.ItemsScanned,
		r.Stats.DOIsFound, r.Stats.DOIsRequested, r.Stats.TitlesQueried)
	if r.CitedMerged != nil && r.CitedMerged.Added > 0 {
		fmt.Fprintf(&b, "    %s %d newly named cited works (%d in the pool)\n",
			uikit.TUI.Dim().Render("pool:"), r.CitedMerged.Added, r.CitedMerged.Total)
	}
	// A key that named nothing is the difference between "your paper is
	// cached" and "your paper was never asked for".
	if n := len(r.KeysUnmatched); n > 0 {
		fmt.Fprintf(&b, "    %s %d key(s) matched no bibliographic item: %s\n",
			uikit.TUI.Dim().Render("gap:"), n, strings.Join(r.KeysUnmatched, ", "))
	}
	if r.MissingWithoutDOI > 0 {
		fmt.Fprintf(&b, "    %s %d items have no DOI, so --missing cannot judge them — name one with --keys\n",
			uikit.TUI.Dim().Render("note:"), r.MissingWithoutDOI)
	}
	if r.MissingKnownAbsent > 0 {
		fmt.Fprintf(&b, "    %s %d DOIs a previous run already found absent from OpenAlex were not re-asked\n",
			uikit.TUI.Dim().Render("held:"), r.MissingKnownAbsent)
	}
	if n := len(r.NotFound); n > 0 {
		fmt.Fprintf(&b, "    %s %d DOIs are not in OpenAlex — listed in the sidecar.\n",
			uikit.TUI.Dim().Render("gap:"), n)
		fmt.Fprintf(&b, "      that is OpenAlex's coverage, not a claim the papers are not real\n")
	}
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("out:"), r.OutPath)
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("meta:"), r.MetaPath)
	return b.String()
}

// OpenAlexEstimateResult prices a sync without making a request.
//
// It exists for a caller that must decide BEFORE it spends: the unattended
// pipeline runs under a request cap, and the only honest way to respect one
// is to know the bill in advance. Nothing on this result is a measurement
// of a run that happened — see [oacache.Plan] for which arms are certain
// and which is a bound.
type OpenAlexEstimateResult struct {
	Scope              string       `json:"scope"`
	StagingDir         string       `json:"staging_dir"`
	Plan               oacache.Plan `json:"plan"`
	Keys               []string     `json:"keys,omitempty"`
	KeysUnmatched      []string     `json:"keys_unmatched,omitempty"`
	MissingWithoutDOI  int          `json:"missing_without_doi,omitempty"`
	MissingKnownAbsent int          `json:"missing_known_absent,omitempty"`
}

// JSON implements cmdutil.Result.
func (r OpenAlexEstimateResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r OpenAlexEstimateResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s a %s sync of %d items would spend %d requests (at most %d)\n",
		uikit.SymOK, r.Plan.Mode, r.Plan.ItemsTargeted, r.Plan.Requests, r.Plan.RequestsMax)
	fmt.Fprintf(&b, "    %s %d DOI lookups (%d unbatchable), %d title lookups, %d cited batches\n",
		uikit.TUI.Dim().Render("arms:"), r.Plan.DOIRequests, r.Plan.DOIsUnbatchable,
		r.Plan.TitleRequests, r.Plan.CitedRequests)
	// The fallback arm is bounded, not priced: it fires once per DOI
	// OpenAlex turns out not to hold, and worst-casing it would treble
	// every estimate.
	if r.Plan.FallbackMax > 0 {
		fmt.Fprintf(&b, "    %s up to %d more if OpenAlex holds none of those DOIs\n",
			uikit.TUI.Dim().Render("bound:"), r.Plan.FallbackMax)
	}
	if n := len(r.KeysUnmatched); n > 0 {
		fmt.Fprintf(&b, "    %s %d key(s) matched no bibliographic item: %s\n",
			uikit.TUI.Dim().Render("gap:"), n, strings.Join(r.KeysUnmatched, ", "))
	}
	if r.MissingWithoutDOI > 0 {
		fmt.Fprintf(&b, "    %s %d items have no DOI, so --missing cannot judge them\n",
			uikit.TUI.Dim().Render("note:"), r.MissingWithoutDOI)
	}
	if r.MissingKnownAbsent > 0 {
		fmt.Fprintf(&b, "    %s %d DOIs already measured as absent from OpenAlex are not re-asked\n",
			uikit.TUI.Dim().Render("held:"), r.MissingKnownAbsent)
	}
	fmt.Fprintf(&b, "    %s nothing was fetched — drop --estimate to spend it\n",
		uikit.TUI.Dim().Render("dry:"))
	return b.String()
}

// CrossrefSyncResult reports what `zot crossref sync` swept and wrote.
//
// It separates two numbers that a single "misses" count would fuse:
// titles Crossref answered with nothing, and titles that could not be
// asked. The first is evidence — for a preprint or a pre-1950 paper,
// "Crossref has no DOI" is usually true and is the reason not to write
// one. The second is the absence of evidence, and letting a flaky network
// vote against a DOI would corrupt the agreement rate this sweep exists
// to measure.
type CrossrefSyncResult struct {
	Scope      string        `json:"scope"`
	OutPath    string        `json:"out_path"`
	MetaPath   string        `json:"meta_path"`
	TitlesSeen int           `json:"titles_seen"`
	Stats      xrcache.Stats `json:"stats"`
	Errored    []string      `json:"errored,omitempty"`
}

// JSON implements cmdutil.Result.
func (r CrossrefSyncResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r CrossrefSyncResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s swept %d titles against Crossref, kept %d candidates (%d requests)\n",
		uikit.SymOK, r.Stats.TitlesQueried, r.Stats.Candidates, r.Stats.Requests)
	fmt.Fprintf(&b, "    %s %d titles matched, %d matched nothing\n",
		uikit.TUI.Dim().Render("hits:"), r.Stats.TitlesWithHits, r.Stats.TitlesNoMatch)
	if r.Stats.TitlesNoMatch > 0 {
		fmt.Fprintf(&b, "      a title Crossref does not have is usually a preprint, a chapter,\n")
		fmt.Fprintf(&b, "      or a pre-DOI paper — evidence that no DOI should be written\n")
	}
	// Errored titles are the denominator problem. Any agreement rate
	// computed without excluding them is measured over questions that
	// were never asked.
	if n := len(r.Errored); n > 0 {
		fmt.Fprintf(&b, "    %s %d titles could not be asked — listed in the sidecar\n",
			uikit.TUI.Dim().Render("unknown:"), n)
		fmt.Fprintf(&b, "      these are NOT no-matches; exclude them before scoring agreement\n")
	}
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("out:"), r.OutPath)
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("meta:"), r.MetaPath)
	return b.String()
}

// CrossrefWorksResult reports what `zot crossref works` fetched and wrote.
//
// The same two-number discipline as CrossrefSyncResult, by DOI instead of
// by title: a DOI Crossref 404s is a finding (DataCite-registered DOIs —
// arXiv, OSF — are structurally absent from Crossref) and joins the
// sidecar's known-absent set so a delta never re-asks it; a DOI that
// could not be asked is not evidence of anything and is listed apart.
type CrossrefWorksResult struct {
	Scope    string `json:"scope"`
	OutPath  string `json:"out_path"`
	MetaPath string `json:"meta_path"`
	// DOIsInLibrary is the sweep's denominator: every distinct DOI a
	// bibliographic item carries in the swept scope.
	DOIsInLibrary int                `json:"dois_in_library"`
	RecordsTotal  int                `json:"records_total"`
	Stats         xrcache.WorksStats `json:"stats"`
	Errored       []string           `json:"errored,omitempty"`
}

// JSON implements cmdutil.Result.
func (r CrossrefWorksResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r CrossrefWorksResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s fetched %d of %d asked DOIs from Crossref (%d requests); cache now holds %d records\n",
		uikit.SymOK, r.Stats.DOIsFetched, r.Stats.DOIsAsked, r.Stats.Requests, r.RecordsTotal)
	fmt.Fprintf(&b, "    %s %d already cached or known absent, skipped; %d newly absent\n",
		uikit.TUI.Dim().Render("delta:"), r.Stats.DOIsSkipped, r.Stats.DOIsAbsent)
	if r.Stats.DOIsAbsent > 0 {
		fmt.Fprintf(&b, "      a DOI Crossref lacks is usually DataCite-registered (arXiv, OSF)\n")
		fmt.Fprintf(&b, "      — recorded in the sidecar so the next run does not re-ask\n")
	}
	if n := len(r.Errored); n > 0 {
		fmt.Fprintf(&b, "    %s %d DOIs could not be asked — listed in the sidecar\n",
			uikit.TUI.Dim().Render("unknown:"), n)
		fmt.Fprintf(&b, "      these are NOT absences; they will be re-asked next run\n")
	}
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("out:"), r.OutPath)
	fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("meta:"), r.MetaPath)
	return b.String()
}
