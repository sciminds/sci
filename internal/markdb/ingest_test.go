package markdb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a file in dir with the given relative path and content.
func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ingestTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InitSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dbPath
}

func TestIngestSingleFile(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "# Hello\nWorld")

	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if stats.Added != 1 {
		t.Errorf("Added = %d, want 1", stats.Added)
	}

	var path, body, hash string
	err = s.db.QueryRow("SELECT path, body, hash FROM files").Scan(&path, &body, &hash)
	if err != nil {
		t.Fatal(err)
	}
	if path != "note.md" {
		t.Errorf("path = %q, want note.md", path)
	}
	if body != "# Hello\nWorld" {
		t.Errorf("body = %q, want '# Hello\\nWorld'", body)
	}
	if hash == "" {
		t.Error("hash is empty")
	}
}

func TestIngestNestedDirs(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "sub/deep/note.md", "nested")

	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("Added = %d, want 1", stats.Added)
	}

	var path string
	_ = s.db.QueryRow("SELECT path FROM files").Scan(&path)
	if path != "sub/deep/note.md" {
		t.Errorf("path = %q, want sub/deep/note.md", path)
	}
}

func TestIngestSkipsDotDirs(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, ".git/config.md", "git config")
	writeFile(t, dir, ".obsidian/plugins.md", "plugins")
	writeFile(t, dir, "real.md", "real content")

	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("Added = %d, want 1", stats.Added)
	}
}

func TestIngestSkipsNonMd(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "readme.txt", "text file")
	writeFile(t, dir, "image.png", "fake png")
	writeFile(t, dir, "note.md", "real note")

	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("Added = %d, want 1", stats.Added)
	}
}

func TestIngestUnchangedSkipped(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", stats.Skipped)
	}
	if stats.Updated != 0 {
		t.Errorf("Updated = %d, want 0", stats.Updated)
	}
}

func TestIngestChangedFile(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "version 1")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "note.md", "version 2")
	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 1 {
		t.Errorf("Updated = %d, want 1", stats.Updated)
	}

	var body string
	_ = s.db.QueryRow("SELECT body FROM files WHERE path = 'note.md'").Scan(&body)
	if body != "version 2" {
		t.Errorf("body = %q, want 'version 2'", body)
	}
}

func TestIngestDeletedFile(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "keep.md", "kept")
	writeFile(t, dir, "remove.md", "removed")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	_ = os.Remove(filepath.Join(dir, "remove.md"))
	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("Removed = %d, want 1", stats.Removed)
	}

	var count int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&count)
	if count != 1 {
		t.Errorf("file count = %d, want 1", count)
	}
}

func TestIngestFrontmatterStored(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "---\ntitle: Hello\n---\nBody")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	var raw, body string
	_ = s.db.QueryRow("SELECT frontmatter_raw, body FROM files").Scan(&raw, &body)
	if raw != "title: Hello\n" {
		t.Errorf("frontmatter_raw = %q, want 'title: Hello\\n'", raw)
	}
	if body != "Body" {
		t.Errorf("body = %q, want 'Body'", body)
	}
}

func TestIngestDynamicColumns(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "---\ntitle: Post A\ncount: 10\n---\nBody A")
	writeFile(t, dir, "b.md", "---\ntitle: Post B\ncategory: work\n---\nBody B")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	// title column should exist and have values.
	var title string
	err := s.db.QueryRow("SELECT title FROM files WHERE path = 'a.md'").Scan(&title)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Post A" {
		t.Errorf("title = %q, want 'Post A'", title)
	}

	// count column should exist.
	var count int
	err = s.db.QueryRow("SELECT count FROM files WHERE path = 'a.md'").Scan(&count)
	if err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 10 {
		t.Errorf("count = %d, want 10", count)
	}
}

func TestIngestMalformedYAML(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "bad.md", "---\n: [invalid\n---\nBody is fine")

	stats, err := s.Ingest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("Added = %d, want 1", stats.Added)
	}
	if stats.Errors != 1 {
		t.Errorf("Errors = %d, want 1", stats.Errors)
	}

	var parseErr, body string
	_ = s.db.QueryRow("SELECT parse_error, body FROM files").Scan(&parseErr, &body)
	if parseErr == "" {
		t.Error("parse_error is empty, want non-empty")
	}
	if body != "Body is fine" {
		t.Errorf("body = %q, want 'Body is fine'", body)
	}
}

func TestIngestSourcesTable(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	var root, lastIngest string
	err := s.db.QueryRow("SELECT root, last_ingest FROM _sources").Scan(&root, &lastIngest)
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if lastIngest == "" {
		t.Error("last_ingest is empty")
	}
}

func TestIngestPlaintextFields(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "---\ntitle: Hello World\n---\n# Heading\nSome **bold** text")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	var bodyText, fmText string
	_ = s.db.QueryRow("SELECT body_text, frontmatter_text FROM files").Scan(&bodyText, &fmText)
	if bodyText == "" {
		t.Error("body_text is empty")
	}
	if fmText == "" {
		t.Error("frontmatter_text is empty")
	}
}

func TestIngestFTSPopulated(t *testing.T) {
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "---\ntitle: UniqueSearchTerm\n---\nBody with anotherUniqueWord")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	// Search body text.
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM files_fts WHERE files_fts MATCH 'anotherUniqueWord'").Scan(&count)
	if err != nil {
		t.Fatalf("FTS query: %v", err)
	}
	if count != 1 {
		t.Errorf("FTS body match count = %d, want 1", count)
	}

	// Search frontmatter text.
	err = s.db.QueryRow("SELECT COUNT(*) FROM files_fts WHERE files_fts MATCH 'UniqueSearchTerm'").Scan(&count)
	if err != nil {
		t.Fatalf("FTS frontmatter query: %v", err)
	}
	if count != 1 {
		t.Errorf("FTS frontmatter match count = %d, want 1", count)
	}
}

// BenchmarkIngest measures ingest throughput for markdown files with frontmatter.
func BenchmarkIngest(b *testing.B) {
	dir := b.TempDir()
	for i := range 100 {
		content := fmt.Sprintf("---\ntitle: Note %d\ntag: bench\n---\n# Note %d\nBody content for benchmarking.", i, i)
		full := filepath.Join(dir, fmt.Sprintf("note_%03d.md", i))
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for range b.N {
		dbPath := filepath.Join(b.TempDir(), "bench.db")
		s, err := Open(dbPath)
		if err != nil {
			b.Fatal(err)
		}
		if err := s.InitSchema(); err != nil {
			b.Fatal(err)
		}
		if _, err := s.Ingest(dir); err != nil {
			b.Fatal(err)
		}
		_ = s.Close()
	}
}
