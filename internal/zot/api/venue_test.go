package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

// Field lists as GET /itemTypeFields actually returns them, trimmed.
var zoteroTypeFields = map[string][]string{
	"journalArticle":  {"title", "abstractNote", "publicationTitle", "volume", "issue", "pages", "DOI", "extra"},
	"bookSection":     {"title", "abstractNote", "bookTitle", "edition", "date", "publisher", "place", "pages", "ISBN", "extra"},
	"conferencePaper": {"title", "abstractNote", "proceedingsTitle", "conferenceName", "publisher", "place", "pages", "extra"},
	"book":            {"title", "abstractNote", "edition", "date", "publisher", "place", "numPages", "ISBN", "extra"},
}

func TestVenueFieldIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ itemType, want string }{
		{"journalArticle", "publicationTitle"},
		{"bookSection", "bookTitle"},
		{"conferencePaper", "proceedingsTitle"},
		// A book has a title but no venue — it IS the volume.
		{"book", ""},
	} {
		if got := VenueFieldIn(zoteroTypeFields[tc.itemType]); got != tc.want {
			t.Errorf("%s: VenueFieldIn = %q, want %q", tc.itemType, got, tc.want)
		}
	}
	if got := VenueFieldIn(nil); got != "" {
		t.Errorf("VenueFieldIn(nil) = %q, want empty", got)
	}
}

func TestSetVenueField(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		field string
		read  func(client.ItemData) *string
	}{
		{"publicationTitle", func(d client.ItemData) *string { return d.PublicationTitle }},
		{"bookTitle", func(d client.ItemData) *string { return d.BookTitle }},
		{"proceedingsTitle", func(d client.ItemData) *string { return d.ProceedingsTitle }},
	} {
		var d client.ItemData
		if err := SetVenueField(&d, tc.field, "The Cognitive Neurosciences"); err != nil {
			t.Fatalf("%s: %v", tc.field, err)
		}
		got := tc.read(d)
		if got == nil || *got != "The Cognitive Neurosciences" {
			t.Errorf("%s: not written to its own field: %+v", tc.field, d)
		}
	}
}

func TestSetVenueField_ClearsTheSiblingFields(t *testing.T) {
	t.Parallel()
	// The shape --openalex leaves behind when the user overrides --type:
	// a publicationTitle placed under the type OpenAlex guessed. Left in
	// place beside a bookTitle, Zotero rejects the whole create.
	stale := "Journal of Irreproducible Results"
	d := client.ItemData{PublicationTitle: &stale}
	if err := SetVenueField(&d, "bookTitle", "The Cognitive Neurosciences"); err != nil {
		t.Fatal(err)
	}
	if d.PublicationTitle != nil {
		t.Errorf("stale publicationTitle survived: %q", *d.PublicationTitle)
	}
	if d.BookTitle == nil || *d.BookTitle != "The Cognitive Neurosciences" {
		t.Errorf("bookTitle = %v", d.BookTitle)
	}
}

func TestSetVenueField_UnknownFieldErrors(t *testing.T) {
	t.Parallel()
	var d client.ItemData
	if err := SetVenueField(&d, "websiteTitle", "x"); err == nil {
		t.Fatal("want an error: websiteTitle is not a field client.ItemData models")
	}
}

func TestVenueField_ResolvesFromTheSchema(t *testing.T) {
	t.Parallel()
	h := newSchemaHandler()
	c, _ := newTestClient(t, h)

	got, err := c.VenueField(context.Background(), "bookSection")
	if err != nil {
		t.Fatal(err)
	}
	if got != "bookTitle" {
		t.Errorf("VenueField = %q, want bookTitle", got)
	}
}

func TestVenueField_NoVenueFieldIsNotAnError(t *testing.T) {
	t.Parallel()
	// A type declaring no venue field is an answer, not a failure — the
	// caller turns the empty string into a usage error naming the type.
	c, _ := newTestClient(t, newSchemaHandler())

	got, err := c.VenueField(context.Background(), "book")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("VenueField = %q, want empty", got)
	}
}

func TestItemTypeFields_CachesPerItemType(t *testing.T) {
	t.Parallel()
	h := newSchemaHandler()
	c, _ := newTestClient(t, h)

	for range 3 {
		if _, err := c.VenueField(context.Background(), "conferencePaper"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.ItemTypeFields(context.Background(), "conferencePaper"); err != nil {
		t.Fatal(err)
	}
	// /itemTypeFields is static and unauthenticated, and `item update`
	// resolves per key — refetching it once per key of a 50-item batch is
	// pure waste.
	if n := h.calls("/itemTypeFields"); n != 1 {
		t.Errorf("schema fetched %d times, want 1 (cached)", n)
	}
}

func TestItemTypeFields_TransportErrorPropagates(t *testing.T) {
	t.Parallel()
	h := newSchemaHandler()
	h.status = http.StatusInternalServerError
	c, _ := newTestClient(t, h)

	// A failed lookup must not launder into "this type has no venue
	// field" — that would silently drop the value the user passed.
	if _, err := c.VenueField(context.Background(), "journalArticle"); err == nil {
		t.Fatal("want an error when the schema lookup fails")
	}
}

func TestItemTypeCreatorTypes(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, newSchemaHandler())

	got, err := c.ItemTypeCreatorTypes(context.Background(), "bookSection")
	if err != nil {
		t.Fatal(err)
	}
	// An edited volume needs an editor, and a journalArticle has no
	// bookAuthor — which is exactly why this is asked per type.
	if !slices.Contains(got, "editor") || !slices.Contains(got, "bookAuthor") {
		t.Errorf("bookSection creator types = %v", got)
	}
}

func TestSetField_ReachesAnUnmodelledField(t *testing.T) {
	t.Parallel()
	// place, edition and conferenceName are real Zotero fields with no
	// member on client.ItemData — the whole point of the escape hatch.
	var d client.ItemData
	SetField(&d, "place", "Cambridge, MA")
	SetField(&d, "conferenceName", "SDAIR-94")

	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["place"] != "Cambridge, MA" {
		t.Errorf("place did not reach the wire: %s", raw)
	}
	if got["conferenceName"] != "SDAIR-94" {
		t.Errorf("conferenceName did not reach the wire: %s", raw)
	}
}

func TestSetField_WinsOverAModelledMember(t *testing.T) {
	t.Parallel()
	// MarshalJSON applies AdditionalProperties last. An explicit --field is
	// the more specific instruction, so this precedence is the right one —
	// but it is emergent from generated code, so it gets pinned.
	stale := "1-2"
	d := client.ItemData{Pages: &stale}
	SetField(&d, "pages", "45-70")

	raw, _ := json.Marshal(d)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["pages"] != "45-70" {
		t.Errorf("pages = %v, want the --field value to win: %s", got["pages"], raw)
	}
}
