package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/version"
)

// TestDefaultCacheFile_EmptyXDGCacheHome guards against an empty
// XDG_CACHE_HOME (set to "" in the shell, not just unset) — without the
// defensive fallback, xdg.CacheHome resolves to ~/Library/Caches on
// darwin instead of ~/.cache.
func TestDefaultCacheFile_EmptyXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".cache", "sci", "update-check.json")
	if got := defaultCacheFile(); got != want {
		t.Errorf("defaultCacheFile with empty XDG_CACHE_HOME = %q, want %q", got, want)
	}
}

func TestReadCacheEmpty(t *testing.T) {
	// Missing file → no message.
	old := cacheFile
	cacheFile = filepath.Join(t.TempDir(), "nonexistent.json")
	defer func() { cacheFile = old }()

	if msg := readCache(); msg != "" {
		t.Errorf("readCache() = %q, want empty for missing file", msg)
	}
}

func TestReadCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	if msg := readCache(); msg != "" {
		t.Errorf("readCache() = %q, want empty for corrupt cache", msg)
	}
}

func TestReadCacheUpToDate(t *testing.T) {
	// Cached latest matches current binary → no message.
	const sha = "abc1234def5678"

	oldCommit := version.Commit
	version.Commit = sha
	defer func() { version.Commit = oldCommit }()

	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	data, _ := json.Marshal(CheckResult{
		Available: false,
		LatestSHA: sha,
	})
	_ = os.WriteFile(path, data, 0o644)

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	if msg := readCache(); msg != "" {
		t.Errorf("readCache() = %q, want empty when up-to-date", msg)
	}
}

func TestReadCacheUpdateAvailable(t *testing.T) {
	oldCommit := version.Commit
	version.Commit = "aaaaaaa1111111"
	defer func() { version.Commit = oldCommit }()

	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	data, _ := json.Marshal(CheckResult{
		Available: true,
		LatestSHA: "bbbbbbb2222222",
	})
	_ = os.WriteFile(path, data, 0o644)

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	msg := readCache()
	if msg == "" {
		t.Fatal("readCache() = empty, want update message")
	}
	if want := "Update available: bbbbbbb → run: sci update"; msg != want {
		t.Errorf("readCache() = %q, want %q", msg, want)
	}
}

func TestReadCacheStaleAfterUpdate(t *testing.T) {
	// Cache says "bbbbbbb is latest" but the user already updated to bbbbbbb.
	oldCommit := version.Commit
	version.Commit = "bbbbbbb2222222"
	defer func() { version.Commit = oldCommit }()

	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	data, _ := json.Marshal(CheckResult{
		Available: true,
		LatestSHA: "bbbbbbb2222222",
	})
	_ = os.WriteFile(path, data, 0o644)

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	if msg := readCache(); msg != "" {
		t.Errorf("readCache() = %q, want empty after user updated to cached SHA", msg)
	}
}

func TestWriteCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "update-check.json")

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	result := CheckResult{
		Available:  true,
		CurrentSHA: "aaa",
		LatestSHA:  "bbb",
	}
	writeCache(result)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	var got CheckResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("cache file invalid JSON: %v", err)
	}
	if got.LatestSHA != "bbb" {
		t.Errorf("LatestSHA = %q, want %q", got.LatestSHA, "bbb")
	}
}

func TestConcurrentCacheReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	oldCommit := version.Commit
	version.Commit = "aaaaaaa1111111"
	defer func() { version.Commit = oldCommit }()

	// Seed the cache with a valid entry.
	writeCache(CheckResult{Available: true, LatestSHA: "bbbbbbb2222222"})

	var wg sync.WaitGroup

	// Hammer the cache with many concurrent readers and writers.
	for i := range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = readCache()
		}()
		go func() {
			defer wg.Done()
			writeCache(CheckResult{
				Available: true,
				LatestSHA: fmt.Sprintf("ccccccc%07d", i),
			})
		}()
	}

	wg.Wait()

	// Cache file must still be valid JSON — no torn writes.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache file missing after concurrent ops: %v", err)
	}

	var result CheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("cache file corrupted after concurrent access: %v\nraw: %s", err, data)
	}
}

func TestWriteCache_ReadOnlyParent(t *testing.T) {
	dir := t.TempDir()
	roDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(roDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(roDir, 0o755) }()

	path := filepath.Join(roDir, "sub", "update-check.json")

	old := cacheFile
	cacheFile = path
	defer func() { cacheFile = old }()

	// writeCache should silently fail, not panic.
	writeCache(CheckResult{Available: true, LatestSHA: "aaa"})

	if _, err := os.Stat(path); err == nil {
		t.Error("cache file should not have been written to read-only directory")
	}
}

func TestReadCachedNotice_DevBuild(t *testing.T) {
	oldCommit := version.Commit
	version.Commit = "unknown"
	defer func() { version.Commit = oldCommit }()

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("ReadCachedNotice() = %q, want empty for dev build", msg)
	}
}

func TestReadCachedNotice_OptOut(t *testing.T) {
	oldCommit := version.Commit
	version.Commit = "abc1234"
	defer func() { version.Commit = oldCommit }()

	t.Setenv("SCI_NO_UPDATE_CHECK", "1")

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("ReadCachedNotice() = %q, want empty when opted out", msg)
	}
}
