package zot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func sampleItem() *local.Item {
	return &local.Item{
		Key:         "ABC12345",
		Type:        "journalArticle",
		Title:       "Deep Learning for Neuroimaging",
		Date:        "2024-03-15",
		DOI:         "10.1000/abc123",
		URL:         "https://example.org/abc",
		Abstract:    "An abstract.",
		Publication: "NeuroImage",
		Creators: []local.Creator{
			{Type: "author", First: "Alice", Last: "Smith"},
			{Type: "author", First: "Bob", Last: "Jones"},
			{Type: "editor", First: "Carol", Last: "Doe"},
		},
		Fields: map[string]string{
			"volume":      "42",
			"issue":       "7",
			"pages":       "100-120",
			"publisher":   "Elsevier",
			"citationKey": "smith2024deep",
		},
	}
}

func TestExport_CSLJSON(t *testing.T) {
	t.Parallel()
	out, err := ExportItem(sampleItem(), ExportCSLJSON)
	if err != nil {
		t.Fatal(err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(parsed) != 1 {
		t.Fatalf("len = %d, want 1", len(parsed))
	}
	item := parsed[0]
	if item["type"] != "article-journal" {
		t.Errorf("type = %v", item["type"])
	}
	if item["title"] != "Deep Learning for Neuroimaging" {
		t.Errorf("title = %v", item["title"])
	}
	if item["DOI"] != "10.1000/abc123" {
		t.Errorf("DOI = %v", item["DOI"])
	}
	if item["container-title"] != "NeuroImage" {
		t.Errorf("container-title = %v", item["container-title"])
	}
	authors, _ := item["author"].([]any)
	if len(authors) != 2 {
		t.Errorf("authors len = %d", len(authors))
	}
	editors, _ := item["editor"].([]any)
	if len(editors) != 1 {
		t.Errorf("editors len = %d", len(editors))
	}
	issued, _ := item["issued"].(map[string]any)
	if issued == nil {
		t.Error("missing issued")
	}
}

func TestExport_BibLaTeX(t *testing.T) {
	t.Parallel()
	out, err := ExportItem(sampleItem(), ExportBibLaTeX)
	if err != nil {
		t.Fatal(err)
	}
	must := []string{
		"@article{smith2024deep,",
		"title = {Deep Learning for Neuroimaging}",
		"author = {Smith, Alice and Jones, Bob}",
		"editor = {Doe, Carol}",
		"journaltitle = {NeuroImage}",
		"date = {2024-03-15}",
		"volume = {42}",
		"number = {7}",
		"pages = {100-120}",
		"publisher = {Elsevier}",
		"doi = {10.1000/abc123}",
		"url = {https://example.org/abc}",
	}
	for _, m := range must {
		if !strings.Contains(out, m) {
			t.Errorf("missing %q in:\n%s", m, out)
		}
	}
}

func TestExport_BibTeXAliasEmitsBibLaTeX(t *testing.T) {
	t.Parallel()
	// "bibtex" is accepted for backwards compatibility but sci emits one
	// .bib flavor only — BibLaTeX — so mixed-tool .bib files stay uniform.
	out, err := ExportItem(sampleItem(), ExportBibTeX)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "journaltitle = {NeuroImage}") {
		t.Errorf("bibtex alias did not emit biblatex fields:\n%s", out)
	}
}

func TestExport_BibLaTeXTypeMap(t *testing.T) {
	t.Parallel()
	// Types must match Zotero's built-in BibLaTeX translator so entries from
	// sci and from SciCite's shim look identical in one .bib.
	tests := map[string]string{
		"journalArticle":  "@article",
		"book":            "@book",
		"bookSection":     "@incollection",
		"conferencePaper": "@inproceedings",
		"thesis":          "@thesis",
		"report":          "@report",
		"webpage":         "@online",
		"artwork":         "@misc", // unmapped types fall through to misc
	}
	for zt, want := range tests {
		it := sampleItem()
		it.Type = zt
		out, err := ExportItem(it, ExportBibLaTeX)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out, want+"{") {
			t.Errorf("type %q: want prefix %q, got:\n%s", zt, want, out)
		}
	}
}

func TestExport_BibLaTeXSynthesizesWhenUnpinned(t *testing.T) {
	t.Parallel()
	// With no pinned citationKey, citekey.Resolve synthesizes a semantic
	// key with a Zotero-key suffix for uniqueness. The suffix is what
	// makes the result stable across drift and round-trippable to the
	// source item; see internal/zot/citekey for the full rationale.
	it := sampleItem()
	delete(it.Fields, "citationKey")
	out, _ := ExportItem(it, ExportBibLaTeX)
	if !strings.Contains(out, "@article{smith2024-deeplearneur-ABC12345,") {
		t.Errorf("expected synthesized key:\n%s", out)
	}
}

