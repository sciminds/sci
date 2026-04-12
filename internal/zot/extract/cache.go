package extract

import (
	"fmt"
	"os"
	"path/filepath"
)

// MarkdownCache stores docling-produced markdown keyed by
// (pdfKey, pdfHash) so a bulk run can resume after a mid-batch failure
// without re-running docling on papers whose extraction already
// succeeded but whose Zotero note post failed.
//
// The cache is a plain directory of files — no index, no lock. Every
// entry is an immutable snapshot identified by its hash, so a changed
// PDF produces a new file rather than overwriting the old one.
// Collisions across pdfKeys are impossible because the filename
// encodes both key and hash.
//
// Layout: <Dir>/<pdfKey>-<hash>.md
//
// Garbage collection: deferred. Every entry is ~100KB and users with
// 10k-paper libraries will accumulate ~1GB in the worst case — fine
// for now. A future `zot extract-lib --prune` can walk the dir and
// drop anything whose (pdfKey, hash) no longer matches the live
// library.
type MarkdownCache struct {
	// Dir is the cache root. It is created on first Put.
	Dir string
}

// Get returns the on-disk path of the cached markdown for
// (pdfKey, hash), and ok=false if no entry exists.
func (c *MarkdownCache) Get(pdfKey, hash string) (string, bool) {
	path := c.pathFor(pdfKey, hash)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// Put writes md to the cache under (pdfKey, hash) and returns the
// final on-disk path. Write is atomic: content is staged to a sibling
// tmp file and renamed into place, so a crash mid-write can never
// leave a half-readable entry under the canonical name.
func (c *MarkdownCache) Put(pdfKey, hash string, md []byte) (string, error) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", fmt.Errorf("cache mkdir: %w", err)
	}
	final := c.pathFor(pdfKey, hash)
	tmp, err := os.CreateTemp(c.Dir, "."+pdfKey+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cache tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(md); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("cache write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("cache close: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("cache rename: %w", err)
	}
	return final, nil
}

// Delete removes the cached entry for (pdfKey, hash), if it exists.
// Used by --reextract to force docling to re-run. A no-op if the
// entry doesn't exist.
func (c *MarkdownCache) Delete(pdfKey, hash string) {
	_ = os.Remove(c.pathFor(pdfKey, hash))
}

func (c *MarkdownCache) pathFor(pdfKey, hash string) string {
	return filepath.Join(c.Dir, pdfKey+"-"+hash+".md")
}

// DefaultCacheDir returns the standard on-disk location for the
// extract markdown cache: <user cache>/sci/zot/extract.
func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "sci", "zot", "extract"), nil
}
