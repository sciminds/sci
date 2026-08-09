package zot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/pkg/citekey"
)

// ExportFormat selects the output format for ExportItem.
type ExportFormat string

// Supported export formats. sci emits exactly one .bib flavor — BibLaTeX,
// matching Zotero's built-in BibLaTeX translator (which SciCite's BBT shim
// and vscode-zotero use) — so entries from every tool coexist in one file.
// "bibtex" is accepted as a legacy alias for that same output.
const (
	ExportCSLJSON  ExportFormat = "csl-json"
	ExportBibLaTeX ExportFormat = "biblatex"
	ExportBibTeX   ExportFormat = "bibtex" // alias for ExportBibLaTeX
)

// Canon returns the canonical name for a format, folding the legacy
// "bibtex" alias into "biblatex". Unknown values pass through unchanged so
// callers still get a precise error from ExportItem / ExportLibrary.
func (f ExportFormat) Canon() ExportFormat {
	if f == ExportBibTeX {
		return ExportBibLaTeX
	}
	return f
}

// ExportItem serializes a Zotero item into the requested citation format.
// Supported formats: csl-json (default), biblatex (basic; "bibtex" is an
// alias).
//
// Scope note: BibLaTeX output is intentionally minimal — it uses the
// existing citationKey field (populated for every item once sci or SciCite
// has ensured keys) and maps a small set of standard fields, using the same
// field vocabulary and type map as Zotero's built-in BibLaTeX translator.
// For fully-featured output including LaTeX escaping, export from the
// Zotero desktop app.
func ExportItem(it *local.Item, format ExportFormat) (string, error) {
	switch format.Canon() {
	case ExportCSLJSON, "":
		return exportCSLJSON(it)
	case ExportBibLaTeX:
		return exportBibLaTeX(it), nil
	default:
		return "", fmt.Errorf("unknown export format %q", format)
	}
}

// cslItem is the subset of CSL-JSON fields we emit.
type cslItem struct {
	ID             string      `json:"id"`
	Type           string      `json:"type"`
	Title          string      `json:"title,omitempty"`
	ContainerTitle string      `json:"container-title,omitempty"`
	DOI            string      `json:"DOI,omitempty"`
	URL            string      `json:"URL,omitempty"`
	Abstract       string      `json:"abstract,omitempty"`
	Volume         string      `json:"volume,omitempty"`
	Issue          string      `json:"issue,omitempty"`
	Page           string      `json:"page,omitempty"`
	Publisher      string      `json:"publisher,omitempty"`
	Author         []cslAuthor `json:"author,omitempty"`
	Editor         []cslAuthor `json:"editor,omitempty"`
	Issued         *cslDate    `json:"issued,omitempty"`
}

type cslAuthor struct {
	Family  string `json:"family,omitempty"`
	Given   string `json:"given,omitempty"`
	Literal string `json:"literal,omitempty"`
}

type cslDate struct {
	DateParts [][]int `json:"date-parts"`
}

// cslTypeMap projects Zotero item types to CSL-JSON types. Unknown types
// pass through unchanged — most Zotero types match CSL already.
var cslTypeMap = map[string]string{
	"journalArticle":  "article-journal",
	"book":            "book",
	"bookSection":     "chapter",
	"conferencePaper": "paper-conference",
	"thesis":          "thesis",
	"report":          "report",
	"webpage":         "webpage",
	"document":        "document",
	"manuscript":      "manuscript",
	"preprint":        "article",
}

