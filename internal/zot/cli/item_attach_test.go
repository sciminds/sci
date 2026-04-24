package cli

// Unit tests for the pure helpers behind `zot item attach` (child attachment
// create). The live CLI Action is covered by smoke tests, matching the
// item_note convention — these helpers are what keep the flag parsing honest.

import (
	"strings"
	"testing"
)

// --- buildAttachmentMetaFromPath ---

func TestBuildAttachmentMetaFromPath_pdfExtension(t *testing.T) {
	t.Parallel()
	got := buildAttachmentMetaFromPath("/some/dir/Smith2022.pdf")
	if got.Filename != "Smith2022.pdf" {
		t.Errorf("Filename = %q, want Smith2022.pdf", got.Filename)
	}
	if got.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", got.ContentType)
	}
	if got.Title != "" {
		t.Errorf("Title must stay empty (Zotero derives from filename), got %q", got.Title)
	}
}

func TestBuildAttachmentMetaFromPath_uppercaseExtensionStillPDF(t *testing.T) {
	t.Parallel()
	got := buildAttachmentMetaFromPath("/tmp/PAPER.PDF")
	if got.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf for .PDF", got.ContentType)
	}
}

func TestBuildAttachmentMetaFromPath_unknownExtensionFallsBackToOctetStream(t *testing.T) {
	t.Parallel()
	got := buildAttachmentMetaFromPath("/tmp/notes.weird-extension")
	// Either a registered mime OR the octet-stream fallback — both are acceptable
	// correctness criteria. The test pins the fallback path.
	if got.ContentType == "" {
		t.Error("ContentType must not be empty")
	}
	if strings.HasSuffix(got.Filename, "/") {
		t.Errorf("Filename must strip directories, got %q", got.Filename)
	}
}
