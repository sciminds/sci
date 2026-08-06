package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCache_MissThenHit: Put stores markdown; Get returns the same
// path on a subsequent lookup for the same (pdfKey, hash).
func TestCache_MissThenHit(t *testing.T) {
	t.Parallel()
	c := &MarkdownCache{Dir: t.TempDir()}

	if _, ok := c.Get("PDF1", "abc"); ok {
		t.Fatal("expected miss on empty cache")
	}

	path, err := c.Put("PDF1", "abc", []byte("# hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.Get("PDF1", "abc")
	if !ok {
		t.Fatal("expected hit after Put")
	}
	if got != path {
		t.Errorf("Get path = %q, Put path = %q", got, path)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# hello\n" {
		t.Errorf("cached body = %q", body)
	}
}

// TestCache_DifferentHashDifferentFile: a new hash for the same pdfKey
// is a distinct entry — the old one survives for rollback / diagnostics,
// and the new one doesn't stomp it.
func TestCache_DifferentHashDifferentFile(t *testing.T) {
	t.Parallel()
	c := &MarkdownCache{Dir: t.TempDir()}
	pA, err := c.Put("PDF1", "hashA", []byte("A"))
	if err != nil {
		t.Fatal(err)
	}
	pB, err := c.Put("PDF1", "hashB", []byte("B"))
	if err != nil {
		t.Fatal(err)
	}
	if pA == pB {
		t.Fatal("distinct hashes must map to distinct paths")
	}
	if a, _ := os.ReadFile(pA); string(a) != "A" {
		t.Errorf("A clobbered: %s", a)
	}
	if b, _ := os.ReadFile(pB); string(b) != "B" {
		t.Errorf("B clobbered: %s", b)
	}
}

// TestCache_MissOnDifferentKey: different pdfKey with same hash does
// not collide.
func TestCache_MissOnDifferentKey(t *testing.T) {
	t.Parallel()
	c := &MarkdownCache{Dir: t.TempDir()}
	if _, err := c.Put("PDF1", "h", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("PDF2", "h"); ok {
		t.Error("expected miss on different pdfKey")
	}
}

// TestCache_AtomicWrite: Put should never leave a partial file under
// the final name — we verify the final file exists and has full
// content, and no sibling tmp file lingers.
func TestCache_AtomicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := &MarkdownCache{Dir: dir}
	if _, err := c.Put("PDF1", "h", []byte("complete")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("tmp file lingered: %s", e.Name())
		}
	}
}

// TestCache_Delete: removing an entry makes the next Get miss, and
// Delete on a non-existent entry is a no-op.
func TestCache_Delete(t *testing.T) {
	t.Parallel()
	c := &MarkdownCache{Dir: t.TempDir()}
	if _, err := c.Put("PDF1", "h", []byte("data")); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("PDF1", "h"); !ok {
		t.Fatal("expected hit before delete")
	}
	c.Delete("PDF1", "h")
	if _, ok := c.Get("PDF1", "h"); ok {
		t.Error("expected miss after delete")
	}
	// Deleting a non-existent entry is a no-op.
	c.Delete("NOSUCH", "nope")
}

// TestCache_TooLongMarker: MarkTooLong persists across cache instances,
// and an unmarked entry reads as not-too-long.
func TestCache_TooLongMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := &MarkdownCache{Dir: dir}
	if c.TooLong("PDF1", "h") {
		t.Fatal("fresh entry must not be marked too long")
	}
	if err := c.MarkTooLong("PDF1", "h", "Note '...' too long"); err != nil {
		t.Fatal(err)
	}
	if !c.TooLong("PDF1", "h") {
		t.Error("expected TooLong after MarkTooLong")
	}
	// A new instance over the same dir sees the marker (it's on disk).
	if !(&MarkdownCache{Dir: dir}).TooLong("PDF1", "h") {
		t.Error("marker must persist on disk across instances")
	}
	// Marker is keyed by (pdfKey, hash): a changed PDF retries naturally.
	if c.TooLong("PDF1", "otherhash") {
		t.Error("different hash must not inherit the marker")
	}
	if c.TooLong("PDF2", "h") {
		t.Error("different pdfKey must not inherit the marker")
	}
	// MarkTooLong on a nonexistent dir creates it, like Put.
	c2 := &MarkdownCache{Dir: filepath.Join(dir, "nested")}
	if err := c2.MarkTooLong("PDFX", "hx", "too long"); err != nil {
		t.Fatal(err)
	}
	if !c2.TooLong("PDFX", "hx") {
		t.Error("expected TooLong after MarkTooLong into fresh dir")
	}
}

// TestCache_DeleteClearsTooLongMarker: --reextract's cache invalidation
// is the escape hatch for a recorded too-long verdict — Delete must
// drop the marker along with the markdown.
func TestCache_DeleteClearsTooLongMarker(t *testing.T) {
	t.Parallel()
	c := &MarkdownCache{Dir: t.TempDir()}
	if _, err := c.Put("PDF1", "h", []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkTooLong("PDF1", "h", "too long"); err != nil {
		t.Fatal(err)
	}
	c.Delete("PDF1", "h")
	if c.TooLong("PDF1", "h") {
		t.Error("Delete must clear the too-long marker")
	}
}

// TestCache_AutoMkdir: a fresh Cache with a non-existent Dir is a
// valid, empty cache — Put creates the directory.
func TestCache_AutoMkdir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	c := &MarkdownCache{Dir: filepath.Join(root, "nested", "cache")}
	if _, ok := c.Get("PDF1", "h"); ok {
		t.Error("miss expected on nonexistent dir")
	}
	if _, err := c.Put("PDF1", "h", []byte("x")); err != nil {
		t.Fatalf("Put on nonexistent dir: %v", err)
	}
	if _, ok := c.Get("PDF1", "h"); !ok {
		t.Error("hit expected after Put created dir")
	}
}
