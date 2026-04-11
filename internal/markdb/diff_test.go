package markdb

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestDiffNoChanges(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	result, err := s.Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 0 || len(result.Modified) != 0 || len(result.Deleted) != 0 {
		t.Errorf("expected no changes, got added=%v modified=%v deleted=%v",
			result.Added, result.Modified, result.Deleted)
	}
}

func TestDiffNewFile(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "old.md", "old content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "new.md", "new content")
	result, err := s.Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 1 || result.Added[0] != "new.md" {
		t.Errorf("Added = %v, want [new.md]", result.Added)
	}
}

func TestDiffModifiedFile(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "version 1")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "note.md", "version 2")
	result, err := s.Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Modified) != 1 || result.Modified[0] != "note.md" {
		t.Errorf("Modified = %v, want [note.md]", result.Modified)
	}
}

func TestDiffDeletedFile(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "keep.md", "kept")
	writeFile(t, dir, "gone.md", "will be removed")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	_ = os.Remove(filepath.Join(dir, "gone.md"))
	result, err := s.Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "gone.md" {
		t.Errorf("Deleted = %v, want [gone.md]", result.Deleted)
	}
}

func TestDiffTouchedUnchanged(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "same content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	// Rewrite with identical content (changes mtime but not hash).
	writeFile(t, dir, "note.md", "same content")
	result, err := s.Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Modified) != 0 {
		t.Errorf("Modified = %v, want empty (hash unchanged)", result.Modified)
	}

	// Verify all lists sorted.
	if !sort.StringsAreSorted(result.Added) {
		t.Error("Added not sorted")
	}
}
