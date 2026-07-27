package local

import (
	"slices"
	"testing"
)

func searchNotes(t *testing.T, words ...string) []int64 {
	t.Helper()
	db := openFixture(t)
	got, err := db.SearchNotes(words)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

// TestSearchNotes_MatchesNoteBodyReturnsParent — the ~5k docling extraction
// notes are child items, so a content hit has to surface the paper, the same
// way a PDF fulltext hit does.
func TestSearchNotes_MatchesNoteBodyReturnsParent(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t, "communicability"); !slices.Equal(got, []int64{20}) {
		t.Errorf("got %v, want [20] (note 91's parent)", got)
	}
}

func TestSearchNotes_StandaloneNoteReturnsItself(t *testing.T) {
	t.Parallel()
	// Note 70 has no parent — the note IS the item.
	if got := searchNotes(t, "Loose thoughts"); !slices.Equal(got, []int64{70}) {
		t.Errorf("got %v, want [70]", got)
	}
}

func TestSearchNotes_AndAcrossWords(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t, "successor", "communicability"); !slices.Equal(got, []int64{20}) {
		t.Errorf("got %v, want [20]", got)
	}
	// "neuroimaging" is in a PDF, not in any note — the AND must fail.
	if got := searchNotes(t, "successor", "neuroimaging"); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

func TestSearchNotes_CaseInsensitive(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t, "SUCCESSOR"); !slices.Equal(got, []int64{20}) {
		t.Errorf("got %v, want [20]", got)
	}
}

// TestSearchNotes_MarkupIsNotContent is the reason this can't be a bare SQL
// LIKE. Every Zotero note is wrapped in <div class="zotero-note znv1">, so a
// naive LIKE makes "div", "class" and "znv1" match the entire corpus.
func TestSearchNotes_MarkupIsNotContent(t *testing.T) {
	t.Parallel()
	for _, w := range []string{"div", "znv1", "zotero-note", "class", "h1"} {
		if got := searchNotes(t, w); len(got) != 0 {
			t.Errorf("markup word %q matched %v", w, got)
		}
	}
}

// TestSearchNotes_EntitiesAreDecoded — "&amp;" in the stored HTML is an
// ampersand to the reader, and searching for one should find it.
func TestSearchNotes_EntitiesAreDecoded(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t, "communicability & predictive"); !slices.Equal(got, []int64{20}) {
		t.Errorf("got %v, want [20]", got)
	}
}

// TestSearchNotes_TagsDoNotWeldWordsTogether — stripping <h1>…</h1><p> must
// leave a separator, or "Representations The" becomes one token and phrases
// spanning a tag boundary match things they shouldn't.
func TestSearchNotes_TagsDoNotWeldWordsTogether(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t, "RepresentationsThe"); len(got) != 0 {
		t.Errorf("tag boundary welded two words: %v", got)
	}
}

func TestSearchNotes_EmptyWordsIsNoOp(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestSearchNotes_LikeWildcardsAreLiteral — a query containing % or _ must
// search for those characters, not glob the whole corpus.
func TestSearchNotes_LikeWildcardsAreLiteral(t *testing.T) {
	t.Parallel()
	if got := searchNotes(t, "%"); len(got) != 0 {
		t.Errorf("%% globbed: %v", got)
	}
	if got := searchNotes(t, "successor_representation"); len(got) != 0 {
		t.Errorf("_ globbed: %v", got)
	}
}

// TestSearchNotes_ScopedToLibrary keeps group-library notes out of a personal
// search (and vice versa).
func TestSearchNotes_ScopedToLibrary(t *testing.T) {
	t.Parallel()
	dir := buildFixture(t)
	db, err := Open(dir, ForGroup(2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	got, err := db.SearchNotes([]string{"communicability"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("personal-library note leaked into the group scope: %v", got)
	}
}

// --- integration with the main search path ---------------------------------

// TestSearchWith_NotesWidensFreeText — --notes is the note-content twin of
// --fulltext: it widens positive free-text clauses, so a paper surfaces when
// only its extraction note mentions the term.
func TestSearchWith_NotesWidensFreeText(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// Without the option, "communicability" appears nowhere in the metadata.
	off, err := db.SearchWith("communicability", 10, SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Fatalf("notes matched without --notes: %v", keysOf(off))
	}

	on, err := db.SearchWith("communicability", 10, SearchOptions{Notes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 1 || on[0].Key != "BBBB2222" {
		t.Errorf("got %v, want [BBBB2222]", keysOf(on))
	}
}

// TestSearchWith_NotesComposesWithFulltext — both widenings can be on at
// once, and each still contributes its own hits.
func TestSearchWith_NotesComposesWithFulltext(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.SearchWith("neuroimaging", 10, SearchOptions{Notes: true, Fulltext: true})
	if err != nil {
		t.Fatal(err)
	}
	// "neuroimaging" is in item 10's title and PDF index, in no note.
	if len(got) != 1 || got[0].Key != "AAAA1111" {
		t.Errorf("got %v, want [AAAA1111]", keysOf(got))
	}
}

// TestSearchWith_NotesDoesNotWidenFieldScopedClauses mirrors --fulltext:
// "@title: x" means the title, not "somewhere in a note".
func TestSearchWith_NotesDoesNotWidenFieldScopedClauses(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.SearchWith("@title: communicability", 10, SearchOptions{Notes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("field-scoped clause consulted note content: %v", keysOf(got))
	}
}
