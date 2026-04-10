package selfupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/version"
)

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
