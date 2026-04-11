package local

import (
	"os"
	"testing"
)

// TestRealLibrary_Smoke runs the local reader against the user's real
// zotero.sqlite when ZOT_REAL_DB points at the containing directory.
// Skipped by default so it never runs in CI or on other machines.
func TestRealLibrary_Smoke(t *testing.T) {
	dir := os.Getenv("ZOT_REAL_DB")
	if dir == "" {
		t.Skip("set ZOT_REAL_DB=<dir containing zotero.sqlite> to run")
	}
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	t.Logf("schema=%d library=%d outOfRange=%v", db.SchemaVersion(), db.LibraryID(), db.SchemaOutOfRange())

	s, err := db.Stats()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stats: total=%d withDOI=%d withAbstract=%d withAttachment=%d colls=%d tags=%d",
		s.TotalItems, s.WithDOI, s.WithAbstract, s.WithAttachment, s.Collections, s.Tags)
	if s.TotalItems == 0 {
		t.Error("real library reports zero items — query likely broken")
	}

	items, err := db.Search("neuro", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		t.Logf("  %s [%s] %s", it.Key, it.Type, it.Title)
	}
	if len(items) == 0 {
		t.Skip("no neuro results in this library — not a failure, just uninformative")
	}
	full, err := db.Read(items[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read %s: type=%s creators=%d tags=%d attachments=%d",
		full.Key, full.Type, len(full.Creators), len(full.Tags), len(full.Attachments))
}
