package extract

import (
	"fmt"
	"os"
)

// HashPDF returns a fast fingerprint of the file at path based on its
// size and modification time. This is used as a cache key and note
// metadata tag — it only needs to change when the PDF changes, not be
// cryptographically secure. A stat() call is orders of magnitude
// faster than reading every byte for SHA-256, which matters when
// planning extraction across thousands of PDFs.
//
// Format: "<size>-<unixMtime>" (e.g. "1048576-1718200000").
func HashPDF(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	return fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().Unix()), nil
}
