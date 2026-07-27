package local

import (
	"slices"
	"testing"
)

func TestItemRelations_RelatedItems(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemRelations("ORPHNNOTE")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"BBBB2222"}; !slices.Equal(got.Related, want) {
		t.Errorf("Related = %v, want %v", got.Related, want)
	}
}

// The relation is written on both items (that is how Zotero's UI stores
// it), so reading from either end must find the other.
func TestItemRelations_ReadableFromBothEnds(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemRelations("BBBB2222")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ORPHNNOTE"}; !slices.Equal(got.Related, want) {
		t.Errorf("Related = %v, want %v", got.Related, want)
	}
}

// owl:sameAs and dc:replaces are Zotero's own. They surface separately so a
// listing can show them without implying sci wrote them — and so `link rm`
// can refuse to touch them.
func TestItemRelations_SeparatesZoteroOwnedPredicates(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemRelations("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Related) != 0 {
		t.Errorf("Related = %v, want empty (the only relation is owl:sameAs)", got.Related)
	}
	if want := []string{"GRPCOPY1"}; !slices.Equal(got.Other["owl:sameAs"], want) {
		t.Errorf("Other[owl:sameAs] = %v, want %v", got.Other["owl:sameAs"], want)
	}
}

func TestItemRelations_NoRelationsIsEmptyNotError(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemRelations("CCCC3333")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Related) != 0 || len(got.Other) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}
