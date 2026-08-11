package cli

// Tests for `item update --type` and for creators on update.
//
// Both existed on `item add` only. The gap was discovered repairing a real
// shared-library item: a PDF import had filed the Gweon et al. 2023 Phil
// Trans A article as a `document` whose sole author was "Judi Thfan" (the
// PDF's mangled byline) and whose venue sat in `publisher`, because a
// `document` has no publicationTitle. Every part of that repair — the type,
// the creators, and the journal fields that only exist on the new type —
// was unreachable from the CLI, so the item could only be fixed by hand in
// the desktop app or by POSTing to the Web API directly.
//
// The data-safety line these tests draw: creators REPLACE. A Zotero PATCH
// overwrites whole arrays, so a creator flag states the complete new list
// and anything the command line does not restate is gone. That is why the
// flags are documented as replacing and why there is no "add one author".

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/client"
)

// stubTypeTargeter answers everything `item update` needs: each key's
// current item data, and the schema for any type.
type stubTypeTargeter struct {
	stubSchema
	items map[string]client.ItemData
	gets  int
}

func (s *stubTypeTargeter) GetItem(_ context.Context, key string) (*client.Item, error) {
	s.gets++
	d, ok := s.items[key]
	if !ok {
		return nil, errors.New("no such item: " + key)
	}
	return &client.Item{Data: d}, nil
}

// gweonDocument is the real shape of the item that motivated this feature:
// a `document` carrying the venue in `publisher` and a wrong single author.
func gweonDocument() client.ItemData {
	title := "Socially intelligent machines that learn from humans and help humans learn"
	pub := "Philosophical Transactions of the Royal Society A."
	doi := "10.1098/rsta.2022.0048"
	ckey := "Thfan2023-pk"
	first, last := "Judi", "Thfan"
	return client.ItemData{
		ItemType:    "document",
		Title:       &title,
		Publisher:   &pub,
		DOI:         &doi,
		CitationKey: &ckey,
		Creators:    &[]client.Creator{{CreatorType: "author", FirstName: &first, LastName: &last}},
	}
}

func newGweonTargeter() *stubTypeTargeter {
	return &stubTypeTargeter{
		stubSchema: zoteroSchema,
		items:      map[string]client.ItemData{"Y26R8338": gweonDocument()},
	}
}

