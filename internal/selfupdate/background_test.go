package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// withCache redirects the package-level cacheFile to a fresh temp path
// for the duration of the test and restores it on cleanup.
func withCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	old := cacheFile
	cacheFile = path
	t.Cleanup(func() { cacheFile = old })
	return path
}

// withCommit pins version.Commit for the test and restores on cleanup.
func withCommit(t *testing.T, sha string) {
	t.Helper()
	old := version.Commit
	version.Commit = sha
	t.Cleanup(func() { version.Commit = old })
}

// withClock pins the package clock to a fixed time so time-dependent
// branches are testable without sleeping.
func withClock(t *testing.T, instant time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return instant }
	t.Cleanup(func() { now = old })
}

func TestReadCachedNotice_MissingCache(t *testing.T) {
	withCache(t)
	withCommit(t, "aaaaaaa1111111")

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty for missing cache", msg)
	}
}

func TestReadCachedNotice_CorruptCache(t *testing.T) {
	path := withCache(t)
	withCommit(t, "aaaaaaa1111111")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty for corrupt cache", msg)
	}
}

func TestReadCachedNotice_UpToDate(t *testing.T) {
	const sha = "abc1234def5678"
	path := withCache(t)
	withCommit(t, sha)

	data, _ := json.Marshal(CheckResult{
		Available:     false,
		LatestSHA:     sha,
		LastCheckedAt: time.Now(),
	})
	_ = os.WriteFile(path, data, 0o644)

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty when up-to-date", msg)
	}
}

func TestReadCachedNotice_UpdateAvailable(t *testing.T) {
	path := withCache(t)
	withCommit(t, "aaaaaaa1111111")

	data, _ := json.Marshal(CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: time.Now(),
	})
	_ = os.WriteFile(path, data, 0o644)

	if want, got := "Update available: bbbbbbb → run: sci update", ReadCachedNotice(); got != want {
		t.Errorf("msg = %q, want %q", got, want)
	}
}

func TestReadCachedNotice_StaleAfterUpdate(t *testing.T) {
	// Cache says "bbbbbbb is latest" but the user already updated to bbbbbbb.
	path := withCache(t)
	withCommit(t, "bbbbbbb2222222")

	data, _ := json.Marshal(CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: time.Now(),
	})
	_ = os.WriteFile(path, data, 0o644)

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty after user updated to cached SHA", msg)
	}
}

// TestReadCachedNotice_AlreadyShownThisCycle verifies the
// show-once-per-cycle gate: when LastShownAt is after LastCheckedAt,
// the notice was already displayed for this refresh cycle.
func TestReadCachedNotice_AlreadyShownThisCycle(t *testing.T) {
	path := withCache(t)
	withCommit(t, "aaaaaaa1111111")

	checkedAt := time.Now().Add(-2 * time.Hour)
	shownAt := checkedAt.Add(time.Minute) // shown one minute after the check
	data, _ := json.Marshal(CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: checkedAt,
		LastShownAt:   shownAt,
	})
	_ = os.WriteFile(path, data, 0o644)

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty when already shown this cycle", msg)
	}
}

// TestReadCachedNotice_NewCycleAfterRefresh verifies the inverse: once a
// fresh refresh stamps LastCheckedAt past the prior LastShownAt, the
// notice re-appears (one display per cycle).
func TestReadCachedNotice_NewCycleAfterRefresh(t *testing.T) {
	path := withCache(t)
	withCommit(t, "aaaaaaa1111111")

	shownAt := time.Now().Add(-25 * time.Hour) // shown a day ago
	checkedAt := time.Now().Add(-time.Minute)  // refreshed just now
	data, _ := json.Marshal(CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: checkedAt,
		LastShownAt:   shownAt,
	})
	_ = os.WriteFile(path, data, 0o644)

	if msg := ReadCachedNotice(); msg == "" {
		t.Error("msg = empty, want notice for new cycle")
	}
}

// TestMarkNoticeShown_WritesTimestamp verifies that MarkNoticeShown
// stamps LastShownAt past LastCheckedAt so the next read suppresses.
func TestMarkNoticeShown_WritesTimestamp(t *testing.T) {
	path := withCache(t)
	withCommit(t, "aaaaaaa1111111")
	pinned := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	withClock(t, pinned)

	checkedAt := pinned.Add(-time.Hour)
	data, _ := json.Marshal(CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: checkedAt,
	})
	_ = os.WriteFile(path, data, 0o644)

	if msg := ReadCachedNotice(); msg == "" {
		t.Fatal("expected non-empty notice as test setup")
	}
	MarkNoticeShown()

	got, ok := loadCache()
	if !ok {
		t.Fatal("loadCache failed after MarkNoticeShown")
	}
	if !got.LastShownAt.Equal(pinned) {
		t.Errorf("LastShownAt = %v, want %v", got.LastShownAt, pinned)
	}

	// Next read must now suppress.
	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg after mark = %q, want empty", msg)
	}
}

