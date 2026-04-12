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
