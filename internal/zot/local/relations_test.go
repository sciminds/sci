package local

import (
	"maps"
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

// ── ItemLabels ───────────────────────────────────────────────────────

func TestItemLabels_ResolvesItemsAndNotesInOneQuery(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemLabels([]string{"BBBB2222", "ORPHNNOTE"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"BBBB2222":  "Transformers in fMRI Analysis",
		"ORPHNNOTE": "Attention Notes",
	}
	if !maps.Equal(got, want) {
		t.Errorf("ItemLabels = %v, want %v", got, want)
	}
}

// A key with no local row (a relation pointing into another library, which
// is exactly what owl:sameAs does) is simply absent — the caller renders
// the bare key rather than an empty label.
func TestItemLabels_UnknownKeyIsAbsentNotError(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemLabels([]string{"GRPCOPY1", "NOSUCH00"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("ItemLabels = %v, want empty", got)
	}
}

func TestItemLabels_EmptyInput(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	got, err := db.ItemLabels(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("ItemLabels(nil) = %v, want empty", got)
	}
}

func TestRelationLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		title string
		body  string
		want  string
	}{
		{"title wins", "Attention Notes", "<p>whatever</p>", "Attention Notes"},
		{"untitled note falls back to body", "",
			`<div class="zotero-note znv1"><p>Loose thoughts on attention.</p></div>`,
			"Loose thoughts on attention."},
		{"nothing to show", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := relationLabel(tt.title, tt.body); got != tt.want {
				t.Errorf("relationLabel(%q, %q) = %q, want %q", tt.title, tt.body, got, tt.want)
			}
		})
	}
}

// ── Read hydration ───────────────────────────────────────────────────

func TestRead_HydratesRelationsWithLabels(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	it, err := db.Read("BBBB2222")
	if err != nil {
		t.Fatal(err)
	}
	if it.Relations == nil {
		t.Fatal("Relations = nil, want the dc:relation to ORPHNNOTE")
	}
	if want := []string{"ORPHNNOTE"}; !slices.Equal(it.Relations.Related, want) {
		t.Errorf("Related = %v, want %v", it.Relations.Related, want)
	}
	if got := it.Relations.Titles["ORPHNNOTE"]; got != "Attention Notes" {
		t.Errorf("Titles[ORPHNNOTE] = %q, want %q", got, "Attention Notes")
	}
}

// Zotero-managed predicates ride along too, and a far end that lives in
// another library simply has no label.
func TestRead_HydratesZoteroManagedRelations(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	it, err := db.Read("AAAA1111")
	if err != nil {
		t.Fatal(err)
	}
	if it.Relations == nil {
		t.Fatal("Relations = nil, want the owl:sameAs to GRPCOPY1")
	}
	if want := []string{"GRPCOPY1"}; !slices.Equal(it.Relations.Other["owl:sameAs"], want) {
		t.Errorf("Other[owl:sameAs] = %v, want %v", it.Relations.Other["owl:sameAs"], want)
	}
	if got, ok := it.Relations.Titles["GRPCOPY1"]; ok {
		t.Errorf("Titles[GRPCOPY1] = %q, want absent (item lives in another library)", got)
	}
}

// An item with no relations leaves the field nil so the JSON key stays
// absent — every command's shape is byte-identical to before.
func TestRead_NoRelationsLeavesFieldNil(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	it, err := db.Read("CCCC3333")
	if err != nil {
		t.Fatal(err)
	}
	if it.Relations != nil {
		t.Errorf("Relations = %+v, want nil", it.Relations)
	}
}
