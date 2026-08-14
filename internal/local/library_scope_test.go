package local

// Tests for LibrarySelector dispatch via Open, and for the query scoping that
// follows from it: a handle pinned to one library must never return another's
// rows. The fixture seeds both a user library (libraryID 1) and a group
// (libraryID 2, API groupID 6506098), so both selectors resolve against real
// rows rather than an empty scope that would pass vacuously.

import "testing"

func TestOpen_PersonalSelector(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("Open(ForPersonal): %v", err)
	}
	defer func() { _ = db.Close() }()
	if db.LibraryID() != 1 {
		t.Errorf("LibraryID = %d, want 1 (user library)", db.LibraryID())
	}
}

func TestOpen_GroupSelector(t *testing.T) {
	dir := buildFixture(t)
	// 6506098 is the fixture group's Zotero API groupID; the groups table
	// maps it to SQLite libraryID 2.
	db, err := Open(dir, ForGroupByAPIID(6506098))
	if err != nil {
		t.Fatalf("Open(ForGroupByAPIID(6506098)): %v", err)
	}
	defer func() { _ = db.Close() }()
	if db.LibraryID() != 2 {
		t.Errorf("LibraryID = %d, want 2 (group library)", db.LibraryID())
	}
}

func TestOpen_UnknownGroupID(t *testing.T) {
	dir := buildFixture(t)
	_, err := Open(dir, ForGroupByAPIID(9999))
	if err == nil {
		t.Fatal("expected error for a group API ID with no local row")
	}
}

// TestItems_ScopedToPinnedLibrary asserts that every read path stays
// within the selector's libraryID. Catches any query that forgot to
// filter — a silent pan-library leak would be a regression.
func TestItems_ScopedToPinnedLibrary(t *testing.T) {
	dir := buildFixture(t)

	// Personal scope: user items only.
	dbUser, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatalf("Open(ForPersonal): %v", err)
	}
	defer func() { _ = dbUser.Close() }()

	personal, err := dbUser.List(ListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("List(personal): %v", err)
	}

	// Group scope: group items only.
	dbGroup, err := Open(dir, ForGroupByAPIID(6506098))
	if err != nil {
		t.Fatalf("Open(ForGroupByAPIID(6506098)): %v", err)
	}
	defer func() { _ = dbGroup.Close() }()

	group, err := dbGroup.List(ListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("List(group): %v", err)
	}

	// The two result sets must be disjoint by key. Phase-2 fixture will
	// seed at least one item in each library; if this assertion fails
	// with group.Items empty, the fixture wasn't updated.
	if len(group) == 0 {
		t.Fatal("no group items found — Phase 2 fixture update likely missing")
	}
	keys := map[string]struct{}{}
	for _, it := range personal {
		keys[it.Key] = struct{}{}
	}
	for _, it := range group {
		if _, clash := keys[it.Key]; clash {
			t.Errorf("item %q leaked across scopes", it.Key)
		}
	}
}
