package local

import (
	"slices"
	"testing"
)

func TestMissingPDFKeys(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	keys, err := db.MissingPDFKeys()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)

	// The four quadrants the predicate must get right:
	//   - item 10 (AAAA1111): PDF child            → has its PDF, excluded
	//   - item 20 (BBBB2222): note child ONLY      → missing; the case a
	//     numChildren==0 reading gets wrong (it has a child, just not a PDF)
	//   - item 30 (CCCC3333): no children at all   → missing
	//   - item 70 (ORPHNNOTE): standalone note     → not bibliographic, excluded
	// Plus: 50 is trashed, 60 is a standalone attachment, 92 is an
	// annotation — all excluded; group-library items never leak into the
	// personal scope.
	want := []string{"BBBB2222", "CCCC3333"}
	if !slices.Equal(keys, want) {
		t.Errorf("MissingPDFKeys() = %v, want %v", keys, want)
	}
}

func TestMissingPDFKeys_GroupScope(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForGroup(2))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	keys, err := db.MissingPDFKeys()
	if err != nil {
		t.Fatal(err)
	}

	// GRPITEM01 has PDF child GRPATT01; GRPITEM02 has no children.
	want := []string{"GRPITEM02"}
	if !slices.Equal(keys, want) {
		t.Errorf("MissingPDFKeys() = %v, want %v", keys, want)
	}
}
