package markdb

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiffResult shows what would change if an ingest were run.
type DiffResult struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

// Diff compares a directory against the database and reports what would change.
// Uses content hashing, not mtime.
func (s *Store) Diff(root string) (*DiffResult, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	// Get source ID.
	var sourceID int64
	err = s.db.QueryRow("SELECT id FROM _sources WHERE root = ?", root).Scan(&sourceID)
	if err != nil {
		return nil, fmt.Errorf("source not found for %q (ingest first): %w", root, err)
	}

	// Get existing file hashes from DB.
	dbHashes, err := s.getExistingHashes(sourceID)
	if err != nil {
		return nil, err
	}

	// Walk directory and compute hashes.
	diskHashes := make(map[string]string)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		h := sha256.Sum256(content)
		diskHashes[rel] = fmt.Sprintf("%x", h)
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := &DiffResult{}

	// Find added and modified.
	for path, diskHash := range diskHashes {
		dbHash, exists := dbHashes[path]
		if !exists {
			result.Added = append(result.Added, path)
		} else if dbHash != diskHash {
			result.Modified = append(result.Modified, path)
		}
	}

	// Find deleted.
	for path := range dbHashes {
		if _, exists := diskHashes[path]; !exists {
			result.Deleted = append(result.Deleted, path)
		}
	}

	sort.Strings(result.Added)
	sort.Strings(result.Modified)
	sort.Strings(result.Deleted)

	return result, nil
}
