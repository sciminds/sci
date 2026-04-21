package pdffind

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCache_PutThenGet(t *testing.T) {
	t.Parallel()
	c := &Cache{Dir: t.TempDir()}
	f := Finding{ItemKey: "ABC", OpenAlexID: "W42", PDFURL: "https://x.pdf"}
	c.Put("10.1/x", f)

	got, ok := c.Get("10.1/x")
	if !ok {
		t.Fatal("want cache hit")
	}
	if got.OpenAlexID != "W42" || got.PDFURL != "https://x.pdf" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestCache_NormalizesKey(t *testing.T) {
	t.Parallel()
	// Same query with cosmetic whitespace/case variation must hit the same slot.
	c := &Cache{Dir: t.TempDir()}
	c.Put("10.1/X", Finding{ItemKey: "ABC"})
	got, ok := c.Get("  10.1/x ")
	if !ok || got.ItemKey != "ABC" {
		t.Errorf("normalized key miss: %+v ok=%v", got, ok)
	}
}

func TestCache_MissOnUnknownKey(t *testing.T) {
	t.Parallel()
	c := &Cache{Dir: t.TempDir()}
	_, ok := c.Get("nothing-cached")
	if ok {
		t.Error("want cache miss on empty dir")
	}
}

func TestCache_NilReceiverIsMiss(t *testing.T) {
	t.Parallel()
	var c *Cache
	_, ok := c.Get("x")
	if ok {
		t.Error("nil cache must be a miss, never a panic")
	}
	c.Put("x", Finding{}) // must not panic either
}

func TestCache_CorruptEntryIsTreatedAsMiss(t *testing.T) {
	t.Parallel()
	c := &Cache{Dir: t.TempDir()}
	// Manually write garbage at the expected path.
	path := c.pathFor("10.1/x")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := c.Get("10.1/x")
	if ok {
		t.Error("corrupt entry must be a miss so we re-fetch, not an error")
	}
}

func TestCache_VersionBumpInvalidates(t *testing.T) {
	t.Parallel()
	c := &Cache{Dir: t.TempDir()}
	// Write a payload with a stale version directly.
	path := c.pathFor("10.1/x")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	stale, _ := json.Marshal(map[string]any{"v": 0, "finding": Finding{ItemKey: "OLD"}})
	if err := os.WriteFile(path, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := c.Get("10.1/x")
	if ok {
		t.Error("stale version must miss so the cache self-heals after a schema change")
	}
}
