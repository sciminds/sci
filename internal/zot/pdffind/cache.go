package pdffind

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cache is an on-disk lookup cache keyed by the OpenAlex query string (a
// DOI or a normalized title). One file per query — JSON-serialized Finding.
//
// Same pattern as extract.MarkdownCache: plain directory, no index, no lock.
// Atomic writes via tmp-rename so a crash mid-write can never produce a
// half-readable cached entry.
//
// Layout: <Dir>/<sha256(normalized-key)>.json
//
// Values include lookup errors — a cached "no title match" is still a cache
// hit, so reruns don't re-flood OpenAlex with queries that we already know
// return nothing. Users who want to retry failures pass --refresh.
type Cache struct {
	Dir string
}

// cachedFinding is the on-disk payload. Version bump lets us invalidate the
// whole cache later if Finding's shape changes.
type cachedFinding struct {
	Version int     `json:"v"`
	Finding Finding `json:"finding"`
}

const cacheVersion = 1

// Get returns the cached Finding for the given query, or ok=false if absent
// or unreadable. A corrupt entry is treated as a miss rather than an error —
// caller will do a fresh lookup and overwrite.
func (c *Cache) Get(query string) (Finding, bool) {
	if c == nil || c.Dir == "" {
		return Finding{}, false
	}
	data, err := os.ReadFile(c.pathFor(query))
	if err != nil {
		return Finding{}, false
	}
	var payload cachedFinding
	if err := json.Unmarshal(data, &payload); err != nil {
		return Finding{}, false
	}
	if payload.Version != cacheVersion {
		return Finding{}, false
	}
	return payload.Finding, true
}

// Put writes the Finding to the cache under the given query.
// Errors are silently swallowed — a cache write failure should never fail
// the user-visible scan.
func (c *Cache) Put(query string, f Finding) {
	if c == nil || c.Dir == "" {
		return
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return
	}
	final := c.pathFor(query)
	tmp, err := os.CreateTemp(c.Dir, ".pdffind-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	data, err := json.Marshal(cachedFinding{Version: cacheVersion, Finding: f})
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, final)
}

func (c *Cache) pathFor(query string) string {
	sum := sha256.Sum256([]byte(normalizeCacheKey(query)))
	return filepath.Join(c.Dir, hex.EncodeToString(sum[:])+".json")
}

// normalizeCacheKey lowercases + collapses whitespace so "10.1/X" and
// "10.1/x " hash to the same file. Safe for DOIs (case-insensitive per
// RFC 3986 for the registrant portion) and titles (we search OpenAlex
// case-insensitively either way).
func normalizeCacheKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// DefaultCacheDir returns <user cache>/sci/zot/pdffind.
func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "sci", "zot", "pdffind"), nil
}
