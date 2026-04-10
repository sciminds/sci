package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sciminds/cli/internal/version"
)

// cacheFile is the path to the cached update-check result. It is a var so
// tests can redirect it to a temp directory.
var cacheFile = ""

func defaultCacheFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sci", "update-check.json")
}

// ReadCachedNotice reads the previous cached check result (instant) and
// returns a user-facing message if an update is available, or "" if not.
// It never touches the network.
func ReadCachedNotice() string {
	if version.Commit == "unknown" {
		return ""
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return ""
	}
	return readCache()
}

// RefreshCache performs a live update check and writes the result to disk so
// the *next* invocation of [ReadCachedNotice] sees up-to-date information.
// This is slow (network call) — callers should run it in a goroutine.
func RefreshCache() {
	if version.Commit == "unknown" {
		return
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return
	}
	writeCache(Check())
}

// readCache loads the last persisted CheckResult and returns a user-facing
// message if an update is available for the *current* binary. Returns "" on
// any error or if no update is available.
func readCache() string {
	path := cachePath()
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var cached CheckResult
	if err := json.Unmarshal(data, &cached); err != nil {
		return ""
	}

	// The cached result was computed against whatever binary wrote it. If the
	// user has since updated, the cached LatestSHA may now match our commit —
	// re-evaluate with the current binary's commit.
	if !commitsDiffer(version.Commit, cached.LatestSHA) {
		return ""
	}

	return fmt.Sprintf("Update available: %s → run: sci update", ShortSHA(cached.LatestSHA))
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
