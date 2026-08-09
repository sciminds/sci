package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

func TestVenueFieldOf(t *testing.T) {
	t.Parallel()
	blank := ""
	for _, tc := range []struct {
		name string
		tmpl client.ItemData
		want string
	}{
		{"journalArticle", client.ItemData{PublicationTitle: &blank}, "publicationTitle"},
		{"bookSection", client.ItemData{BookTitle: &blank}, "bookTitle"},
		{"conferencePaper", client.ItemData{ProceedingsTitle: &blank}, "proceedingsTitle"},
		// A book has a title but no venue — it IS the volume.
		{"book", client.ItemData{Title: &blank}, ""},
		{"empty", client.ItemData{}, ""},
	} {
		if got := VenueFieldOf(&tc.tmpl); got != tc.want {
			t.Errorf("%s: VenueFieldOf = %q, want %q", tc.name, got, tc.want)
		}
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

func TestVenueField_ResolvesFromTemplate(t *testing.T) {
	t.Parallel()
	h := &itemTemplateHandler{
		body: []byte(`{"itemType":"bookSection","title":"","bookTitle":"","pages":""}`),
	}
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
	h := &itemTemplateHandler{body: []byte(`{"itemType":"book","title":"","publisher":""}`)}
	c, _ := newTestClient(t, h)

	got, err := c.VenueField(context.Background(), "book")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("VenueField = %q, want empty", got)
	}
}

func TestVenueField_CachesPerItemType(t *testing.T) {
	t.Parallel()
	h := &countingTemplateHandler{
		itemTemplateHandler: itemTemplateHandler{
			body: []byte(`{"itemType":"conferencePaper","title":"","proceedingsTitle":""}`),
		},
	}
	c, _ := newTestClient(t, h)

	for range 3 {
		if _, err := c.VenueField(context.Background(), "conferencePaper"); err != nil {
			t.Fatal(err)
		}
	}
	// /items/new is a static, unauthenticated schema endpoint — refetching
	// it once per item on a batch update is pure waste.
	if n := h.calls(); n != 1 {
		t.Errorf("template fetched %d times, want 1 (cached)", n)
	}
}

func TestVenueField_TransportErrorPropagates(t *testing.T) {
	t.Parallel()
	h := &itemTemplateHandler{status: http.StatusInternalServerError}
	c, _ := newTestClient(t, h)

	// A failed lookup must not launder into "this type has no venue
	// field" — that would silently drop the value the user passed.
	if _, err := c.VenueField(context.Background(), "journalArticle"); err == nil {
		t.Fatal("want an error when the template lookup fails")
	}
}
