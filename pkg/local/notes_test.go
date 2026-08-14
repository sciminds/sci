package local

import "testing"

// ── ReadNote ─────────────────────────────────────────────────────────

func TestReadNote_HappyPath(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ReadNote("NOTECH10")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "NOTECH10" {
		t.Errorf("Key = %q, want NOTECH10", got.Key)
	}
	if got.ParentKey != "AAAA1111" {
		t.Errorf("ParentKey = %q, want AAAA1111", got.ParentKey)
	}
	if got.Title != "Extraction Note" {
		t.Errorf("Title = %q, want Extraction Note", got.Title)
	}
	if got.Body != "<p>Extracted via docling.</p>" {
		t.Errorf("Body = %q", got.Body)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "docling" {
		t.Errorf("Tags = %v, want [docling]", got.Tags)
	}
}

func TestReadNote_NotFound(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	_, err := db.ReadNote("NOSUCH00")
	if err == nil {
		t.Fatal("expected error for missing note")
	}
}

func TestReadNote_NotANote(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// DDDD4444 is an attachment, not a note.
	_, err := db.ReadNote("DDDD4444")
	if err == nil {
		t.Fatal("expected error for non-note item")
	}
}

// ── Reader interface compliance ──────────────────────────────────────

func TestNoteMethods_ViaReader(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	var r Reader = db
	if _, err := r.ListNotes(); err != nil {
		t.Errorf("ListNotes via Reader: %v", err)
	}
	if _, err := r.ReadNote("NOTECH10"); err != nil {
		t.Errorf("ReadNote via Reader: %v", err)
	}
}

// ListNotes returns the notes a human wrote, not the extractions sci
// posted. It must include standalone notes (no
// parent) — those are exactly the ones a docling-shaped query drops, and
// they're the ones worth linking to items later.
func TestListNotes_ExcludesExtractionsIncludesStandalone(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ListNotes()
	if err != nil {
		t.Fatal(err)
	}

	keys := make(map[string]NoteSummary, len(got))
	for _, n := range got {
		keys[n.NoteKey] = n
	}

	if _, ok := keys["NOTECH10"]; ok {
		t.Error("NOTECH10 is a docling extraction and must not appear in ListNotes")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (ORPHNNOTE + NOTECH11): %+v", len(got), got)
	}

	standalone, ok := keys["ORPHNNOTE"]
	if !ok {
		t.Fatal("standalone note ORPHNNOTE missing — a parent JOIN dropped it")
	}
	if standalone.ParentKey != "" {
		t.Errorf("standalone ParentKey = %q, want empty", standalone.ParentKey)
	}

	attached, ok := keys["NOTECH11"]
	if !ok {
		t.Fatal("attached note NOTECH11 missing")
	}
	if attached.ParentKey != "BBBB2222" {
		t.Errorf("ParentKey = %q, want BBBB2222", attached.ParentKey)
	}
	if attached.ParentTitle == "" {
		t.Error("attached note should carry its parent's title")
	}
}
