package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// HashLen is the number of hex chars kept from the sha256 digest when
// embedding in sentinels and note headers. 12 hex chars is 48 bits —
// enough to detect any practical drift on a single parent item while
// keeping the sentinel comment short.
const HashLen = 12

// HashPDF returns the first HashLen hex chars of the sha256 digest of
// the file at path. Used by the CLI layer to populate
// PlanRequest.PDFHash and NoteMeta.Hash before calling PlanExtract.
func HashPDF(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))[:HashLen], nil
}
