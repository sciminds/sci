package cli

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

// fakeCollectionReader stubs local.Reader's ListCollections for unit-testing
// resolveCollectionKey without spinning up the shared SQLite fixture.
type fakeCollectionReader struct {
	local.Reader
	collections []local.Collection
	err         error
}

func (f *fakeCollectionReader) ListCollections() ([]local.Collection, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.collections, nil
}

func TestResolveCollectionKey_ByExactKey(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "ABCD1234", Name: "missing-pdf"},
	}}
	key, name, err := resolveCollectionKey(db, "ABCD1234")
	if err != nil {
		t.Fatal(err)
	}
	if key != "ABCD1234" || name != "missing-pdf" {
		t.Errorf("key=%q name=%q", key, name)
	}
}

func TestResolveCollectionKey_ByNameCaseInsensitive(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "ABCD1234", Name: "Missing-PDF"},
	}}
	key, name, err := resolveCollectionKey(db, "missing-pdf")
	if err != nil {
		t.Fatal(err)
	}
	if key != "ABCD1234" {
		t.Errorf("key=%q", key)
	}
	if name != "Missing-PDF" {
		t.Errorf("display name should preserve canonical casing, got %q", name)
	}
}

func TestResolveCollectionKey_AmbiguousName(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "AAAA1111", Name: "notes"},
		{Key: "BBBB2222", Name: "notes"},
	}}
	_, _, err := resolveCollectionKey(db, "notes")
	if err == nil {
		t.Fatal("want error on ambiguous name")
	}
	if !strings.Contains(err.Error(), "AAAA1111") || !strings.Contains(err.Error(), "BBBB2222") {
		t.Errorf("error must list all matches, got %q", err)
	}
}

func TestResolveCollectionKey_NameNotFound(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "AAAA1111", Name: "notes"},
	}}
	_, _, err := resolveCollectionKey(db, "nonexistent")
	if err == nil {
		t.Fatal("want error when name is missing")
	}
}

func TestResolveCollectionKey_EmptyInput(t *testing.T) {
	t.Parallel()
	_, _, err := resolveCollectionKey(&fakeCollectionReader{}, "")
	if err == nil {
		t.Fatal("want error on empty input")
	}
}
