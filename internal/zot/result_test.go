package zot

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sciminds/sci/internal/local"
)

func TestCleanDate(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"":                          "",
		"2024":                      "2024",
		"2024-03-15":                "2024-03-15",
		"2024-03-15 March 15, 2024": "2024-03-15", // Zotero dual-encoding
		"2024-03-15\tMarch 15":      "2024-03-15", // tab-delimited variant
		" 2024":                     "",           // leading space → empty
	}
	for in, want := range tests {
		if got := cleanDate(in); got != want {
			t.Errorf("cleanDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListResult_Empty(t *testing.T) {
	t.Parallel()
	r := ListResult{Count: 0}
	if !strings.Contains(r.Human(), "no items") {
		t.Errorf("empty ListResult.Human() = %q", r.Human())
	}

	r = ListResult{Count: 0, Query: "quantum"}
	if !strings.Contains(r.Human(), "no results for") || !strings.Contains(r.Human(), "quantum") {
		t.Errorf("empty query ListResult.Human() = %q", r.Human())
	}
}

func TestListResult_ZeroHit_WithScopeAndHint(t *testing.T) {
	t.Parallel()
	r := ListResult{
		Count: 0,
		Query: "mcp",
		Scope: "title, DOI, publication (local)",
		Hint:  "widen with --library all to search personal and shared together",
	}
	out := r.Human()
	if !strings.Contains(out, "no results for") {
		t.Errorf("missing no-results line:\n%s", out)
	}
	if !strings.Contains(out, "title, DOI, publication") {
		t.Errorf("scope not shown on zero-hit:\n%s", out)
	}
	if !strings.Contains(out, "--library all") {
		t.Errorf("hint not shown on zero-hit:\n%s", out)
	}
}

func TestListResult_Populated(t *testing.T) {
	t.Parallel()
	r := ListResult{
		Count: 2,
		Items: []local.Item{
			{
				Key:         "AAAA1111",
				Title:       "Deep Learning",
				Type:        "journalArticle",
				Date:        "2024-03-15 March 15, 2024",
				Publication: "NeuroImage",
			},
			{
				Key:  "BBBB2222",
				Type: "book", // untitled
			},
		},
	}
	out := r.Human()
	// Item 1 shows title, cleaned year, and publication.
	if !strings.Contains(out, "Deep Learning") {
		t.Errorf("missing title:\n%s", out)
	}
	if !strings.Contains(out, "(2024)") {
		t.Errorf("year not cleaned from dual-encoded date:\n%s", out)
	}
	if !strings.Contains(out, "NeuroImage") {
		t.Errorf("missing publication:\n%s", out)
	}
	// Item 2 shows (untitled) fallback.
	if !strings.Contains(out, "(untitled)") {
		t.Errorf("missing untitled fallback:\n%s", out)
	}
	// Summary line.
	if !strings.Contains(out, "2 item(s)") {
		t.Errorf("missing count summary:\n%s", out)
	}
}

func TestListResult_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	r := ListResult{Count: 1, Library: 42, Items: []local.Item{{Key: "X"}}}
	b, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"library_id":42`) {
		t.Errorf("library_id not in JSON: %s", b)
	}
}

func TestItemResult_Human(t *testing.T) {
	t.Parallel()
	r := ItemResult{Item: local.Item{
		Key:   "ABC12345",
		Type:  "journalArticle",
		Title: "A Paper",
		Date:  "2024-03-15 March 15, 2024",
		Creators: []local.Creator{
			{Type: "author", First: "Alice", Last: "Smith"},
			{Type: "author", Name: "NASA"},
		},
		DOI:         "10.1/abc",
		Publication: "NeuroImage",
		Abstract:    "Hello.",
		Tags:        []string{"ml"},
		Collections: []string{"COLLAAA1"},
		Attachments: []local.Attachment{{Key: "ATT1", Filename: "p.pdf"}},
		Relations: &local.ItemRelationSet{
			Related: []string{"NOTE0001"},
			Other:   map[string][]string{"owl:sameAs": {"GRPCOPY1"}},
			Titles:  map[string]string{"NOTE0001": "Reading notes on prediction"},
		},
	}}
	out := r.Human()
	for _, want := range []string{
		"A Paper", "ABC12345", "journalArticle",
		"Alice Smith", "NASA",
		"2024-03-15", // cleaned
		"10.1/abc", "NeuroImage", "Hello.",
		"ml", "COLLAAA1", "p.pdf",
		// Related items render like tags/attachments do: you should not
		// have to know `link list` exists to see what an item is related to.
		"related", "NOTE0001", "Reading notes on prediction",
		// Zotero's own predicates keep their real names so nothing suggests
		// `link rm` should be pointed at them.
		"owl:sameAs", "GRPCOPY1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// dual-encoded original text must NOT leak into display.
	if strings.Contains(out, "March 15, 2024") {
		t.Errorf("original date text leaked into display:\n%s", out)
	}
}

// Relations hang off local.Item, not off ItemResult, precisely so they
// survive JSON() returning the bare item. Pin both halves of that.
func TestItemResult_JSONCarriesRelationsAtTopLevel(t *testing.T) {
	t.Parallel()
	r := ItemResult{Item: local.Item{
		Key:       "ABC12345",
		Relations: &local.ItemRelationSet{Related: []string{"NOTE0001"}},
	}}
	b, _ := json.Marshal(r.JSON())
	if !strings.Contains(string(b), `"relations":{"related":["NOTE0001"]}`) {
		t.Errorf("relations not at the top level of the item JSON: %s", b)
	}
}

// An item with no relations emits no `relations` key at all — every
// existing agent's parse of `item read --json` stays byte-identical.
func TestItemResult_JSONOmitsAbsentRelations(t *testing.T) {
	t.Parallel()
	b, _ := json.Marshal(ItemResult{Item: local.Item{Key: "ABC12345"}}.JSON())
	if strings.Contains(string(b), "relations") {
		t.Errorf("relations key present on a relation-free item: %s", b)
	}
}

func TestItemResult_HumanWithoutRelations(t *testing.T) {
	t.Parallel()
	out := ItemResult{Item: local.Item{Key: "ABC12345", Title: "A Paper"}}.Human()
	if strings.Contains(out, "related") {
		t.Errorf("related block rendered for a relation-free item:\n%s", out)
	}
}

func TestItemResult_JSONIsItem(t *testing.T) {
	t.Parallel()
	// ItemResult.JSON() returns the inner Item so callers see the same
	// shape as the underlying local package — verify that contract.
	r := ItemResult{Item: local.Item{Key: "ABC12345"}}
	b, _ := json.Marshal(r.JSON())
	if !strings.Contains(string(b), `"key":"ABC12345"`) {
		t.Errorf("JSON shape: %s", b)
	}
}

func TestItemResult_Untitled(t *testing.T) {
	t.Parallel()
	r := ItemResult{Item: local.Item{Key: "X", Type: "book"}}
	if !strings.Contains(r.Human(), "(untitled)") {
		t.Errorf("missing untitled fallback")
	}
}

func TestStatsResult_Human(t *testing.T) {
	t.Parallel()
	r := StatsResult{
		DataDir: "/home/u/Zotero",
		Schema:  125,
		Stats: local.Stats{
			TotalItems: 10, WithDOI: 7, WithAbstract: 3,
			WithAttachment: 5, Collections: 2, Tags: 4,
			ByType: map[string]int{"journalArticle": 7, "book": 3},
		},
	}
	out := r.Human()
	for _, want := range []string{
		"/home/u/Zotero", "schema v125",
		"journalArticle", "book",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// journalArticle (7) must come before book (3) — sorted desc by count.
	ja := strings.Index(out, "journalArticle")
	bk := strings.Index(out, "book")
	if ja < 0 || bk < 0 || ja > bk {
		t.Errorf("by-type order wrong (expect journalArticle before book):\n%s", out)
	}
}

func TestListBriefResult_HumanMultibyteAbstract(t *testing.T) {
	t.Parallel()
	// Byte-slicing a multibyte abstract cut mid-rune and emitted invalid
	// UTF-8; truncation must respect rune boundaries.
	r := ListBriefResult{
		Count: 1,
		Items: []ItemBrief{{Key: "AAAA1111", Title: "T", Abstract: "a" + strings.Repeat("é", 200)}},
	}
	if out := r.Human(); !utf8.ValidString(out) {
		t.Errorf("abstract preview truncation produced invalid UTF-8:\n%q", out)
	}
}

func TestNoteSnippet_MultibyteTruncation(t *testing.T) {
	t.Parallel()
	got := noteSnippet("<p>" + strings.Repeat("é", 100) + "</p>")
	if !utf8.ValidString(got) {
		t.Errorf("noteSnippet produced invalid UTF-8: %q", got)
	}
}

func TestStatsResult_HumanByTypeTieOrder(t *testing.T) {
	t.Parallel()
	// Equal counts must fall back to name order — the old map iteration made
	// ties shuffle between runs of the same `sci zot info`.
	r := StatsResult{
		Stats: local.Stats{
			ByType: map[string]int{
				"thesis": 2, "book": 2, "report": 2, "journalArticle": 2, "conferencePaper": 2,
			},
		},
	}
	out := r.Human()
	prev := -1
	for _, name := range []string{"book", "conferencePaper", "journalArticle", "report", "thesis"} {
		idx := strings.Index(out, name)
		if idx < 0 {
			t.Fatalf("missing %q:\n%s", name, out)
		}
		if idx < prev {
			t.Fatalf("tied by-type entries not in name order:\n%s", out)
		}
		prev = idx
	}
}

// TestStatsResult_OrientCoverageReportsAndStops pins the first output the
// guide tells an agent to run: `info --orient` reports how much of the
// library has an extraction and stops there. Naming a verb that reads that
// text would send the reader to a command this surface does not register.
func TestStatsResult_OrientCoverageReportsAndStops(t *testing.T) {
	t.Parallel()
	r := StatsResult{
		ExtractionCoverage: &local.ExtractionCoverage{
			TotalItems: 100, WithExtraction: 87, Percent: 87.0,
		},
	}
	out := r.Human()
	if !strings.Contains(out, "87 / 100") {
		t.Errorf("orient block should report the coverage counts:\n%s", out)
	}
	for _, banned := range []string{"zot llm", "zot read", "zot query", "zot binary"} {
		if strings.Contains(out, banned) {
			t.Errorf("orient block names %q, which is not a command this surface has:\n%s", banned, out)
		}
	}
}

func TestExportResult(t *testing.T) {
	t.Parallel()
	r := ExportResult{Key: "K", Format: "biblatex", Body: "@article{K,}\n"}
	if r.Human() != "@article{K,}\n\n" {
		t.Errorf("Human = %q", r.Human())
	}
	b, _ := json.Marshal(r.JSON())
	if !strings.Contains(string(b), `"format":"biblatex"`) {
		t.Errorf("JSON: %s", b)
	}
}

func TestOpenResult(t *testing.T) {
	t.Parallel()
	// Launched = success symbol.
	r := OpenResult{Key: "K", Path: "/tmp/p.pdf", Launched: true, Message: "opened"}
	if !strings.Contains(r.Human(), "opened") || !strings.Contains(r.Human(), "/tmp/p.pdf") {
		t.Errorf("launched Human = %q", r.Human())
	}
	// Not launched = failure symbol (different visual).
	r.Launched = false
	r.Message = "no attachment"
	if !strings.Contains(r.Human(), "no attachment") {
		t.Errorf("unlaunched Human = %q", r.Human())
	}
}

// TestConfig_HasNoSecretToMask replaces the masking tests that lived here.
// A zot config is a data directory and an optional shared group — there is
// no credential in it, so both surfaces emit the stored shape verbatim and
// there is nothing to strip. The fence that matters now is the inverse:
// neither surface may resurrect a credential field.
func TestConfig_HasNoSecretToMask(t *testing.T) {
	t.Parallel()
	cfg := Config{
		SharedGroupID:   "6506098",
		SharedGroupName: "sciminds",
		DataDir:         "/Users/test/Zotero",
	}

	out := cfg.Human()
	for _, want := range []string{"/Users/test/Zotero", "sciminds", "6506098"} {
		if !strings.Contains(out, want) {
			t.Errorf("Human() missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "api key") || strings.Contains(out, "user id") {
		t.Errorf("Human() still renders a credential line:\n%s", out)
	}

	b, err := json.Marshal(cfg.JSON())
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, `"data_dir":"/Users/test/Zotero"`) {
		t.Errorf("data_dir missing from JSON:\n%s", js)
	}
	for _, gone := range []string{"api_key", "has_api_key", "user_id"} {
		if strings.Contains(js, gone) {
			t.Errorf("JSON still carries retired field %q:\n%s", gone, js)
		}
	}
}

func TestSetupResult(t *testing.T) {
	t.Parallel()
	r := SetupResult{OK: true, SharedGroup: "sciminds", DataDir: "/z", Message: "configured"}
	out := r.Human()
	for _, want := range []string{"configured", "sciminds", "/z"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Failure case hides library/data dir details.
	r = SetupResult{OK: false, SharedGroup: "sciminds", DataDir: "/z", Message: "failed"}
	out = r.Human()
	if !strings.Contains(out, "failed") {
		t.Errorf("missing 'failed' in: %q", out)
	}
	if strings.Contains(out, "/z") {
		t.Errorf("data dir leaked on failure: %q", out)
	}
}
