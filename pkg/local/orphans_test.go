package local

import "testing"

func TestScanEmptyCollections(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	got, err := db.ScanEmptyCollections()
	if err != nil {
		t.Fatal(err)
	}
	// Fixture has collection 102 "Empty Box" with 0 items and no children.
	// Collection 100 "Brain Papers" has items; 101 "Favorites" has items
	// AND is a child of 100. Neither should appear.
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Key != "EMPTYCOL" || got[0].Name != "Empty Box" {
		t.Errorf("empty collection = %+v", got[0])
	}
}

func TestScanStandaloneAttachments(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	got, err := db.ScanStandaloneAttachments()
	if err != nil {
		t.Fatal(err)
	}
	// Attachment 40 has parent item 10 → not an orphan.
	// Attachment 60 has NULL parent → orphan.
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	a := got[0]
	if a.Key != "ORPHANATT" {
		t.Errorf("key = %q, want ORPHANATT", a.Key)
	}
	if a.Filename != "standalone.pdf" {
		t.Errorf("filename = %q, want standalone.pdf", a.Filename)
	}
	if a.ContentType != "application/pdf" {
		t.Errorf("contentType = %q", a.ContentType)
	}
}

func TestScanStandaloneNotes(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	got, err := db.ScanStandaloneNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.Key != "ORPHNNOTE" {
		t.Errorf("key = %q", n.Key)
	}
	if n.Title != "Attention Notes" {
		t.Errorf("title = %q", n.Title)
	}
}

func TestScanUncollectedItems(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	got, err := db.ScanUncollectedItems()
	if err != nil {
		t.Fatal(err)
	}
	// Items 10 and 20 are in collection 100; 10 is also in 101.
	// Items 30 and 80 are in no collection → uncollected.
	// Item 50 is trashed → excluded.
	// Items 40, 60, 81 (attachments) and 70 (note) are non-content → excluded.
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	keys := map[string]bool{}
	for _, it := range got {
		keys[it.Key] = true
	}
	if !keys["CCCC3333"] || !keys["GGGG7777"] {
		t.Errorf("keys = %v, want CCCC3333 + GGGG7777", keys)
	}
}

func TestScanUnusedTags(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	got, err := db.ScanUnusedTags()
	if err != nil {
		t.Fatal(err)
	}
	// Tag 4 "orphan-tag" has no itemTags row.
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Name != "orphan-tag" {
		t.Errorf("tag = %q, want orphan-tag", got[0].Name)
	}
}
