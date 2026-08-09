package cli

// Tests for the --field / --creator escape hatches on `item add`, and for
// the slice-flag comma split that used to corrupt every creator name.
//
// Zotero's item types accept ~30 fields and ~6 creator types each, and sci
// modelled six fields and one creator type. Filing a book chapter meant
// POSTing to the Web API by hand to reach edition, publisher, place, pages
// and an editor. Rather than grow eight more flags, --field and --creator
// take any name the item type's own schema declares.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/client"
)

// stubSchema answers the two per-item-type schema lookups from a table.
type stubSchema struct {
	stubVenueResolver
	fields   map[string][]string
	creators map[string][]string
	err      error
}

func (s stubSchema) ItemTypeFields(_ context.Context, itemType string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.fields[itemType], nil
}

func (s stubSchema) ItemTypeCreatorTypes(_ context.Context, itemType string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.creators[itemType], nil
}

var zoteroSchema = stubSchema{
	stubVenueResolver: zoteroVenues,
	fields: map[string][]string{
		"journalArticle":  {"title", "abstractNote", "publicationTitle", "volume", "issue", "pages", "DOI", "extra"},
		"bookSection":     {"title", "abstractNote", "bookTitle", "edition", "date", "publisher", "place", "pages", "ISBN", "extra"},
		"conferencePaper": {"title", "abstractNote", "proceedingsTitle", "conferenceName", "publisher", "place", "pages", "extra"},
	},
	creators: map[string][]string{
		"journalArticle":  {"author", "contributor", "editor", "reviewedAuthor", "translator"},
		"bookSection":     {"author", "bookAuthor", "contributor", "editor", "seriesEditor", "translator"},
		"conferencePaper": {"author", "contributor", "editor", "seriesEditor", "translator"},
	},
}

func codedFrom(t *testing.T, err error) *cmdutil.CodedError {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("error is not coded, so it exits 1 as a runtime failure: %v", err)
	}
	return coded
}

func TestParseFieldAssignment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, name, value string }{
		{"pages=45-70", "pages", "45-70"},
		{"place=Cambridge, MA", "place", "Cambridge, MA"},
		// A value may contain =; only the FIRST one separates.
		{"url=https://x.test/a?b=c", "url", "https://x.test/a?b=c"},
		// An explicitly empty value is a real instruction, same as
		// --extra "" — it is how you blank a field.
		{"extra=", "extra", ""},
	} {
		name, value, err := parseFieldAssignment(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if name != tc.name || value != tc.value {
			t.Errorf("%q -> (%q, %q), want (%q, %q)", tc.in, name, value, tc.name, tc.value)
		}
	}
}

func TestParseFieldAssignment_Rejects(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"pages", "=45-70", ""} {
		if _, _, err := parseFieldAssignment(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}

func TestParseCreatorAssignment(t *testing.T) {
	t.Parallel()
	// The name half carries a comma, which is the whole reason the slice
	// separator has to be off.
	kind, name, err := parseCreatorAssignment("editor:Gazzaniga, Michael")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "editor" || name != "Gazzaniga, Michael" {
		t.Errorf("got (%q, %q)", kind, name)
	}
	for _, in := range []string{"Gazzaniga, Michael", ":x", "editor:"} {
		if _, _, err := parseCreatorAssignment(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}

func TestApplyFields_WritesWhatTheTypeDeclares(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	err := applyFields(context.Background(), zoteroSchema, &data, "bookSection",
		[]string{"edition=2", "publisher=MIT Press", "place=Cambridge, MA", "pages=45-70"})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"edition": "2", "publisher": "MIT Press", "place": "Cambridge, MA", "pages": "45-70",
	} {
		got, ok := data.Get(name)
		if !ok || got != want {
			t.Errorf("%s = %v, want %q", name, got, want)
		}
	}
}

func TestApplyFields_UnknownFieldIsAUsageError(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	// numPages is a real Zotero field — on `book`, not on `bookSection`.
	// Rejecting per type rather than against a global field list is the
	// point: the API would fail the whole request otherwise.
	err := applyFields(context.Background(), zoteroSchema, &data, "bookSection", []string{"numPages=300"})
	coded := codedFrom(t, err)
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	if !strings.Contains(coded.Message, "numPages") || !strings.Contains(coded.Message, "bookSection") {
		t.Errorf("message names neither the field nor the type: %q", coded.Message)
	}
	// The valid set is the actionable half — without it there is nothing
	// to do but guess.
	if !strings.Contains(coded.Try, "edition") {
		t.Errorf("try does not list the type's real fields: %q", coded.Try)
	}
}

func TestApplyFields_RefusesAFieldADedicatedFlagOwns(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	// Silent precedence between --title and --field title= is exactly the
	// kind of thing that writes the wrong value and looks fine.
	err := applyFields(context.Background(), zoteroSchema, &data, "bookSection", []string{"title=Something"})
	coded := codedFrom(t, err)
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	if !strings.Contains(coded.Try, "--title") {
		t.Errorf("try does not name the flag that owns the field: %q", coded.Try)
	}
}

func TestApplyFields_VenueFieldsBelongToPublication(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	err := applyFields(context.Background(), zoteroSchema, &data, "bookSection", []string{"bookTitle=X"})
	coded := codedFrom(t, err)
	if !strings.Contains(coded.Try, "--publication") {
		t.Errorf("try should route the venue fields to --publication: %q", coded.Try)
	}
}

func TestApplyCreators_BuildsTypedCreatorsInOrder(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	err := applyCreators(context.Background(), zoteroSchema, &data, "bookSection",
		[]string{"Manning, Jeremy", "Norman, Kenneth"},
		[]string{"editor:Gazzaniga, Michael", "editor:Mangun, George"})
	if err != nil {
		t.Fatal(err)
	}
	if data.Creators == nil {
		t.Fatal("no creators built")
	}
	got := *data.Creators
	// Author order is a claim about contribution, so it is preserved, and
	// --author comes before --creator.
	want := []struct{ kind, last, first string }{
		{"author", "Manning", "Jeremy"},
		{"author", "Norman", "Kenneth"},
		{"editor", "Gazzaniga", "Michael"},
		{"editor", "Mangun", "George"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d creators, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		c := got[i]
		if string(c.CreatorType) != w.kind {
			t.Errorf("creator %d type = %q, want %q", i, c.CreatorType, w.kind)
		}
		if c.LastName == nil || *c.LastName != w.last || c.FirstName == nil || *c.FirstName != w.first {
			t.Errorf("creator %d = %+v, want %s, %s", i, c, w.last, w.first)
		}
	}
}

func TestApplyCreators_UnknownTypeForThisItemTypeIsAUsageError(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	// bookAuthor is real, and real on bookSection — but not on a journal
	// article, which is why the check is per item type.
	err := applyCreators(context.Background(), zoteroSchema, &data, "journalArticle",
		nil, []string{"bookAuthor:Smith, Alice"})
	coded := codedFrom(t, err)
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	if !strings.Contains(coded.Try, "reviewedAuthor") {
		t.Errorf("try does not list the type's real creator types: %q", coded.Try)
	}
}

func TestApplyCreators_InstitutionalNameStaysOneField(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	if err := applyCreators(context.Background(), zoteroSchema, &data, "journalArticle",
		[]string{"NASA"}, nil); err != nil {
		t.Fatal(err)
	}
	c := (*data.Creators)[0]
	if c.Name == nil || *c.Name != "NASA" {
		t.Errorf("institutional creator was split: %+v", c)
	}
}
