package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// contentHashBytes bounds how much of each PDF we read into the SHA-256
// digest. 1 MB is enough to differentiate any two distinct PDFs in practice
// (the file header, xref offsets, and metadata stream all sit well within
// the first MB), and reading the bytes adds at most a few ms per file — a
// cost we eat at planning time, not at every extraction.
const contentHashBytes = 1 << 20 // 1 MiB

// HashPDF returns a fingerprint of the file at path that combines fast stat
// metadata with a hash of the file's leading bytes. The full file isn't
// hashed because planning across thousands of PDFs needs to stay cheap; the
// 1 MB prefix is enough to distinguish distinct content while still being
// fast.
//
// Why include the content hash at all when size+mtime is so cheap: tools
// that preserve mtime (`cp -p`, `rsync -t`, `touch -r`, Zotero attachment
// restore from cloud sync) would otherwise produce the same fingerprint
// for changed content, leading to silent stale-cache hits.
//
// Format: "<size>-<unixMtime>-<sha16>" (e.g. "1048576-1718200000-a1b2c3d4e5f6a7b8").
// The suffix is the first 16 hex chars of SHA-256(first 1 MB). Changing
// this format invalidates every existing extract cache entry — acceptable
// because the prior format had a correctness gap.
func HashPDF(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, contentHashBytes)); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	digest := hex.EncodeToString(h.Sum(nil))[:16]
	return fmt.Sprintf("%d-%d-%s", fi.Size(), fi.ModTime().Unix(), digest), nil
}