// TestRefreshCache_PreservesLastShownAt verifies that a refresh does not
// clobber the LastShownAt bookkeeping field. The show-once-per-cycle
// gate uses LastShownAt vs LastCheckedAt for cadence; resetting on
// refresh would over-trigger.
func TestRefreshCache_PreservesLastShownAt(t *testing.T) {
	path := withCache(t)
	shown := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	data, _ := json.Marshal(CheckResult{
		Available:   true,
		LatestSHA:   "bbbbbbb",
		LastShownAt: shown,
	})
	_ = os.WriteFile(path, data, 0o644)

	// Simulate the merge step inside RefreshCache: fresh Check() result
	// + preserve LastShownAt from existing cache + stamp LastCheckedAt.
	fresh := CheckResult{Available: true, LatestSHA: "bbbbbbb"}
	fresh.LastCheckedAt = time.Now()
	if prev, ok := loadCache(); ok {
		fresh.LastShownAt = prev.LastShownAt
	}
	writeCache(fresh)

	got, ok := loadCache()
	if !ok {
		t.Fatal("loadCache failed after writeCache")
	}
	if !got.LastShownAt.Equal(shown) {
		t.Errorf("LastShownAt = %v, want preserved %v", got.LastShownAt, shown)
	}
	if got.LastCheckedAt.IsZero() {
		t.Error("LastCheckedAt = zero, want stamped")
	}
}

// TestCacheIsFresh verifies the staleness gate that
// SpawnDetachedRefresh uses to dedupe rapid-fire invocations.
func TestCacheIsFresh(t *testing.T) {
	withCache(t)

	// No cache → not fresh (caller must refresh).
	if cacheIsFresh() {
		t.Error("cacheIsFresh = true for missing cache; want false")
	}

	// Just-stamped cache → fresh.
	writeCache(CheckResult{LatestSHA: "bbb", LastCheckedAt: time.Now()})
	if !cacheIsFresh() {
		t.Error("cacheIsFresh = false for just-refreshed cache")
	}

	// Past the TTL → stale.
	writeCache(CheckResult{LatestSHA: "bbb", LastCheckedAt: time.Now().Add(-2 * refreshTTL)})
	if cacheIsFresh() {
		t.Error("cacheIsFresh = true for stale cache")
	}

	// Zero timestamp (legacy cache from before LastCheckedAt existed) → stale.
	writeCache(CheckResult{LatestSHA: "bbb"})
	if cacheIsFresh() {
		t.Error("cacheIsFresh = true for zero LastCheckedAt; legacy caches must refresh")
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
	withCache(t)
	withCommit(t, "aaaaaaa1111111")

	// Seed with a valid entry.
	writeCache(CheckResult{Available: true, LatestSHA: "bbbbbbb2222222"})

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = ReadCachedNotice()
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

	data, err := os.ReadFile(cacheFile)
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

// TestInvalidateCache_ClearsLastCheckedAt verifies the post-update hook:
// after sci updates itself, the freshly installed binary must NOT trust
// the cache's "we checked recently" stamp — a second release of the day
// could have landed minutes after the user's update, and a 1h TTL window
// would still hide it. InvalidateCache zeros LastCheckedAt so the next
// SpawnDetachedRefresh sees the cache as stale and re-checks.
func TestInvalidateCache_ClearsLastCheckedAt(t *testing.T) {
	withCache(t)

	writeCache(CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: time.Now(), // fresh — would normally suppress refresh
	})
	if !cacheIsFresh() {
		t.Fatal("test setup: expected fresh cache before invalidation")
	}

	InvalidateCache()

	if cacheIsFresh() {
		t.Error("cacheIsFresh = true after InvalidateCache; next refresh would be skipped")
	}
}

// TestInvalidateCache_NoCacheIsNoop verifies that calling InvalidateCache
// when no cache file exists is safe — sci update on a brand-new install
// (no prior update check ever ran) must not panic or error.
func TestInvalidateCache_NoCacheIsNoop(t *testing.T) {
	withCache(t)
	InvalidateCache() // must not panic
}

func TestReadCachedNotice_DevBuild(t *testing.T) {
	withCommit(t, "unknown")

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty for dev build", msg)
	}
}

func TestReadCachedNotice_OptOut(t *testing.T) {
	withCommit(t, "abc1234")
	t.Setenv("SCI_NO_UPDATE_CHECK", "1")

	if msg := ReadCachedNotice(); msg != "" {
		t.Errorf("msg = %q, want empty when opted out", msg)
	}
}
