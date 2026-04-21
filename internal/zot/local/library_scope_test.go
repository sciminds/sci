package local

// Tests for LibrarySelector dispatch via Open + local query scoping. These reference
// symbols not yet implemented:
//   - LibrarySelector (type)
//   - ForPersonal() / ForGroup(int64) (constructors)
//   - Open(dir, selector) (new signature)
// Fixture updates (adding a group library + group items) happen in Phase 2.

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
	// Fixture (updated in Phase 2) must include a row:
	//   libraries(libraryID=2, type='group', version=...)
	// and at least one item scoped to libraryID=2 for downstream tests.
	dir := buildFixture(t)
	db, err := Open(dir, ForGroup(2))
	if err != nil {
		t.Fatalf("Open(ForGroup(2)): %v", err)
	}
	defer func() { _ = db.Close() }()
	if db.LibraryID() != 2 {
		t.Errorf("LibraryID = %d, want 2 (group library)", db.LibraryID())
	}
}

func TestOpen_UnknownGroupID(t *testing.T) {
	dir := buildFixture(t)
	_, err := Open(dir, ForGroup(9999))
	if err == nil {
		t.Fatal("expected error for unknown group libraryID")
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
	dbGroup, err := Open(dir, ForGroup(2))
	if err != nil {
		t.Fatalf("Open(ForGroup(2)): %v", err)
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

// TestOpen_BackcompatSignatureRemoved guards against a stale Open(dir)
// call being reintroduced after the selector lands. This test should
// fail to compile if someone accidentally restores the one-arg version.
//
// It is commented out deliberately — enable after Phase 2 if you want a
// belt-and-suspenders check (and a compile-time regression alarm).
//
//	func TestOpen_BackcompatSignatureRemoved(t *testing.T) {
//		_, _ = Open(".") // should not compile
//	}