func TestExport_UnknownFormat(t *testing.T) {
	t.Parallel()
	if _, err := ExportItem(sampleItem(), ExportFormat("ris")); err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestExport_YearFromDate(t *testing.T) {
	t.Parallel()
	tests := map[string]int{
		"2024":                      2024,
		"2024-03-15":                2024,
		"2024-03-15 March 15, 2024": 2024, // Zotero dual-encoding
		"":                          0,
		"March 2024":                0,
		"abc":                       0,
	}
	for in, want := range tests {
		if got := yearFromDate(in); got != want {
			t.Errorf("yearFromDate(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestExport_BibLaTeX_InstitutionalAuthor(t *testing.T) {
	t.Parallel()
	// Zotero stores institutional creators like "NASA" with fieldMode=1,
	// which the local reader surfaces as Creator.Name (First/Last empty).
	// Bib(La)TeX must wrap these in braces so the name parser doesn't
	// split them as "last, first".
	it := sampleItem()
	it.Creators = []local.Creator{
		{Type: "author", Name: "NASA"},
		{Type: "author", First: "Alice", Last: "Smith"},
	}
	out, err := ExportItem(it, ExportBibLaTeX)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "author = {{NASA} and Smith, Alice}") {
		t.Errorf("institutional author not wrapped in braces:\n%s", out)
	}
}

func TestExport_BibLaTeX_EscapesBraces(t *testing.T) {
	t.Parallel()
	it := sampleItem()
	it.Title = `A {Curly} title with \backslash`
	out, err := ExportItem(it, ExportBibLaTeX)
	if err != nil {
		t.Fatal(err)
	}
	// A literal backslash is NOT "\\" — that is a line break in LaTeX, and
	// it is what this exporter used to emit. \textbackslash{} is the literal.
	want := `title = {A \{Curly\} title with \textbackslash{}backslash}`
	if !strings.Contains(out, want) {
		t.Errorf("escaping wrong:\nwant %q\ngot:\n%s", want, out)
	}
}

// TestExport_BibLaTeX_EscapesLaTeXSpecials is about whether the file
// COMPILES, not about typography.
//
// A bare & is a tabular alignment character and errors out wherever it
// lands; % comments the rest of the line; # $ _ are equally active. The
// live library carries 180 such values — 126 of them journal names like
// "Philosophical Transactions ... A & B" — so any manuscript citing one of
// those papers failed to build, and the failure points at the .bib rather
// than at the citation.
func TestExport_BibLaTeX_EscapesLaTeXSpecials(t *testing.T) {
	t.Parallel()
	it := sampleItem()
	it.Publication = "Attention & Perception"
	it.Title = "Growth of 50% in C#, cost $5, snake_case"
	out, err := ExportItem(it, ExportBibLaTeX)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`journaltitle = {Attention \& Perception}`,
		`title = {Growth of 50\% in C\#, cost \$5, snake\_case}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

// TestExport_BibLaTeX_LeavesVerbatimFieldsAlone.
//
// biblatex declares url and doi as VERBATIM fields: their content is not
// LaTeX-processed, and \url / hyperref take them as-is. Escaping an
// underscore there does not protect anything — it corrupts the address,
// and a DOI is a resolvable identifier, not prose. 113 DOIs and 131 URLs
// in the live library carry an underscore.
func TestExport_BibLaTeX_LeavesVerbatimFieldsAlone(t *testing.T) {
	t.Parallel()
	it := sampleItem()
	it.DOI = "10.1037/a0028_15"
	it.URL = "https://example.org/a_b?x=1&y=2#frag"
	out, err := ExportItem(it, ExportBibLaTeX)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`doi = {10.1037/a0028_15}`,
		`url = {https://example.org/a_b?x=1&y=2#frag}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestExport_BibLaTeX_DateFromZoteroDate(t *testing.T) {
	t.Parallel()
	// Zotero stores dates as "YYYY-MM-DD originalText" with 00-padding for
	// unspecified components; biblatex `date` wants the ISO part with the
	// unknown components trimmed.
	tests := map[string]string{
		"2024-03-15 March 15, 2024": "date = {2024-03-15}",
		"2024-03-00 March 2024":     "date = {2024-03}",
		"1871-00-00 1871":           "date = {1871}",
		"2024":                      "date = {2024}",
	}
	for in, want := range tests {
		it := sampleItem()
		it.Date = in
		out, _ := ExportItem(it, ExportBibLaTeX)
		if !strings.Contains(out, want) {
			t.Errorf("date %q: missing %q in:\n%s", in, want, out)
		}
	}
	// An unparseable date omits the field rather than guessing.
	it := sampleItem()
	it.Date = "March 2024"
	out, _ := ExportItem(it, ExportBibLaTeX)
	if strings.Contains(out, "date = {") {
		t.Errorf("unparseable date should omit the field:\n%s", out)
	}
}

func TestExport_CSLJSON_SingleNameAuthor(t *testing.T) {
	t.Parallel()
	it := sampleItem()
	it.Creators = []local.Creator{{Type: "author", Name: "NASA"}}
	out, err := ExportItem(it, ExportCSLJSON)
	if err != nil {
		t.Fatal(err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	authors := parsed[0]["author"].([]any)
	if len(authors) != 1 {
		t.Fatalf("authors len = %d", len(authors))
	}
	a := authors[0].(map[string]any)
	if a["literal"] != "NASA" {
		t.Errorf("literal = %v, want NASA", a["literal"])
	}
	if a["family"] != nil || a["given"] != nil {
		t.Errorf("family/given should be omitted: %+v", a)
	}
}
