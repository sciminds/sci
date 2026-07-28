package local

// Tests for the all-libraries mode: Open with ForAll merges the personal
// library and the configured shared group into one read pool. Only the
// converted query paths (Search, List/ListAll/CountList) honor the
// merged scope — the CLI gates which commands may open a multi-library
// DB, so the unconverted paths are unreachable under `all`.
//
// Every row carries Library ("personal" | "shared") regardless of mode —
// zen renders it per row, and a constant value on single-library reads
// is harmless where a missing one is a client-side join.

import (
	"slices"
	"testing"
)

func openFixtureAll(t *testing.T) *DB {
	t.Helper()
	dir := buildFixture(t)
	db, err := Open(dir, ForAll(6506098))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestForAll_SearchMergesLibraries(t *testing.T) {
	t.Parallel()
	db := openFixtureAll(t)
	// "cats" lives only in the personal library, "shared" only in the
	// group — one call, one ranked pool, hits from both.
	items, err := db.Search("cats | shared", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(items)
	for _, want := range []string{"CCCC3333", "GRPITEM01", "GRPITEM02"} {
		if !slices.Contains(got, want) {
			t.Errorf("merged search missing %s: %v", want, got)
		}
	}
}

func TestForAll_RowsCarryLibraryProvenance(t *testing.T) {
	t.Parallel()
	db := openFixtureAll(t)
	items, err := db.Search("cats | shared", 50)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Item{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	if byKey["CCCC3333"].Library != "personal" {
		t.Errorf("CCCC3333.Library = %q, want personal", byKey["CCCC3333"].Library)
	}
	if byKey["GRPITEM01"].Library != "shared" {
		t.Errorf("GRPITEM01.Library = %q, want shared", byKey["GRPITEM01"].Library)
	}
}

func TestSingleLibrary_RowsCarryConstantProvenance(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("cats", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Library != "personal" {
		t.Errorf("single-library rows should still stamp Library: %+v", keysOf(items))
	}
}

func TestForAll_ListAllMergesLibraries(t *testing.T) {
	t.Parallel()
	db := openFixtureAll(t)
	// bib resolves against ListAll's pool — it must span both libraries.
	items, err := db.ListAll(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(items)
	if !slices.Contains(got, "AAAA1111") || !slices.Contains(got, "GRPITEM01") {
		t.Errorf("ListAll pool should span both libraries: %v", got)
	}
}

func TestForAll_ReadFindsEitherLibrary(t *testing.T) {
	t.Parallel()
	db := openFixtureAll(t)
	for key, wantLib := range map[string]string{
		"AAAA1111":  "personal",
		"GRPITEM01": "shared",
	} {
		it, err := db.Read(key)
		if err != nil {
			t.Fatalf("Read(%s): %v", key, err)
		}
		if it.Library != wantLib {
			t.Errorf("Read(%s).Library = %q, want %q", key, it.Library, wantLib)
		}
	}
}

func TestForAll_LibraryIDIsZero(t *testing.T) {
	t.Parallel()
	db := openFixtureAll(t)
	// No single library owns a merged pool — 0 tells result shells the
	// top-level library_id is meaningless here (per-row Library rules).
	if got := db.LibraryID(); got != 0 {
		t.Errorf("LibraryID() = %d under all, want 0", got)
	}
}

func TestForAll_UnknownGroupErrors(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	if _, err := Open(dir, ForAll(999)); err == nil {
		t.Fatal("ForAll with an unsynced group should error, not degrade to personal-only")
	}
}
