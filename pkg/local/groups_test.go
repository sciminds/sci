package local

import "testing"

// TestGroups reads the shared-library identity out of the local database.
// This is the replacement for the `/users/{id}/groups` probe that setup used
// to make, so the assertion is the one the probe answered: a group's Web API
// id, its SQLite libraryID, and its display name, with no credential in play.
func TestGroups(t *testing.T) {
	db := openFixture(t)

	groups, err := db.Groups()
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}
	g := groups[0]
	if g.GroupID != 6506098 {
		t.Errorf("GroupID = %d, want 6506098", g.GroupID)
	}
	if g.LibraryID != 2 {
		t.Errorf("LibraryID = %d, want 2", g.LibraryID)
	}
	if g.Name != "sciminds" {
		t.Errorf("Name = %q, want %q", g.Name, "sciminds")
	}
}
