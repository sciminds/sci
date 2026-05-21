package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/version"
)

// cacheFile is the path to the cached update-check result. It is a var so
// tests can redirect it to a temp directory.
var cacheFile = ""

// InternalRefreshEnv is the sentinel env var that flips the parent binary
// into "refresh the cache and exit" mode. cmd/sci/main checks for it
// before any urfave/cli wiring runs; SpawnDetachedRefresh sets it on the
// child so the child can detect (and short-circuit out of) recursion.
const InternalRefreshEnv = "SCI_INTERNAL_REFRESH_UPDATE_CACHE"

// refreshTTL is the minimum time between live update checks. Rapid-fire
// invocations within this window read from the cache without re-hitting
// GitHub — preserves the API rate-limit budget and avoids spawning
// redundant refresh subprocesses. 1h is calibrated to sci's release
// cadence (multiple releases per day during bursts); a longer window
// would leave lab members blind to mid-day releases. Even an LLM
// chaining hundreds of commands triggers at most one refresh per hour,
// well inside GitHub's 60/h unauth rate limit.
const refreshTTL = time.Hour

// now is the package's clock seam — tests override it to exercise
// time-dependent paths without sleeping.
var now = time.Now

// defaultCacheFile returns the canonical update-check cache path under
// $XDG_CACHE_HOME (e.g. ~/.cache/sci/update-check.json on Linux,
// ~/Library/Caches/sci/update-check.json on macOS).
//
// An empty XDG_CACHE_HOME (set to "" in the shell, not just unset) is
// treated as "use $HOME/.cache" rather than trusting xdg.CacheHome's
// fallback — on darwin that fallback is ~/Library/Caches, which is not
// where users with a blanked env var expect the cache to live.
func defaultCacheFile() string {
	if os.Getenv("XDG_CACHE_HOME") == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cache", "sci", "update-check.json")
	}
	return filepath.Join(xdg.CacheHome, "sci", "update-check.json")
}

// ReadCachedNotice returns the user-facing "Update available" message
// for the current binary, or "" when there is nothing to show. The
// notice is suppressed in three cases:
//   - the cache reports no update available (or the user already updated);
//   - we've already shown the notice for the current refresh cycle (i.e.
//     LastShownAt is after LastCheckedAt);
//   - dev build or SCI_NO_UPDATE_CHECK opt-out.
//
// Callers MUST invoke [MarkNoticeShown] after actually displaying the
// returned message so subsequent invocations stay quiet until the next
// refresh cycle.
func ReadCachedNotice() string {
	if version.Commit == "unknown" {
		return ""
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return ""
	}
	cached, ok := loadCache()
	if !ok {
		return ""
	}
	// Cache was written by some previous binary. If the user has since
	// updated, the cached LatestSHA may now match our commit — re-evaluate
	// with the current binary's commit.
	if !commitsDiffer(version.Commit, cached.LatestSHA) {
		return ""
	}
	// Show once per refresh cycle: if LastShownAt is after LastCheckedAt,
	// we've already announced this cycle's findings.
	if cached.LastShownAt.After(cached.LastCheckedAt) {
		return ""
	}
	return fmt.Sprintf("Update available: %s → run: sci update", ShortSHA(cached.LatestSHA))
}

// MarkNoticeShown stamps the cache so subsequent ReadCachedNotice calls
// return "" until the next refresh observes new information. Failures
// are swallowed — display tracking is best-effort.
func MarkNoticeShown() {
	cached, ok := loadCache()
	if !ok {
		return
	}
	cached.LastShownAt = now()
	writeCache(cached)
}

// RefreshCache performs a live update check and writes the result to disk.
// Stamps LastCheckedAt on success so [SpawnDetachedRefresh] can dedupe
// burst invocations and [ReadCachedNotice] can detect a fresh cycle.
// LastShownAt is preserved across refreshes — the show-once-per-cycle
// gate in ReadCachedNotice handles cadence.
func RefreshCache() {
	if version.Commit == "unknown" {
		return
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return
	}
	result := Check()
	if result.Error != "" {
		// Don't overwrite a good cache with an error result — the previous
		// state is still our best information.
		return
	}
	result.LastCheckedAt = now()
	if prev, ok := loadCache(); ok {
		result.LastShownAt = prev.LastShownAt
	}
	writeCache(result)
}

// InvalidateCache marks the cache as stale by zeroing LastCheckedAt,
// forcing the next [SpawnDetachedRefresh] to re-hit GitHub. Intended
// to be called after a successful `sci update`: a new release can land
// minutes after the user's update, and trusting the stamp would hide
// it for up to [refreshTTL]. Missing cache is a no-op.
//
// LastShownAt is preserved — once the next refresh stamps a newer
// LastCheckedAt, the show-once-per-cycle gate naturally re-opens.
func InvalidateCache() {
	cached, ok := loadCache()
	if !ok {
		return
	}
	cached.LastCheckedAt = time.Time{}
	writeCache(cached)
}

// cacheIsFresh reports whether the cache was refreshed within [refreshTTL].
// Used by SpawnDetachedRefresh to dedupe rapid-fire invocations without
// wasting GitHub API requests.
func cacheIsFresh() bool {
	cached, ok := loadCache()
	if !ok {
		return false
	}
	return !cached.LastCheckedAt.IsZero() && now().Sub(cached.LastCheckedAt) < refreshTTL
}

// loadCache reads the persisted CheckResult. The boolean is false on any
// error (missing file, corrupt JSON, empty path).
func loadCache() (CheckResult, bool) {
	path := cachePath()
	if path == "" {
		return CheckResult{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{}, false
	}
	var cached CheckResult
	if err := json.Unmarshal(data, &cached); err != nil {
		return CheckResult{}, false
	}
	return cached, true
}

// writeCache persists a CheckResult to disk for the next invocation.
// It uses write-to-temp + rename for atomicity so concurrent CLI
// invocations never produce a torn (partial/corrupt) cache file.
func writeCache(result CheckResult) {
	path := cachePath()
	if path == "" {
		return
	}

	data, err := json.Marshal(result)
	if err != nil {
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	tmp, err := os.CreateTemp(dir, ".update-check-*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return
	}
	_ = tmp.Close()

	// Atomic rename — readers see either the old file or the new one, never
	// a partial write.
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
	}
}

func cachePath() string {
	if cacheFile != "" {
		return cacheFile
	}
	return defaultCacheFile()
}
