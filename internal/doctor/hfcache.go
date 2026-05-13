package doctor

// hfcache.go — small on-disk cache for the last successful `hf auth whoami`
// result. Used by [checkIdentity] so that a transient network blip on a
// machine that has already verified its HF identity doesn't surface as a
// scary "could not verify" warning.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// hfCacheEntry mirrors the shape of `hf auth whoami` output we care about.
type hfCacheEntry struct {
	User string   `json:"user"`
	Orgs []string `json:"orgs"`
}

// hfCacheFile is overridable in tests; empty string means "use the default
// XDG cache path".
var hfCacheFile = ""

// hfCachePath returns the on-disk cache location, honoring XDG_CACHE_HOME
// with the same blanked-var defensive fallback used elsewhere in sci.
func hfCachePath() string {
	if hfCacheFile != "" {
		return hfCacheFile
	}
	if os.Getenv("XDG_CACHE_HOME") == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cache", "sci", "hf-whoami.json")
	}
	return filepath.Join(xdg.CacheHome, "sci", "hf-whoami.json")
}

// readHFCache returns the cached user + org list, or ok=false if no usable
// entry exists.
func readHFCache() (user string, orgs []string, ok bool) {
	data, err := os.ReadFile(hfCachePath())
	if err != nil {
		return "", nil, false
	}
	var entry hfCacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return "", nil, false
	}
	if entry.User == "" {
		return "", nil, false
	}
	return entry.User, entry.Orgs, true
}

// writeHFCache persists the latest whoami result. Best-effort — failures
// here must never break the doctor check.
func writeHFCache(user string, orgs []string) {
	path := hfCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(hfCacheEntry{User: user, Orgs: orgs})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
