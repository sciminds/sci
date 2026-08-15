package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot/notemd"
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

// ItemResult is returned for `sci zot item read <key>` with a single key.
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

// ItemsResult is returned for `sci zot item read` with more than one key —
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
// look for its tags — without having to know `sci zot link list` exists.
//
// Predicate handling mirrors LinkListResult.Human: dc:relation renders as
// "related" (it is the user's own link), and Zotero's own bookkeeping
// predicates render last, under their real names, so they are never mistaken
// for something the user put there.
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
// returned by `sci zot item children`. Mirrors local.ChildItem verbatim —
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

// ChildrenListResult is returned for `sci zot item children <KEY>`:
// a flat listing of a parent item's child items (attachments + notes),
// read from the local database. Used both by humans and by scripts that
// feed the child keys into a later read (`sci zot notes read`,
// `sci zot item open`).
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
	for _, r := range notemd.StripProvenance(local.UnwrapZoteroDiv(html)) {
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

// CollectionListResult is returned for `sci zot collection list`.
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

// TagListResult is returned for `sci zot tags list`.
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

// StatsResult is returned for `sci zot info --library X` and as the building
// block of MultiStatsResult (one entry per library).
//
// Scope is the bare scope token ("personal" or "shared"); Library is the
// human label ("shared (sciminds)"). LibraryAPIID carries the numeric
// Zotero group id for a shared library — so agents can build
// `zotero://select/groups/{id}/...` deeplinks without grepping the on-disk
// config — and is empty for personal, which has no such id.
type StatsResult struct {
	Library      string      `json:"library"`
	Scope        string      `json:"scope,omitempty"`
	LibraryAPIID string      `json:"library_api_id,omitempty"`
	Stats        local.Stats `json:"stats"`
	DataDir      string      `json:"data_dir"`
	Schema       int         `json:"schema_version"`

	// Orient fields — populated only when `info --orient` is set. Kept
	// optional via omitempty so the default `info` shape stays unchanged.
	// Source: pkg/local/orient.go.
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
		fmt.Fprintf(b, "    %d / %d items (%.1f%%)\n\n",
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

// ExportResult is returned for `sci zot item export` (single-item). Body
// holds the emitted citation when streaming to stdout; when the user passed
// -o, Body is empty and OutPath points at the file on disk — the same split
// LibraryExportResult uses, so the whole export family reads alike.
type ExportResult struct {
	Key     string `json:"key"`
	Format  string `json:"format"`
	OutPath string `json:"out_path,omitempty"`
	Body    string `json:"body,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ExportResult) JSON() any { return r }

// Human implements cmdutil.Result. Streaming to stdout, the citation is the
// entire output — see [ExportResult.Diagnostics].
func (r ExportResult) Human() string {
	if r.OutPath == "" {
		return r.Body + "\n"
	}
	return fmt.Sprintf("  %s wrote %s to %s\n", uikit.SymOK, r.Format, r.OutPath)
}

// Diagnostics implements cmdutil.Diagnostician — the one-item footer, on
// stderr, so `sci zot item export KEY --format csl-json > ref.json` stays
// valid JSON. `zot bib`, `zot export` and `zot search --export` all report
// what they emitted; this one used to report nothing, so the family answered
// the same question two different ways.
func (r ExportResult) Diagnostics() string {
	if r.OutPath != "" {
		return ""
	}
	return fmt.Sprintf("    %s 1 item(s): %s as %s\n",
		uikit.TUI.Dim().Render("·"), r.Key, r.Format)
}

// LibraryExportResult is returned for `sci zot export` (full library) and
// for `sci zot search --export` (filtered subset). Body holds the emitted document
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
//
// Streaming to stdout (no -o), the body IS the output and nothing else may
// join it: `sci zot export --format csl-json > refs.json` has to produce a
// file that parses. The stats move to [LibraryExportResult.Diagnostics],
// which cmdutil.Output writes to stderr. With -o the human output is a
// status report rather than a document, so the footer stays here — splitting
// one message across two streams would only scatter it.
func (r LibraryExportResult) Human() string {
	if r.OutPath == "" {
		return r.Body + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s wrote %s to %s\n", uikit.SymOK, r.Format, r.OutPath)
	if r.KeymapPath != "" {
		fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("keymap:"), r.KeymapPath)
	}
	b.WriteString(r.statsFooter())
	return b.String()
}

// Diagnostics implements cmdutil.Diagnostician — the cite-key stats, on
// stderr, and only when the document went to stdout.
func (r LibraryExportResult) Diagnostics() string {
	if r.OutPath != "" {
		return ""
	}
	return r.statsFooter()
}

// statsFooter renders the cite-key tally shared by both output modes.
func (r LibraryExportResult) statsFooter() string {
	var b strings.Builder
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

// LibraryDumpResult is returned for `sci zot export --format ndjson` — the
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

// Human implements cmdutil.Result. Streaming to stdout, the ndjson body is
// the whole output — see [LibraryDumpResult.Diagnostics].
func (r LibraryDumpResult) Human() string {
	if r.OutPath == "" {
		if strings.HasSuffix(r.Body, "\n") {
			return r.Body
		}
		return r.Body + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s dumped %s to %s\n", uikit.SymOK, r.scopeLabel(), r.OutPath)
	if r.MetaPath != "" {
		fmt.Fprintf(&b, "    %s %s\n", uikit.TUI.Dim().Render("meta:"), r.MetaPath)
	}
	b.WriteString(r.statsFooter())
	return b.String()
}

// Diagnostics implements cmdutil.Diagnostician — the record tally and the
// missing-sidecar caveat, on stderr, so a redirected dump is pure ndjson.
func (r LibraryDumpResult) Diagnostics() string {
	if r.OutPath != "" {
		return ""
	}
	return r.statsFooter() + fmt.Sprintf("    %s no --out: completeness sidecar not written\n",
		uikit.TUI.Dim().Render("!"))
}

// statsFooter renders the record tally shared by both output modes.
func (r LibraryDumpResult) statsFooter() string {
	return fmt.Sprintf("    %s %d item(s), %d collection(s)\n",
		uikit.TUI.Dim().Render("·"), r.Stats.Items, r.Stats.Collections)
}

// scopeLabel names the pool that was dumped. `--library all` is a merged
// read pool over personal + shared, so it is plural — "all library" named
// something that does not exist.
func (r LibraryDumpResult) scopeLabel() string {
	if r.Scope == string(LibAll) {
		return "all libraries"
	}
	return r.Scope + " library"
}

// OpenResult is returned for `sci zot item open`.
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

// MultiStatsResult is returned for `sci zot info` without --library —
// summarizes every library the local database holds (personal + the
// configured shared group). PerLibrary entries are rendered in order; errors
// are stashed so partial output still ships (e.g. shared group not synced
// yet).
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
