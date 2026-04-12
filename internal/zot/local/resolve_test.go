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
