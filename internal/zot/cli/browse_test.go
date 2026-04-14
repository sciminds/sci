package cli

import (
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

// ── tagEntry ─────────────────────────────────────────────────────────

func TestTagEntryTitle(t *testing.T) {
	e := tagEntry{tag: local.Tag{Name: "neuroscience", Count: 42}}
	if got := e.Title(); got != "neuroscience" {
		t.Errorf("Title() = %q, want %q", got, "neuroscience")
	}
}

func TestTagEntryDescription(t *testing.T) {
	e := tagEntry{tag: local.Tag{Name: "x", Count: 1}}
	if got := e.Description(); got != "1 items" {
		t.Errorf("Description() = %q, want %q", got, "1 items")
	}
}

func TestTagEntryFilterValue(t *testing.T) {
	e := tagEntry{tag: local.Tag{Name: "deep-learning"}}
	if got := e.FilterValue(); got != "deep-learning" {
		t.Errorf("FilterValue() = %q, want %q", got, "deep-learning")
	}
}

// ── collEntry ────────────────────────────────────────────────────────

func TestCollEntryTitle(t *testing.T) {
	e := collEntry{coll: local.Collection{Name: "Brain Papers", Key: "ABC123"}}
	if got := e.Title(); got != "Brain Papers" {
		t.Errorf("Title() = %q, want %q", got, "Brain Papers")
	}
}

func TestCollEntryDescription(t *testing.T) {
	e := collEntry{coll: local.Collection{Name: "x", ItemCount: 12, Key: "ABC123"}}
	if got := e.Description(); got != "12 items · ABC123" {
		t.Errorf("Description() = %q, want %q", got, "12 items · ABC123")
	}
}

func TestCollEntryFilterValue(t *testing.T) {
	e := collEntry{coll: local.Collection{Name: "Methods"}}
	if got := e.FilterValue(); got != "Methods" {
		t.Errorf("FilterValue() = %q, want %q", got, "Methods")
	}
}

// ── itemEntry ────────────────────────────────────────────────────────

func TestItemEntryTitleFull(t *testing.T) {
	e := itemEntry{item: local.Item{
		Title:    "Deep Learning for Brain Imaging",
		Date:     "2024-01-15",
		Creators: []local.Creator{{Last: "Smith"}},
	}}
	want := "Smith 2024 — Deep Learning for Brain Imaging"
	if got := e.Title(); got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}

func TestItemEntryTitleNoCreator(t *testing.T) {
	e := itemEntry{item: local.Item{
		Title: "Orphaned Paper",
		Date:  "2023-06-01",
	}}
	want := "2023 — Orphaned Paper"
	if got := e.Title(); got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}

func TestItemEntryTitleNoDate(t *testing.T) {
	e := itemEntry{item: local.Item{
		Title:    "Undated Paper",
		Creators: []local.Creator{{Last: "Jones"}},
	}}
	want := "Jones — Undated Paper"
	if got := e.Title(); got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}

func TestItemEntryTitleInstitutionalAuthor(t *testing.T) {
	e := itemEntry{item: local.Item{
		Title:    "Space Report",
		Date:     "2020-03-10",
		Creators: []local.Creator{{Name: "NASA"}},
	}}
	want := "NASA 2020 — Space Report"
	if got := e.Title(); got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}

func TestItemEntryTitleOnly(t *testing.T) {
	e := itemEntry{item: local.Item{Title: "Just a Title"}}
	if got := e.Title(); got != "Just a Title" {
		t.Errorf("Title() = %q, want %q", got, "Just a Title")
	}
}

func TestItemEntryTitleFallbackKey(t *testing.T) {
	e := itemEntry{item: local.Item{Key: "ABC12345"}}
	if got := e.Title(); got != "ABC12345" {
		t.Errorf("Title() = %q, want %q", got, "ABC12345")
	}
}

func TestItemEntryDescription(t *testing.T) {
	e := itemEntry{item: local.Item{Type: "journalArticle", DOI: "10.1000/test"}}
	want := "journalArticle · 10.1000/test"
	if got := e.Description(); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func TestItemEntryDescriptionNoDOI(t *testing.T) {
	e := itemEntry{item: local.Item{Type: "book", Key: "XYZ789"}}
	want := "book · XYZ789"
	if got := e.Description(); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func TestItemEntryFilterValue(t *testing.T) {
	e := itemEntry{item: local.Item{
		Title:    "My Paper",
		Creators: []local.Creator{{Last: "Smith"}},
		DOI:      "10.1000/x",
	}}
	got := e.FilterValue()
	for _, want := range []string{"My Paper", "Smith", "10.1000/x"} {
		if !contains(got, want) {
			t.Errorf("FilterValue() = %q, missing %q", got, want)
		}
	}
}

// ── buildActions ─────────────────────────────────────────────────────

func TestBuildActionsWithAttachment(t *testing.T) {
	it := &local.Item{
		Key:   "TEST0001",
		Title: "Test Paper",
		Attachments: []local.Attachment{
			{Key: "ATT00001", ContentType: "application/pdf", Filename: "paper.pdf", LinkMode: 0},
		},
	}
	menu := buildActions("/data", it)
	// All 3 actions should be present, PDF should be enabled
	if menu.Picked() != -1 {
		t.Error("menu should not have a pick yet")
	}
	// Verify cursor starts at 0 (first enabled)
	if menu.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", menu.Cursor())
	}
}

func TestBuildActionsNoAttachment(t *testing.T) {
	it := &local.Item{
		Key:   "TEST0002",
		Title: "No PDF Paper",
	}
	menu := buildActions("/data", it)
	// Cursor should skip the disabled PDF action (index 1) if starting from 0
	// Index 0 (Copy BibTeX) is enabled, so cursor stays at 0
	if menu.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", menu.Cursor())
	}
}

// helper

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
