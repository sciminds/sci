package zot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/zot/local"
)

// ExportFormat selects the output format for ExportItem.
type ExportFormat string

const (
	ExportCSLJSON ExportFormat = "csl-json"
	ExportBibTeX  ExportFormat = "bibtex"
)

// ExportItem serializes a Zotero item into the requested citation format.
// Supported formats: csl-json (default), bibtex (basic).
//
// Scope note: BibTeX output is intentionally minimal — it uses the existing
// citationKey field (populated by Better BibTeX for every item in typical
// libraries) and maps a small set of standard fields. For fully-featured
// BibTeX including LaTeX escaping and cite-key derivation, use Better BibTeX's
// own export from the Zotero desktop app.
func ExportItem(it *local.Item, format ExportFormat) (string, error) {
	switch format {
	case ExportCSLJSON, "":
		return exportCSLJSON(it)
	case ExportBibTeX:
		return exportBibTeX(it), nil
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
	for i := 0; i < 4; i++ {
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

// exportBibTeX is the single-item entry point. Resolves the cite-key via
// ResolveCiteKey (honoring pinned keys, then BBT-extra, then synthesis) and
// always appends a zotero:// round-trip URI to pinned entries so callers can
// round-trip back to the Zotero item regardless of cite-key drift.
func exportBibTeX(it *local.Item) string {
	key, synth := ResolveCiteKey(it)
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
	writeBibField(&b, "journal", it.Publication)
	writeBibField(&b, "year", firstDigits(it.Date, 4))
	writeBibField(&b, "volume", it.Fields["volume"])
	writeBibField(&b, "number", it.Fields["issue"])
	writeBibField(&b, "pages", it.Fields["pages"])
	writeBibField(&b, "publisher", it.Fields["publisher"])
	writeBibField(&b, "doi", it.DOI)
	writeBibField(&b, "url", it.URL)
	// `note` combines any user-authored prose from the Zotero `extra`
	// field with the zotero:// round-trip URI. User content always
	// survives — we append, never overwrite.
	noteBody := buildNoteField(it, opts.ZoteroURI)
	writeBibField(&b, "note", noteBody)
	b.WriteString("}\n")
	return b.String()
}

func buildNoteField(it *local.Item, zoteroURI string) string {
	user := extractExtraNote(it.Fields["extra"])
	switch {
	case user != "" && zoteroURI != "":
		return user + "\n" + zoteroURI
	case user != "":
		return user
	default:
		return zoteroURI
	}
}

func bibTypeFor(zt string) string {
	switch zt {
	case "journalArticle":
		return "article"
	case "book":
		return "book"
	case "bookSection":
		return "inbook"
	case "conferencePaper":
		return "inproceedings"
	case "thesis":
		return "phdthesis"
	case "report":
		return "techreport"
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

// bibEscape performs minimal BibTeX escaping: braces and backslashes.
// Anything more sophisticated belongs in Better BibTeX — see the scope note
// on ExportItem.
func bibEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `{`, `\{`, `}`, `\}`).Replace(s)
}

func bibAuthors(creators []local.Creator, kind string) string {
	parts := make([]string, 0, len(creators))
	for _, c := range creators {
		if c.Type != kind && (kind != "author" || c.Type != "") {
			continue
		}
		if c.Name != "" {
			// Institutional author: escape content, then wrap in protective
			// braces so BibTeX does not try to parse "Last, First".
			parts = append(parts, "{"+bibEscape(c.Name)+"}")
		} else {
			parts = append(parts, bibEscape(c.Last)+", "+bibEscape(c.First))
		}
	}
	return strings.Join(parts, " and ")
}

func firstDigits(s string, n int) string {
	if len(s) < n {
		return ""
	}
	for i := 0; i < n; i++ {
		if s[i] < '0' || s[i] > '9' {
			return ""
		}
	}
	return s[:n]
}