// TestUpdate_CreatorsReplaceTheWholeList.
//
// The item arrives with one wrong author and a bogus editor; the repair
// states three authors and no editor. Replace semantics means the result is
// exactly those three — a merge would leave "Judi Thfan" in place forever,
// which is the whole defect being repaired.
func TestUpdate_CreatorsReplaceTheWholeList(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()
	spec := updateSpec{
		newType: "journalArticle",
		authors: []string{"Gweon, Hyowon", "Fan, Judith", "Kim, Been"},
	}

	got, _, err := perItemPatches(context.Background(), c, []string{"Y26R8338"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	creators := got["Y26R8338"].Creators
	if creators == nil {
		t.Fatal("no creators on the patch: the wrong author would survive the repair")
	}
	if len(*creators) != 3 {
		t.Fatalf("got %d creators, want exactly 3 — the list REPLACES", len(*creators))
	}
	wantLast := []string{"Gweon", "Fan", "Kim"}
	for i, w := range wantLast {
		if l := (*creators)[i].LastName; l == nil || *l != w {
			t.Errorf("creator %d last name = %v, want %q (order is a claim about contribution)", i, l, w)
		}
		if ct := (*creators)[i].CreatorType; ct != "author" {
			t.Errorf("creator %d type = %q, want author", i, ct)
		}
	}
}

// TestUpdate_NoCreatorFlagsLeavesCreatorsAlone pins the other half of
// replace semantics: an update that never names a creator must not carry a
// creators array at all, or every non-creator edit would blank the authors.
func TestUpdate_NoCreatorFlagsLeavesCreatorsAlone(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()
	doi := "10.1098/rsta.2022.0048"

	got, _, err := perItemPatches(context.Background(), c, []string{"Y26R8338"},
		updateSpec{patch: client.ItemData{DOI: &doi}})
	if err != nil {
		t.Fatal(err)
	}
	if got["Y26R8338"].Creators != nil {
		t.Fatal("an update with no creator flags carried a creators array — it would erase the item's authors")
	}
}

// TestUpdate_TypeChangeCarriesTheNewType.
func TestUpdate_TypeChangeCarriesTheNewType(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()

	got, _, err := perItemPatches(context.Background(), c, []string{"Y26R8338"},
		updateSpec{newType: "journalArticle"})
	if err != nil {
		t.Fatal(err)
	}
	if it := got["Y26R8338"].ItemType; string(it) != "journalArticle" {
		t.Errorf("itemType = %q, want journalArticle", it)
	}
}

// TestUpdate_SchemaFlagsValidateAgainstTheNewType.
//
// The repair sets volume/issue/pages and a publicationTitle in the SAME
// command that changes the type. None of those fields exists on `document`,
// so validating against the item's current type would refuse the write and
// force a two-step repair through an intermediate state.
func TestUpdate_SchemaFlagsValidateAgainstTheNewType(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()
	venue := "Philosophical Transactions of the Royal Society A: Mathematical, Physical and Engineering Sciences"
	spec := updateSpec{
		newType: "journalArticle",
		venue:   &venue,
		fields:  []string{"volume=381", "issue=2251", "pages=20220048"},
	}

	got, _, err := perItemPatches(context.Background(), c, []string{"Y26R8338"}, spec)
	if err != nil {
		t.Fatalf("schema flags were validated against the OLD type: %v", err)
	}
	d := got["Y26R8338"]
	if d.PublicationTitle == nil || *d.PublicationTitle != venue {
		t.Errorf("publicationTitle = %v, want the journal", d.PublicationTitle)
	}
	for name, want := range map[string]string{"volume": "381", "issue": "2251", "pages": "20220048"} {
		v, ok := d.Get(name)
		if !ok || v != want {
			t.Errorf("%s = %v (present=%v), want %q", name, v, ok, want)
		}
	}
}

// TestUpdate_CreatorTypesValidateAgainstTheNewType is the same rule for
// --creator: a reviewedAuthor is legal on a journalArticle, and refusing it
// because the item is still a `document` would be validating against a type
// the item is about to stop being.
func TestUpdate_CreatorTypesValidateAgainstTheNewType(t *testing.T) {
	t.Parallel()
	c := &stubTypeTargeter{
		stubSchema: zoteroSchema,
		items:      map[string]client.ItemData{"BOOKSEC1": {ItemType: "bookSection"}},
	}
	// bookAuthor is valid on bookSection and NOT on journalArticle, so a
	// type change to journalArticle must refuse it.
	_, _, err := perItemPatches(context.Background(), c, []string{"BOOKSEC1"},
		updateSpec{newType: "journalArticle", creators: []string{"bookAuthor:Smith, Alice"}})
	if err == nil {
		t.Fatal("want a refusal: bookAuthor is not a creator type on journalArticle")
	}
	coded := codedFrom(t, err)
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %v, want usage", coded.Code)
	}
}

// TestUpdate_TypeChangeClearsFieldsTheNewTypeCannotHold.
//
// Zotero validates the item that RESULTS from a patch, so leaving a
// `publisher` on an item becoming a journalArticle is a failed write, not a
// degraded one. Clearing them in the same patch is also what makes the
// dropped-field report exact rather than a guess about server behaviour.
func TestUpdate_TypeChangeClearsFieldsTheNewTypeCannotHold(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()

	got, plans, err := perItemPatches(context.Background(), c, []string{"Y26R8338"},
		updateSpec{newType: "journalArticle"})
	if err != nil {
		t.Fatal(err)
	}
	// `publisher` and `citationKey` are on `document` and not on
	// journalArticle; `title` and `DOI` are on both and must survive.
	d := got["Y26R8338"]
	for _, name := range []string{"publisher", "citationKey"} {
		v, ok := d.Get(name)
		if !ok {
			t.Errorf("%s was not cleared: Zotero would refuse the whole patch", name)
			continue
		}
		if v != "" {
			t.Errorf("%s = %v, want the empty string that removes it", name, v)
		}
	}
	if d.Title != nil {
		t.Error("title was cleared though journalArticle declares it")
	}
	if !slices.Contains(plans["Y26R8338"].WillDrop, "publisher") {
		t.Errorf("the plan did not predict dropping publisher: %v", plans["Y26R8338"].WillDrop)
	}
	if plans["Y26R8338"].FromType != "document" {
		t.Errorf("plan lost the old type: %+v", plans["Y26R8338"])
	}
}

// TestUpdate_SameTypeIsNotATypeChange — passing --type with the type the
// item already has must not clear anything, because nothing stops applying.
func TestUpdate_SameTypeIsNotATypeChange(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()

	got, plans, err := perItemPatches(context.Background(), c, []string{"Y26R8338"},
		updateSpec{newType: "document"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["Y26R8338"].Get("publisher"); ok {
		t.Errorf("publisher was cleared on a no-op type change: %v", v)
	}
	if len(plans["Y26R8338"].WillDrop) != 0 {
		t.Errorf("a no-op type change predicted drops: %v", plans["Y26R8338"].WillDrop)
	}
}

// TestValidateItemType_UnknownTypeRefusedBeforeAnyWrite.
//
// An unknown type must fail as USAGE, before the command reads or writes a
// single item: Zotero would otherwise answer 400 after the round trip, and
// on a multi-key update some items would already have been patched.
func TestValidateItemType_UnknownTypeRefusedBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	c := newGweonTargeter()

	err := validateItemType(context.Background(), c, "journal-article")
	if err == nil {
		t.Fatal("want a refusal for a type Zotero does not declare")
	}
	coded := codedFrom(t, err)
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %v, want usage", coded.Code)
	}
	// The remedy is the correct spelling, so the message has to carry it.
	if !strings.Contains(coded.Try, "journalArticle") {
		t.Errorf("try does not name the real types: %q", coded.Try)
	}
	if c.gets != 0 {
		t.Errorf("read %d item(s) before refusing an invalid type", c.gets)
	}
}

func TestValidateItemType_EmptyTypeSkipsTheLookup(t *testing.T) {
	t.Parallel()
	// err on the schema proves the lookup never happened.
	c := &stubTypeTargeter{stubSchema: stubSchema{err: errors.New("boom")}}
	if err := validateItemType(context.Background(), c, ""); err != nil {
		t.Fatalf("no --type must not consult the schema: %v", err)
	}
}

// TestDroppedFields_ReportsWhatTheServerRemoved.
//
// The report is a DIFF of two reads, not a replay of what we asked for: the
// caller needs to know what the server actually did with the item, and a
// type change removes fields nobody named.
func TestDroppedFields_ReportsWhatTheServerRemoved(t *testing.T) {
	t.Parallel()
	before := gweonDocument()
	after := client.ItemData{ItemType: "journalArticle"}
	title := "Socially intelligent machines that learn from humans and help humans learn"
	doi := "10.1098/rsta.2022.0048"
	vol := "381"
	after.Title = &title
	after.DOI = &doi
	after.Volume = &vol

	got := droppedFields(before, after)
	want := []string{"citationKey", "publisher"}
	if !slices.Equal(got, want) {
		t.Errorf("dropped = %v, want %v (sorted, and only fields that VANISHED)", got, want)
	}
}

// TestDroppedFields_IgnoresChangedAndAddedFields — a field whose value
// changed is not a loss, and reporting it as one would bury the real
// casualties in noise.
func TestDroppedFields_IgnoresChangedAndAddedFields(t *testing.T) {
	t.Parallel()
	oldTitle, newTitle := "Old", "New"
	vol := "381"
	before := client.ItemData{ItemType: "journalArticle", Title: &oldTitle}
	after := client.ItemData{ItemType: "journalArticle", Title: &newTitle, Volume: &vol}

	if got := droppedFields(before, after); len(got) != 0 {
		t.Errorf("dropped = %v, want none", got)
	}
}
