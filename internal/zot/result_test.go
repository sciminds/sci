package zot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestCleanDate(t *testing.T) {
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
	r := ListResult{Count: 0}
	if !strings.Contains(r.Human(), "no items") {
		t.Errorf("empty ListResult.Human() = %q", r.Human())
	}

	r = ListResult{Count: 0, Query: "quantum"}
	if !strings.Contains(r.Human(), "no results for") || !strings.Contains(r.Human(), "quantum") {
		t.Errorf("empty query ListResult.Human() = %q", r.Human())
	}
}

func TestListResult_Populated(t *testing.T) {
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
	}}
	out := r.Human()
	for _, want := range []string{
		"A Paper", "ABC12345", "journalArticle",
		"Alice Smith", "NASA",
		"2024-03-15", // cleaned
		"10.1/abc", "NeuroImage", "Hello.",
		"ml", "COLLAAA1", "p.pdf",
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

func TestItemResult_JSONIsItem(t *testing.T) {
	// ItemResult.JSON() returns the inner Item so callers see the same
	// shape as the underlying local package — verify that contract.
	r := ItemResult{Item: local.Item{Key: "ABC12345"}}
	b, _ := json.Marshal(r.JSON())
	if !strings.Contains(string(b), `"key":"ABC12345"`) {
		t.Errorf("JSON shape: %s", b)
	}
}

func TestItemResult_Untitled(t *testing.T) {
	r := ItemResult{Item: local.Item{Key: "X", Type: "book"}}
	if !strings.Contains(r.Human(), "(untitled)") {
		t.Errorf("missing untitled fallback")
	}
}

func TestStatsResult_Human(t *testing.T) {
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

func TestExportResult(t *testing.T) {
	r := ExportResult{Key: "K", Format: "bibtex", Body: "@article{K,}\n"}
	if r.Human() != "@article{K,}\n\n" {
		t.Errorf("Human = %q", r.Human())
	}
	b, _ := json.Marshal(r.JSON())
	if !strings.Contains(string(b), `"format":"bibtex"`) {
		t.Errorf("JSON: %s", b)
	}
}

func TestOpenResult(t *testing.T) {
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

func TestWriteResult(t *testing.T) {
	r := WriteResult{Action: "trashed", Kind: "item", Target: "ABC12345"}
	if !strings.Contains(r.Human(), "trashed item ABC12345") {
		t.Errorf("default Human = %q", r.Human())
	}
	// Explicit message overrides the default sentence.
	r = WriteResult{Action: "trashed", Kind: "item", Target: "ABC12345", Message: "custom"}
	if !strings.Contains(r.Human(), "custom") || strings.Contains(r.Human(), "trashed item") {
		t.Errorf("custom message should replace default: %q", r.Human())
	}
}

func TestSetupResult(t *testing.T) {
	r := SetupResult{OK: true, LibraryID: "42", DataDir: "/z", Message: "configured"}
	out := r.Human()
	for _, want := range []string{"configured", "42", "/z"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Failure case hides library/data dir details.
	r = SetupResult{OK: false, LibraryID: "42", DataDir: "/z", Message: "failed"}
	out = r.Human()
	if !strings.Contains(out, "failed") {
		t.Errorf("missing 'failed' in: %q", out)
	}
	if strings.Contains(out, "/z") {
		t.Errorf("data dir leaked on failure: %q", out)
	}
}
