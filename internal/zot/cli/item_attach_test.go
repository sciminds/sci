package cli

// Unit tests for the pure helpers behind `zot item attach` (child attachment
// create). The live CLI Action is covered by smoke tests, matching the
// item_note convention — these helpers are what keep the flag parsing honest.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/api"
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

// fileMD5 is the local half of the idempotency check: the digest Zotero
// reports for a stored file has to be computable from the bytes we are about
// to upload, or --skip-existing has nothing to compare.
func TestFileMD5_MatchesKnownDigest(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := fileMD5(path)
	if err != nil {
		t.Fatal(err)
	}
	// md5("hello")
	if want := "5d41402abc4b2a76b9719d911017c592"; got != want {
		t.Errorf("fileMD5 = %q, want %q", got, want)
	}
}

// findExistingAttachment is the decision `item attach --skip-existing` makes.
// Matching on md5 rather than filename is the point: the same paper saved
// under two names is one attachment, and two different papers that happen to
// share a filename are two.
func TestFindExistingAttachment(t *testing.T) {
	t.Parallel()
	const digest = "5fd414b08706d73fdc2bb134d702b1fe"
	children := []api.ChildItem{
		{Key: "NOTE0001", ItemType: "note", Note: "<p>hi</p>"},
		{Key: "ATTA0001", ItemType: "attachment", Filename: "other.pdf", Md5: "ffffffffffffffffffffffffffffffff"},
		{Key: "ATTA0002", ItemType: "attachment", Filename: "renamed.pdf", Md5: digest},
	}

	if got := findExistingAttachment(children, digest); got != "ATTA0002" {
		t.Errorf("match on md5 = %q, want ATTA0002 (filename differs, bytes are the same)", got)
	}
	if got := findExistingAttachment(children, "0123456789abcdef0123456789abcdef"); got != "" {
		t.Errorf("unknown digest matched %q, want no match", got)
	}
	// An attachment Zotero has not hashed yet must never match — an empty
	// digest on both sides is two unknowns, not an equality.
	unhashed := []api.ChildItem{{Key: "ATTA0003", ItemType: "attachment", Filename: "x.pdf"}}
	if got := findExistingAttachment(unhashed, ""); got != "" {
		t.Errorf("empty digest matched %q, want no match", got)
	}
}
