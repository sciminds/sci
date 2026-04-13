package local

import "testing"

// ── ListDoclingNotes ─────────────────────────────────────────────────

func TestListDoclingNotes_HappyPath(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// AAAA1111 has one docling-tagged child note (NOTECH10).
	got, err := db.ListDoclingNotes("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.Key != "NOTECH10" {
		t.Errorf("Key = %q, want NOTECH10", n.Key)
	}
	if n.ItemType != "note" {
		t.Errorf("ItemType = %q, want note", n.ItemType)
	}
	if n.Title != "Extraction Note" {
		t.Errorf("Title = %q, want Extraction Note", n.Title)
	}
	if n.Note != "<p>Extracted via docling.</p>" {
		t.Errorf("Note = %q", n.Note)
	}
	if len(n.Tags) != 1 || n.Tags[0] != "docling" {
		t.Errorf("Tags = %v, want [docling]", n.Tags)
	}
}

func TestListDoclingNotes_NoDoclingNotes(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// GGGG7777 has a PDF attachment but no docling notes.
	got, err := db.ListDoclingNotes("GGGG7777")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0: %+v", len(got), got)
	}
}

func TestListDoclingNotes_NoChildren(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// BBBB2222 has no children at all.
	got, err := db.ListDoclingNotes("BBBB2222")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0: %+v", len(got), got)
	}
}

// ── ListAllDoclingNotes ──────────────────────────────────────────────

func TestListAllDoclingNotes(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ListAllDoclingNotes()
	if err != nil {
		t.Fatal(err)
	}
	// Fixture has exactly one docling-tagged note: NOTECH10, child of AAAA1111.
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	s := got[0]
	if s.NoteKey != "NOTECH10" {
		t.Errorf("NoteKey = %q, want NOTECH10", s.NoteKey)
	}
	if s.ParentKey != "AAAA1111" {
		t.Errorf("ParentKey = %q, want AAAA1111", s.ParentKey)
	}
	if s.ParentTitle != "Deep Learning for Neuroimaging" {
		t.Errorf("ParentTitle = %q", s.ParentTitle)
	}
	if s.Body != "<p>Extracted via docling.</p>" {
		t.Errorf("Body = %q", s.Body)
	}
	if len(s.Tags) != 1 || s.Tags[0] != "docling" {
		t.Errorf("Tags = %v, want [docling]", s.Tags)
	}
}

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
	if _, err := r.ListDoclingNotes("AAAA1111"); err != nil {
		t.Errorf("ListDoclingNotes via Reader: %v", err)
	}
	if _, err := r.ListAllDoclingNotes(); err != nil {
		t.Errorf("ListAllDoclingNotes via Reader: %v", err)
	}
	if _, err := r.ReadNote("NOTECH10"); err != nil {
		t.Errorf("ReadNote via Reader: %v", err)
	}
}