func exportCSLJSON(it *local.Item) (string, error) {
	b, err := json.MarshalIndent([]cslItem{buildCSLItem(it)}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// buildCSLItem projects a local.Item into the CSL-JSON shape. Split out from
// exportCSLJSON so ExportLibrary can marshal a batch in one array.
func buildCSLItem(it *local.Item) cslItem {
	c := cslItem{
		ID:             it.Key,
		Type:           mapCSLType(it.Type),
		Title:          it.Title,
		ContainerTitle: it.Publication,
		DOI:            it.DOI,
		URL:            it.URL,
		Abstract:       it.Abstract,
		Volume:         it.Fields["volume"],
		Issue:          it.Fields["issue"],
		Page:           it.Fields["pages"],
		Publisher:      it.Fields["publisher"],
	}
	for _, cr := range it.Creators {
		a := cslAuthor{Family: cr.Last, Given: cr.First, Literal: cr.Name}
		switch cr.Type {
		case "editor":
			c.Editor = append(c.Editor, a)
		default:
			c.Author = append(c.Author, a)
		}
	}
	if y := yearFromDate(it.Date); y > 0 {
		c.Issued = &cslDate{DateParts: [][]int{{y}}}
	}
	return c
}

func mapCSLType(t string) string {
	if m, ok := cslTypeMap[t]; ok {
		return m
	}
	return t
}

func yearFromDate(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for i := range 4 {
		c := date[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

// bibEntryOpts carries per-entry knobs for writeBibEntry. Populated by
// ExportLibrary based on pinned/synthesized/drifted state; zero value yields
// a plain entry with no alias or zotero:// round-trip URI.
type bibEntryOpts struct {
	CiteKey   string // resolved cite-key (pinned or synthesized)
	IDsAlias  string // prior cite-key to emit as biblatex `ids = {...}`, or ""
	ZoteroURI string // zotero:// URI to append to `note`, or ""
}

// exportBibLaTeX is the single-item entry point. Resolves the cite-key via
// ResolveCiteKey (honoring pinned keys, then BBT-extra, then synthesis) and
// always appends a zotero:// round-trip URI to pinned entries so callers can
// round-trip back to the Zotero item regardless of cite-key drift.
func exportBibLaTeX(it *local.Item) string {
	key, synth := citekey.Resolve(it)
	opts := bibEntryOpts{CiteKey: key}
	if !synth {
		opts.ZoteroURI = zoteroSelectURI(it.Key)
	}
	return writeBibEntry(it, opts)
}

// writeBibEntry is the formatter shared by single-item and library export.
func writeBibEntry(it *local.Item, opts bibEntryOpts) string {
	entryType := bibTypeFor(it.Type)

	var b strings.Builder
	fmt.Fprintf(&b, "@%s{%s,\n", entryType, opts.CiteKey)
	if opts.IDsAlias != "" {
		writeBibField(&b, "ids", opts.IDsAlias)
	}
	writeBibField(&b, "title", it.Title)
	// Author/editor strings are already-structured BibTeX: they contain
	// protective braces around institutional names like {NASA} that must
	// survive intact. Write them raw — bibAuthors escapes any user-provided
	// content before wrapping.
	if authors := bibAuthors(it.Creators, "author"); authors != "" {
		writeBibFieldRaw(&b, "author", authors)
	}
	if editors := bibAuthors(it.Creators, "editor"); editors != "" {
		writeBibFieldRaw(&b, "editor", editors)
	}
	writeBibField(&b, "journaltitle", it.Publication)
	writeBibField(&b, "date", bibDate(it.Date))
	writeBibField(&b, "volume", it.Fields["volume"])
	writeBibField(&b, "number", it.Fields["issue"])
	writeBibField(&b, "pages", it.Fields["pages"])
	writeBibField(&b, "publisher", it.Fields["publisher"])
	// doi and url are biblatex VERBATIM fields: their content is not
	// LaTeX-processed, so escaping an underscore there corrupts the
	// address instead of protecting it.
	writeBibVerbatimField(&b, "doi", it.DOI)
	writeBibVerbatimField(&b, "url", it.URL)
	// `note` combines any user-authored prose from the Zotero `extra`
	// field with the zotero:// round-trip URI. User content always
	// survives — we append, never overwrite.
	noteBody := buildNoteField(it, opts.ZoteroURI)
	writeBibField(&b, "note", noteBody)
	b.WriteString("}\n")
	return b.String()
}

func buildNoteField(it *local.Item, zoteroURI string) string {
	user := citekey.ExtractNote(it.Fields["extra"])
	switch {
	case user != "" && zoteroURI != "":
		return user + "\n" + zoteroURI
	case user != "":
		return user
	default:
		return zoteroURI
	}
}

// bibTypeFor maps Zotero item types to biblatex entry types, mirroring the
// zotero2biblatexTypeMap in Zotero's built-in BibLaTeX translator for the
// types sci handles. Unknown types fall through to misc, same as upstream.
func bibTypeFor(zt string) string {
	switch zt {
	case "journalArticle":
		return "article"
	case "book":
		return "book"
	case "bookSection":
		return "incollection"
	case "conferencePaper":
		return "inproceedings"
	case "thesis":
		return "thesis"
	case "report":
		return "report"
	case "webpage":
		return "online"
	default:
		return "misc"
	}
}

func writeBibField(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	writeBibFieldRaw(b, name, bibEscape(value))
}

// writeBibFieldRaw writes a field whose value is already escaped / structured.
func writeBibFieldRaw(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "  %s = {%s},\n", name, value)
}

// writeBibVerbatimField writes a field biblatex treats as verbatim.
//
// Only the two characters that would break the FILE are touched: a raw
// brace ends the value early and desynchronises every entry after it.
// Everything else is left exactly as the publisher wrote it.
func writeBibVerbatimField(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	writeBibFieldRaw(b, name, strings.NewReplacer(`{`, `\{`, `}`, `\}`).Replace(value))
}

// bibEscape escapes a value destined for a LaTeX TEXT field.
//
// This is about whether the .bib COMPILES, not about typography. A bare &
// is a tabular alignment character and errors wherever it lands, % comments
// away the rest of the line, and # $ _ are equally active. The live library
// carries 180 such values — 126 of them journal names like "Philosophical
// Transactions ... A & B" — so a manuscript citing one of those papers
// failed to build, pointing at the .bib rather than at the citation.
//
// The backslash case was worse than missing: it mapped to `\\`, which is a
// LINE BREAK in LaTeX, not a literal backslash. \textbackslash{} is the
// literal, and the trailing {} is what keeps it from eating the next
// character.
//
// Replacement is single-pass (strings.Replacer), so the braces introduced
// by the backslash and tilde replacements are not themselves re-escaped —
// which is exactly what a naive sequence of ReplaceAll calls would do.
//
// Typography beyond compilation — smart quotes, dashes, math mode — still
// belongs in Better BibTeX; see the scope note on ExportItem.
func bibEscape(s string) string {
	return strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`{`, `\{`,
		`}`, `\}`,
		`&`, `\&`,
		`%`, `\%`,
		`$`, `\$`,
		`#`, `\#`,
		`_`, `\_`,
		`~`, `\textasciitilde{}`,
		`^`, `\textasciicircum{}`,
	).Replace(s)
}

func bibAuthors(creators []local.Creator, kind string) string {
	parts := lo.FilterMap(creators, func(c local.Creator, _ int) (string, bool) {
		if c.Type != kind && (kind != "author" || c.Type != "") {
			return "", false
		}
		if c.Name != "" {
			// Institutional author: escape content, then wrap in protective
			// braces so BibTeX does not try to parse "Last, First".
			return "{" + bibEscape(c.Name) + "}", true
		}
		return bibEscape(c.Last) + ", " + bibEscape(c.First), true
	})
	return strings.Join(parts, " and ")
}

// bibDate extracts the biblatex `date` value from Zotero's stored date,
// whose sortable form is "YYYY-MM-DD originalText" with 00-padding for
// unspecified components ("1871-00-00 1871" means year-only). The ISO token
// is kept with the unknown components trimmed; a date that doesn't lead
// with a 4-digit year is omitted rather than guessed.
func bibDate(s string) string {
	iso, _, _ := strings.Cut(s, " ")
	if firstDigits(iso, 4) == "" {
		return ""
	}
	for strings.HasSuffix(iso, "-00") {
		iso = strings.TrimSuffix(iso, "-00")
	}
	return iso
}

func firstDigits(s string, n int) string {
	if len(s) < n {
		return ""
	}
	for i := range n {
		if s[i] < '0' || s[i] > '9' {
			return ""
		}
	}
	return s[:n]
}
