package local

import (
	"strings"
	"testing"
)

func TestResolvePDFAttachment_HappyPath(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// AAAA1111 is the journalArticle seeded in the shared fixture.
	// Its title is "Deep Learning for Neuroimaging" (from valueID 1);
	// its attachment DDDD4444 has contentType=application/pdf and
	// path=storage:deeplearning.pdf. ResolvePDFAttachment returns the
	// PARENT's title, not the attachment's.
	att, err := db.ResolvePDFAttachment("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if att.Key != "DDDD4444" {
		t.Errorf("Key = %q, want DDDD4444", att.Key)
	}
	if att.Filename != "deeplearning.pdf" {
		t.Errorf("Filename = %q, want deeplearning.pdf (storage: prefix must be stripped)", att.Filename)
	}
	if att.Title != "Deep Learning for Neuroimaging" {
		t.Errorf("Title = %q, want parent title 'Deep Learning for Neuroimaging'", att.Title)
	}
}

func TestResolvePDFAttachment_NoAttachments(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// BBBB2222 has no attachments at all.
	_, err := db.ResolvePDFAttachment("BBBB2222")
	if err == nil {
		t.Fatal("expected error for parent without PDF attachment")
	}
	if !strings.Contains(err.Error(), "BBBB2222") {
		t.Errorf("error should mention parent key; got: %v", err)
	}
}

func TestResolvePDFAttachment_MissingParent(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	_, err := db.ResolvePDFAttachment("NOSUCH00")
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
}

// TestListAllPDFAttachments returns one entry per non-trashed parent
// that owns at least one PDF child, ordered by parent key.
func TestListAllPDFAttachments(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	all, err := db.ListAllPDFAttachments()
	if err != nil {
		t.Fatal(err)
	}
	// Fixture has two parents with PDF attachments: AAAA1111 (itemID
	// 10, att DDDD4444) and BBBB2222 (itemID 20, att FFFF6666).
	// EEEE5555 (itemID 50) is trashed. ORPHANATT (itemID 60) is
	// standalone (no parent). So we expect exactly two rows.
	if len(all) != 2 {
		t.Fatalf("got %d parents, want 2; items: %+v", len(all), all)
	}
	if all[0].ParentKey != "AAAA1111" || all[0].Attachment.Key != "DDDD4444" {
		t.Errorf("row 0: %+v", all[0])
	}
	if all[0].Attachment.Title != "Deep Learning for Neuroimaging" {
		t.Errorf("row 0 title: %q (want parent's title)", all[0].Attachment.Title)
	}
	if all[1].ParentKey != "GGGG7777" || all[1].Attachment.Key != "HHHH8888" {
		t.Errorf("row 1: %+v", all[1])
	}
	if all[1].Attachment.Filename != "transformers.pdf" {
		t.Errorf("row 1 filename: %q", all[1].Attachment.Filename)
	}
	if all[1].Attachment.Title != "Attention Mechanisms in Cortical Networks" {
		t.Errorf("row 1 title: %q", all[1].Attachment.Title)
	}
}

func TestResolvePDFAttachment_TrashedParent(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// EEEE5555 (itemID 50) is flagged in deletedItems; resolver must
	// treat trashed parents as absent so we never re-extract against
	// something the user deleted.
	_, err := db.ResolvePDFAttachment("EEEE5555")
	if err == nil {
		t.Fatal("expected error for trashed parent")
	}
}
