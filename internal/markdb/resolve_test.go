package markdb

import (
	"database/sql"
	"testing"
)

func TestResolveWikilink(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "hello.md", "---\ntitle: Hello\n---\nSee [[world]]")
	writeFile(t, dir, "world.md", "---\ntitle: World\n---\nContent")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	resolved, broken, err := s.ResolveLinks()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 {
		t.Errorf("resolved = %d, want 1", resolved)
	}
	if broken != 0 {
		t.Errorf("broken = %d, want 0", broken)
	}

	// Verify target_id is set.
	var targetID sql.NullInt64
	err = s.db.QueryRow("SELECT target_id FROM links").Scan(&targetID)
	if err != nil {
		t.Fatal(err)
	}
	if !targetID.Valid {
		t.Error("target_id is NULL, want non-NULL")
	}
}

func TestResolveWikilinkCaseInsensitive(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "hello.md", "See [[World]]")
	writeFile(t, dir, "world.md", "Content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := s.ResolveLinks()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 {
		t.Errorf("resolved = %d, want 1", resolved)
	}
}

func TestResolveWikilinkWithExtension(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "hello.md", "See [[world.md]]")
	writeFile(t, dir, "world.md", "Content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := s.ResolveLinks()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 {
		t.Errorf("resolved = %d, want 1", resolved)
	}
}

func TestResolveRelativeLink(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "sub/hello.md", "See [other](../other.md)")
	writeFile(t, dir, "other.md", "Content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := s.ResolveLinks()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 {
		t.Errorf("resolved = %d, want 1", resolved)
	}
}

func TestResolveBrokenLink(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "hello.md", "See [[nonexistent]]")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	resolved, broken, err := s.ResolveLinks()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 0 {
		t.Errorf("resolved = %d, want 0", resolved)
	}
	if broken != 1 {
		t.Errorf("broken = %d, want 1", broken)
	}

	var targetID sql.NullInt64
	err = s.db.QueryRow("SELECT target_id FROM links").Scan(&targetID)
	if err != nil {
		t.Fatal(err)
	}
	if targetID.Valid {
		t.Error("target_id is non-NULL, want NULL for broken link")
	}
}

func TestResolveLinkMetadata(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "hello.md", "Line 1\n\nSee [[world#heading|display]]")
	writeFile(t, dir, "world.md", "Content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveLinks(); err != nil {
		t.Fatal(err)
	}

	var raw, targetPath, fragment, alias string
	var line int
	err := s.db.QueryRow(
		"SELECT raw, target_path, fragment, alias, line FROM links",
	).Scan(&raw, &targetPath, &fragment, &alias, &line)
	if err != nil {
		t.Fatal(err)
	}
	if fragment != "heading" {
		t.Errorf("fragment = %q, want heading", fragment)
	}
	if alias != "display" {
		t.Errorf("alias = %q, want display", alias)
	}
	if line != 3 {
		t.Errorf("line = %d, want 3", line)
	}
}

func TestResolveLinksReIngest(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "hello.md", "See [[world]]")
	writeFile(t, dir, "world.md", "Content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveLinks(); err != nil {
		t.Fatal(err)
	}

	// Change the link.
	writeFile(t, dir, "hello.md", "See [[other]]")
	writeFile(t, dir, "other.md", "Other content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveLinks(); err != nil {
		t.Fatal(err)
	}

	// Should have links to "other", not "world".
	var count int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM links WHERE target_path = 'other'").Scan(&count)
	if count != 1 {
		t.Errorf("links to 'other' = %d, want 1", count)
	}
	_ = s.db.QueryRow("SELECT COUNT(*) FROM links WHERE target_path = 'world'").Scan(&count)
	if count != 0 {
		t.Errorf("links to 'world' = %d, want 0", count)
	}
}
