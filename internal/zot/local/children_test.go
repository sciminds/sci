package local

import "testing"

func TestListChildren(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// Item 10 has two children: attachment DDDD4444 and note NOTECH10.
	got, err := db.ListChildren("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}

	// Ordered by dateAdded: attachment (2024-01-01 10:05) then note (2024-01-02).
	att := got[0]
	if att.Key != "DDDD4444" {
		t.Errorf("child[0].Key = %q, want DDDD4444", att.Key)
	}
	if att.ItemType != "attachment" {
		t.Errorf("child[0].ItemType = %q, want attachment", att.ItemType)
	}
	if att.Filename != "deeplearning.pdf" {
		t.Errorf("child[0].Filename = %q, want deeplearning.pdf", att.Filename)
	}
	if att.ContentType != "application/pdf" {
		t.Errorf("child[0].ContentType = %q, want application/pdf", att.ContentType)
	}

	note := got[1]
	if note.Key != "NOTECH10" {
		t.Errorf("child[1].Key = %q, want NOTECH10", note.Key)
	}
	if note.ItemType != "note" {
		t.Errorf("child[1].ItemType = %q, want note", note.ItemType)
	}
	if note.Title != "Extraction Note" {
		t.Errorf("child[1].Title = %q, want Extraction Note", note.Title)
	}
	if note.Note != "<p>Extracted via docling.</p>" {
		t.Errorf("child[1].Note = %q", note.Note)
	}
	// Note 90 is tagged "docling".
	if len(note.Tags) != 1 || note.Tags[0] != "docling" {
		t.Errorf("child[1].Tags = %v, want [docling]", note.Tags)
	}
}

func TestListChildren_NoChildren(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// Item 20 has no children.
	got, err := db.ListChildren("BBBB2222")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0: %+v", len(got), got)
	}
}

func TestListChildren_ViaReader(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// Verify ListChildren is accessible through the Reader interface.
	var r Reader = db
	got, err := r.ListChildren("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Reader.ListChildren: len = %d, want 2", len(got))
	}
}
