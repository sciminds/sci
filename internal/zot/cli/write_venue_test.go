package cli

// Tests for --publication venue routing on `item add` / `item update`.
//
// Zotero accepts a venue title under a different field name per item type,
// and sending the wrong one fails the whole request rather than degrading:
//
//	'publicationTitle' is not a valid field for type 'bookSection'
//
// which is what made book chapters and proceedings papers — the two types
// OpenAlex resolves worst, so the two most often entered by hand —
// impossible to create or repair through the CLI.
//
// The wire-level proof that the template drives this lives in
// internal/zot/api/venue_test.go; these pin the CLI's use of it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/client"
)

// stubVenueResolver answers from a table instead of the template endpoint.
type stubVenueResolver struct {
	fields map[string]string
	err    error
}

func (s stubVenueResolver) VenueField(_ context.Context, itemType string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.fields[itemType], nil
}

// zoteroVenues is what GET /items/new declares for these types, verbatim.
var zoteroVenues = stubVenueResolver{fields: map[string]string{
	"journalArticle":  "publicationTitle",
	"magazineArticle": "publicationTitle",
	"bookSection":     "bookTitle",
	"conferencePaper": "proceedingsTitle",
	"book":            "",
	"thesis":          "",
}}

func TestApplyVenue_LandsInTheTypesOwnField(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		itemType string
		read     func(client.ItemData) *string
		field    string
	}{
		{"journalArticle", func(d client.ItemData) *string { return d.PublicationTitle }, "publicationTitle"},
		{"bookSection", func(d client.ItemData) *string { return d.BookTitle }, "bookTitle"},
		{"conferencePaper", func(d client.ItemData) *string { return d.ProceedingsTitle }, "proceedingsTitle"},
	} {
		var data client.ItemData
		if err := applyVenue(context.Background(), zoteroVenues, &data, tc.itemType, "The Cognitive Neurosciences"); err != nil {
			t.Fatalf("%s: %v", tc.itemType, err)
		}
		if got := tc.read(data); got == nil || *got != "The Cognitive Neurosciences" {
			t.Errorf("%s: venue did not land in %s: %+v", tc.itemType, tc.field, data)
		}
		// The wrong field staying empty is the actual bug: a publicationTitle
		// on a bookSection is what Zotero rejects.
		if tc.itemType != "journalArticle" && data.PublicationTitle != nil {
			t.Errorf("%s: publicationTitle was set anyway — Zotero will reject this", tc.itemType)
		}
	}
}

func TestApplyVenue_TypeWithNoVenueIsAUsageError(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	err := applyVenue(context.Background(), zoteroVenues, &data, "book", "Some Press")
	if err == nil {
		t.Fatal("want an error: a book has no venue field")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("error is not coded, so it exits 1 as a runtime failure: %v", err)
	}
	// Exit 2. The old behaviour was a `runtime` error surfacing the API's
	// own complaint after a round trip; bad input should not need one.
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	if !strings.Contains(coded.Message, "book") {
		t.Errorf("message does not name the offending type: %q", coded.Message)
	}
	// Naming the two types that DO take one is the whole fix from the
	// user's side — otherwise there is no way to discover it.
	if !strings.Contains(coded.Try, "bookSection") || !strings.Contains(coded.Try, "conferencePaper") {
		t.Errorf("try does not name the types that accept a venue: %q", coded.Try)
	}
}

// stubVenueTargeter answers everything `item update` needs: each key's
// item type, and that type's schema.
type stubVenueTargeter struct {
	stubSchema
	types map[string]string
}

func (s stubVenueTargeter) GetItem(_ context.Context, key string) (*client.Item, error) {
	t, ok := s.types[key]
	if !ok {
		return nil, errors.New("no such item: " + key)
	}
	return &client.Item{Data: client.ItemData{ItemType: client.ItemDataItemType(t)}}, nil
}

func TestVenuePatches_ResolvesPerItemNotPerCall(t *testing.T) {
	t.Parallel()
	c := stubVenueTargeter{
		stubSchema: zoteroSchema,
		types: map[string]string{
			"JOURNAL1": "journalArticle",
			"CHAPTER1": "bookSection",
			"PROCEED1": "conferencePaper",
		},
	}
	keys := []string{"JOURNAL1", "CHAPTER1", "PROCEED1"}
	venue := "The Cognitive Neurosciences"

	got, err := perItemPatches(context.Background(), c, keys, client.ItemData{}, &venue, nil)
	if err != nil {
		t.Fatal(err)
	}
	// One --publication, three item types, three different fields. Reading
	// the type off the first key and reusing it would fail the other two.
	if p := got["JOURNAL1"].PublicationTitle; p == nil || *p != venue {
		t.Errorf("JOURNAL1 did not get publicationTitle: %+v", got["JOURNAL1"])
	}
	if p := got["CHAPTER1"].BookTitle; p == nil || *p != venue {
		t.Errorf("CHAPTER1 did not get bookTitle: %+v", got["CHAPTER1"])
	}
	if p := got["PROCEED1"].ProceedingsTitle; p == nil || *p != venue {
		t.Errorf("PROCEED1 did not get proceedingsTitle: %+v", got["PROCEED1"])
	}
}

func TestVenuePatches_NoPublicationSkipsTheLookup(t *testing.T) {
	t.Parallel()
	// No --publication means no reason to read the items at all; GetItem
	// returning an error for every key proves it was never called.
	c := stubVenueTargeter{stubSchema: zoteroSchema}
	title := "New Title"

	got, err := perItemPatches(context.Background(), c, []string{"AAAA1111", "BBBB2222"},
		client.ItemData{Title: &title}, nil, nil)
	if err != nil {
		t.Fatalf("an update with no --publication must not read the items: %v", err)
	}
	for _, k := range []string{"AAAA1111", "BBBB2222"} {
		if got[k].Title == nil || *got[k].Title != title {
			t.Errorf("%s lost the shared patch: %+v", k, got[k])
		}
	}
}

func TestApplyVenue_LookupFailureDoesNotDropTheValue(t *testing.T) {
	t.Parallel()
	var data client.ItemData
	err := applyVenue(context.Background(), stubVenueResolver{err: errors.New("boom")},
		&data, "journalArticle", "Nature")
	if err == nil {
		t.Fatal("want an error when the template lookup fails")
	}
	// A failed lookup must not read as "this type has no venue field" and
	// must not silently write nothing — either would lose user input.
	if coded, ok := errors.AsType[*cmdutil.CodedError](err); ok && coded.Code == cmdutil.CodeUsage {
		t.Error("a transport failure was reported as bad user input")
	}
}
