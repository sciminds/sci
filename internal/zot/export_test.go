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

func TestExport_BibTeX(t *testing.T) {
	t.Parallel()
	out, err := ExportItem(sampleItem(), ExportBibTeX)
	if err != nil {
		t.Fatal(err)
	}
	must := []string{
		"@article{smith2024deep,",
		"title = {Deep Learning for Neuroimaging}",
		"author = {Smith, Alice and Jones, Bob}",
		"editor = {Doe, Carol}",
		"journal = {NeuroImage}",
		"year = {2024}",
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

func TestExport_BibTeXSynthesizesWhenUnpinned(t *testing.T) {
	t.Parallel()
	// With no pinned citationKey, citekey.Resolve synthesizes a semantic
	// key with a Zotero-key suffix for uniqueness. The suffix is what
	// makes the result stable across drift and round-trippable to the
	// source item; see internal/zot/citekey for the full rationale.
	it := sampleItem()
	delete(it.Fields, "citationKey")
	out, _ := ExportItem(it, ExportBibTeX)
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

func TestExport_BibTeX_InstitutionalAuthor(t *testing.T) {
	t.Parallel()
	// Zotero stores institutional creators like "NASA" with fieldMode=1,
	// which the local reader surfaces as Creator.Name (First/Last empty).
	// BibTeX must wrap these in braces so BibTeX doesn't parse them as
	// "last, first".
	it := sampleItem()
	it.Creators = []local.Creator{
		{Type: "author", Name: "NASA"},
		{Type: "author", First: "Alice", Last: "Smith"},
	}
	out, err := ExportItem(it, ExportBibTeX)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "author = {{NASA} and Smith, Alice}") {
		t.Errorf("institutional author not wrapped in braces:\n%s", out)
	}
}

func TestExport_BibTeX_EscapesBraces(t *testing.T) {
	t.Parallel()
	it := sampleItem()
	title := `A {Curly} title with \backslash`
	it.Title = title
	out, err := ExportItem(it, ExportBibTeX)
	if err != nil {
		t.Fatal(err)
	}
	// Order of replacement matters: "{" → "\{", "}" → "\}", "\" → "\\" —
	// the replacer runs left-to-right in the order given, so a literal
	// backslash in the input becomes "\\" AND "\{" gets its newly-added
	// backslash doubled. Verify the final form round-trips through the
	// BibTeX escape contract: no unbalanced raw braces in field values.
	want := `title = {A \{Curly\} title with \\backslash}`
	if !strings.Contains(out, want) {
		t.Errorf("escaping wrong:\nwant %q\ngot:\n%s", want, out)
	}
}

func TestExport_BibTeX_DualEncodedDateYear(t *testing.T) {
	t.Parallel()
	it := sampleItem()
	it.Date = "2024-03-15 March 15, 2024"
	out, _ := ExportItem(it, ExportBibTeX)
	if !strings.Contains(out, "year = {2024}") {
		t.Errorf("year not extracted from dual-encoded date:\n%s", out)
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
