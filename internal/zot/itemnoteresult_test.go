package zot

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- NoteItemReadResult ---

func TestNoteItemReadResult_JSON_roundTripsAllPublicFields(t *testing.T) {
	t.Parallel()
	r := NoteItemReadResult{
		Key:         "NOTE1234",
		ParentItem:  "PAPER567",
		Collections: []string{"COLL1234"},
		Tags:        []string{"lit-review", "sr"},
		Body:        "<p>hello</p>",
		DateAdded:   "2026-04-21T00:00:00Z",
		ShowHTML:    true, // must not appear in JSON
	}
	out, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip: decode back and compare values. Avoids asserting on
	// HTML-escape details of the encoder (cmdutil.Output turns off
	// HTML-escaping at the encoder level — tests shouldn't pin byte form).
	var got NoteItemReadResult
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if got.Key != r.Key {
		t.Errorf("Key = %q, want %q", got.Key, r.Key)
	}
	if got.ParentItem != r.ParentItem {
		t.Errorf("ParentItem = %q, want %q", got.ParentItem, r.ParentItem)
	}
	if len(got.Collections) != 1 || got.Collections[0] != "COLL1234" {
		t.Errorf("Collections = %v", got.Collections)
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags = %v", got.Tags)
	}
	if got.Body != "<p>hello</p>" {
		t.Errorf("Body = %q", got.Body)
	}
	if got.DateAdded != "2026-04-21T00:00:00Z" {
		t.Errorf("DateAdded = %q", got.DateAdded)
	}
	// ShowHTML is json:"-" and must stay zero after round-trip.
	if got.ShowHTML {
		t.Error("ShowHTML should not round-trip through JSON")
	}
	// Sanity: the key `show_html` / `ShowHTML` must not appear anywhere
	// in the raw bytes.
	if strings.Contains(string(out), "show_html") || strings.Contains(string(out), "ShowHTML") {
		t.Errorf("ShowHTML leaked into JSON: %s", out)
	}
}

func TestNoteItemReadResult_Human_stripsTagsByDefault(t *testing.T) {
	t.Parallel()
	r := NoteItemReadResult{
		Key:  "NOTE1234",
		Body: "<h1>Title</h1><p>Hello <strong>world</strong></p>",
	}
	got := r.Human()
	if strings.Contains(got, "<h1>") || strings.Contains(got, "<strong>") {
		t.Errorf("default Human must strip tags, got:\n%s", got)
	}
	if !strings.Contains(got, "Title") || !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Errorf("text content lost:\n%s", got)
	}
}

func TestNoteItemReadResult_Human_rawHTMLWithFlag(t *testing.T) {
	t.Parallel()
	r := NoteItemReadResult{
		Key:      "NOTE1234",
		Body:     "<p>raw</p>",
		ShowHTML: true,
	}
	got := r.Human()
	if !strings.Contains(got, "<p>raw</p>") {
		t.Errorf("ShowHTML=true must preserve HTML, got:\n%s", got)
	}
}

func TestNoteItemReadResult_Human_parentAndTagsShown(t *testing.T) {
	t.Parallel()
	r := NoteItemReadResult{
		Key:        "NOTE1234",
		ParentItem: "PAPER567",
		Tags:       []string{"idea", "todo"},
		Body:       "<p>body</p>",
	}
	got := r.Human()
	if !strings.Contains(got, "PAPER567") {
		t.Errorf("parent key missing: %s", got)
	}
	if !strings.Contains(got, "idea") || !strings.Contains(got, "todo") {
		t.Errorf("tags missing: %s", got)
	}
}

// --- NoteItemListResult ---

func TestNoteItemListResult_Human_empty(t *testing.T) {
	t.Parallel()
	r := NoteItemListResult{ParentKey: "PAPER567", Count: 0}
	got := r.Human()
	if !strings.Contains(got, "PAPER567") {
		t.Errorf("parent key missing from empty message: %s", got)
	}
	if !strings.Contains(strings.ToLower(got), "no notes") {
		t.Errorf("empty-state message missing 'no notes': %s", got)
	}
}

func TestNoteItemListResult_Human_showsKeysAndSnippets(t *testing.T) {
	t.Parallel()
	r := NoteItemListResult{
		ParentKey: "PAPER567",
		Count:     2,
		Notes: []NoteItemListEntry{
			{Key: "NOTEAAAA", Body: "<p>First note body here</p>", Tags: []string{"idea"}},
			{Key: "NOTEBBBB", Body: "<p>Second note body</p>"},
		},
	}
	got := r.Human()
	if !strings.Contains(got, "NOTEAAAA") || !strings.Contains(got, "NOTEBBBB") {
		t.Errorf("keys missing: %s", got)
	}
	if !strings.Contains(got, "First note body") {
		t.Errorf("first snippet missing: %s", got)
	}
	if !strings.Contains(got, "Second note body") {
		t.Errorf("second snippet missing: %s", got)
	}
	if !strings.Contains(got, "idea") {
		t.Errorf("tag missing: %s", got)
	}
}

func TestNoteItemListResult_JSON_roundTripsFullBodies(t *testing.T) {
	t.Parallel()
	// Unlike Human, JSON must preserve the raw HTML body so LLM callers
	// can reason over structure.
	r := NoteItemListResult{
		ParentKey: "PAPER567",
		Count:     1,
		Notes:     []NoteItemListEntry{{Key: "NOTEAAAA", Body: "<h1>T</h1><p>b</p>"}},
	}
	out, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var got NoteItemListResult
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if got.ParentKey != "PAPER567" {
		t.Errorf("ParentKey = %q", got.ParentKey)
	}
	if got.Count != 1 {
		t.Errorf("Count = %d", got.Count)
	}
	if len(got.Notes) != 1 || got.Notes[0].Body != "<h1>T</h1><p>b</p>" {
		t.Errorf("Notes = %+v", got.Notes)
	}
}
