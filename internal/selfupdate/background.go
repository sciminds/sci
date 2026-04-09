package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sciminds/cli/internal/version"
)

const checkInterval = 24 * time.Hour

// cachedCheck is persisted to disk between runs.
type cachedCheck struct {
	CheckedAt time.Time `json:"checked_at"`
	Available bool      `json:"available"`
	LatestSHA string    `json:"latest_sha,omitempty"`
	ForCommit string    `json:"for_commit"` // the build commit this check was for
}

// cacheDir returns ~/.config/sci, creating it if needed.
func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "sci")
	return dir, os.MkdirAll(dir, 0o700)
}

func cachePath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update_check.json"), nil
}

func loadCache() (cachedCheck, bool) {
	path, err := cachePath()
	if err != nil {
		return cachedCheck{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cachedCheck{}, false
	}
	var c cachedCheck
	if json.Unmarshal(data, &c) != nil {
		return cachedCheck{}, false
	}
	return c, true
}

func saveCache(c cachedCheck) {
	path, err := cachePath()
	if err != nil {
		return
	}
	data, _ := json.Marshal(c)
	_ = os.WriteFile(path, data, 0o600)
}

// CheckBackground runs a cached update check. It returns a user-facing
// message if an update is available, or "" if not (or on error/skip).
// Safe to call from any goroutine — network I/O only happens once per
// [checkInterval].
func CheckBackground() string {
	if version.Commit == "unknown" {
		return ""
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return ""
	}

	cached, ok := loadCache()

	// If we have a fresh cache for this same build, use it.
	if ok && cached.ForCommit == version.Commit && time.Since(cached.CheckedAt) < checkInterval {
		if cached.Available {
			return formatNotice(cached.LatestSHA)
		}
		return ""
	}

	// Stale or missing — do a live check.
	result := Check()
	saveCache(cachedCheck{
		CheckedAt: time.Now(),
		Available: result.Available,
		LatestSHA: result.LatestSHA,
		ForCommit: version.Commit,
	})

	if result.Available {
		return formatNotice(result.LatestSHA)
	}
	return ""
}

func formatNotice(sha string) string {
	return fmt.Sprintf("Update available: %s → run: sci update", ShortSHA(sha))
}
