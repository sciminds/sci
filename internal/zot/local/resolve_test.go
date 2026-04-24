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
// that owns at least one PDF child, ordered by dateAdded DESC (newest first).
func TestListAllPDFAttachments(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	all, err := db.ListAllPDFAttachments()
	if err != nil {
		t.Fatal(err)
	}
	// Fixture has two parents with PDF attachments: AAAA1111 (itemID
	// 10, dateAdded 2024-01-01, att DDDD4444) and GGGG7777 (itemID 80,
	// dateAdded 2024-06-01, att HHHH8888).
	// EEEE5555 (itemID 50) is trashed. ORPHANATT (itemID 60) is
	// standalone (no parent). So we expect exactly two rows, newest first.
	if len(all) != 2 {
		t.Fatalf("got %d parents, want 2; items: %+v", len(all), all)
	}
	// Row 0: GGGG7777 (2024-06-01) — most recently added.
	if all[0].ParentKey != "GGGG7777" || all[0].Attachment.Key != "HHHH8888" {
		t.Errorf("row 0: %+v", all[0])
	}
	if all[0].Attachment.Filename != "transformers.pdf" {
		t.Errorf("row 0 filename: %q", all[0].Attachment.Filename)
	}
	if all[0].Attachment.Title != "Attention Mechanisms in Cortical Networks" {
		t.Errorf("row 0 title: %q", all[0].Attachment.Title)
	}
	// Row 1: AAAA1111 (2024-01-01) — oldest.
	if all[1].ParentKey != "AAAA1111" || all[1].Attachment.Key != "DDDD4444" {
		t.Errorf("row 1: %+v", all[1])
	}
	if all[1].Attachment.Title != "Deep Learning for Neuroimaging" {
		t.Errorf("row 1 title: %q (want parent's title)", all[1].Attachment.Title)
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

// TestParentsWithDoclingNotesMissingTag exercises the backfill query.
// It's the inverse of ParentsWithDoclingNotes filtered by parent-tag
// presence: returns parents that own a docling note but lack the named
// tag on the parent itself (not on the child).
//
// AAAA1111 has docling note NOTECH10 + parent tags neuroimaging,
// deep-learning. So:
//   - MissingTag("has-markdown") includes AAAA1111 (lacks the tag).
//   - MissingTag("neuroimaging") excludes AAAA1111 (already tagged).
func TestParentsWithDoclingNotesMissingTag(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	missing, err := db.ParentsWithDoclingNotesMissingTag("has-markdown")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "AAAA1111" {
		t.Errorf("MissingTag(has-markdown) = %v, want [AAAA1111]", missing)
	}

	excluded, err := db.ParentsWithDoclingNotesMissingTag("neuroimaging")
	if err != nil {
		t.Fatal(err)
	}
	if len(excluded) != 0 {
		t.Errorf("MissingTag(neuroimaging) = %v, want [] (parent already tagged)", excluded)
	}
}
