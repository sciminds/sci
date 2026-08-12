package cli

// Tests for the hashing half of `doctor duplicates --strategy content`.
// The clustering itself is unit-tested in internal/zot/hygiene; what
// matters here is that identical bytes in different storage dirs
// produce the same key, and that unreadable PDFs drop out silently
// instead of colliding on "".

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/sci/pkg/local"
)

// fakePDFLister is a local.Reader stub exposing only the one method
// buildContentKeys calls.
type fakePDFLister struct {
	local.Reader
	parents []local.PDFParent
}

func (f fakePDFLister) ListAllPDFAttachments() ([]local.PDFParent, error) {
	return f.parents, nil
}

// writeStoredPDF lays a file out the way Zotero does:
// <dataDir>/storage/<attachmentKey>/<filename>.
func writeStoredPDF(t *testing.T, dataDir, attKey, filename string, body []byte) {
	t.Helper()
	dir := filepath.Join(dataDir, "storage", attKey)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func parent(parentKey, attKey, filename string) local.PDFParent {
	return local.PDFParent{
		ParentKey:  parentKey,
		Attachment: local.PDFAttachment{Key: attKey, Filename: filename},
	}
}

func TestBuildContentKeys_IdenticalBytesShareAKey(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	body := []byte("%PDF-1.4\nthe same scan, downloaded twice\n")
	writeStoredPDF(t, dataDir, "ATT00001", "bartlett.pdf", body)
	writeStoredPDF(t, dataDir, "ATT00002", "remembering.pdf", body)
	writeStoredPDF(t, dataDir, "ATT00003", "other.pdf", []byte("%PDF-1.4\ndifferent\n"))

	db := fakePDFLister{parents: []local.PDFParent{
		parent("AAAA1111", "ATT00001", "bartlett.pdf"),
		parent("BBBB2222", "ATT00002", "remembering.pdf"),
		parent("CCCC3333", "ATT00003", "other.pdf"),
	}}

	keys, err := buildContentKeys(context.Background(), db, dataDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("keys = %d, want 3: %+v", len(keys), keys)
	}
	if keys["AAAA1111"] != keys["BBBB2222"] {
		t.Errorf("same bytes produced different keys: %q vs %q", keys["AAAA1111"], keys["BBBB2222"])
	}
	if keys["CCCC3333"] == keys["AAAA1111"] {
		t.Error("different bytes produced the same key")
	}
	if keys["AAAA1111"] == "" {
		t.Error("content key is empty")
	}
}

// A missing file must drop out of the map entirely. Mapping it to ""
// would be worse than useless: every missing PDF would then share a
// key with every other missing PDF.
func TestBuildContentKeys_MissingFilesAreOmitted(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	writeStoredPDF(t, dataDir, "ATT00001", "present.pdf", []byte("%PDF-1.4\nhere\n"))

	db := fakePDFLister{parents: []local.PDFParent{
		parent("AAAA1111", "ATT00001", "present.pdf"),
		parent("BBBB2222", "ATT00002", "gone.pdf"),
		parent("CCCC3333", "ATT00003", "also-gone.pdf"),
	}}

	keys, err := buildContentKeys(context.Background(), db, dataDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys = %+v, want only the present file", keys)
	}
	if _, ok := keys["BBBB2222"]; ok {
		t.Error("a missing PDF earned a content key")
	}
}

func TestBuildContentKeys_ReportsProgress(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	for _, k := range []string{"ATT00001", "ATT00002", "ATT00003"} {
		writeStoredPDF(t, dataDir, k, "f.pdf", []byte("%PDF-1.4\n"+k+"\n"))
	}
	db := fakePDFLister{parents: []local.PDFParent{
		parent("A", "ATT00001", "f.pdf"),
		parent("B", "ATT00002", "f.pdf"),
		parent("C", "ATT00003", "f.pdf"),
	}}

	var calls int
	var lastDone, lastTotal int
	if _, err := buildContentKeys(context.Background(), db, dataDir, func(done, total int) {
		calls++
		lastDone, lastTotal = done, total
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("progress calls = %d, want 3", calls)
	}
	if lastDone != 3 || lastTotal != 3 {
		t.Errorf("final progress = %d/%d, want 3/3", lastDone, lastTotal)
	}
}

func TestBuildContentKeys_CancelledContext(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	writeStoredPDF(t, dataDir, "ATT00001", "f.pdf", []byte("%PDF-1.4\nx\n"))
	db := fakePDFLister{parents: []local.PDFParent{parent("A", "ATT00001", "f.pdf")}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := buildContentKeys(ctx, db, dataDir, nil); err == nil {
		t.Error("want the cancellation surfaced, not a partial map presented as complete")
	}
}

func TestBuildContentKeys_EmptyLibrary(t *testing.T) {
	t.Parallel()
	keys, err := buildContentKeys(context.Background(), fakePDFLister{}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("keys = %+v, want empty", keys)
	}
}
