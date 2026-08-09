package api

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/zot/client"
)

// Zotero has no single "venue" field. A journalArticle carries its venue in
// publicationTitle, a bookSection in bookTitle, a conferencePaper in
// proceedingsTitle — and writing the wrong one is not a silent no-op, it
// fails the whole request with "'publicationTitle' is not a valid field for
// type 'bookSection'". These three are the venue fields client.ItemData
// models; the wider family (websiteTitle, blogTitle, seriesTitle) needs the
// AdditionalProperties escape hatch and is not reachable through a typed
// field yet.
//
// Which types accept which is not hardcoded here on purpose: Zotero's
// /items/new template declares it, so VenueFieldOf reads the answer off the
// template rather than restating a schema that lives on the server.

// VenueFieldOf reports which venue field an item-type template declares, or
// "" when the type has no venue at all (a book is the volume; a thesis and
// a preprint have no container).
//
// It reads presence, not value: the template returns every field the type
// accepts, blank, so a non-nil pointer to "" means "this type has this
// field".
func VenueFieldOf(tmpl *client.ItemData) string {
	switch {
	case tmpl.PublicationTitle != nil:
		return "publicationTitle"
	case tmpl.BookTitle != nil:
		return "bookTitle"
	case tmpl.ProceedingsTitle != nil:
		return "proceedingsTitle"
	default:
		return ""
	}
}

// SetVenueField writes value into the named venue field, clearing the other
// two. The name must be one VenueFieldOf can return — anything else is a
// programming error rather than bad user input, so it errors instead of
// silently picking a field.
//
// Clearing the siblings is what makes this authoritative rather than
// additive. `item add --openalex <journal article> --type bookSection` has
// already had publicationTitle filled in by the OpenAlex mapper under the
// type it guessed; leaving that behind next to a bookTitle sends Zotero a
// field the final type does not accept, and the whole create fails. On a
// PATCH the siblings stay nil rather than empty, so nothing is cleared on
// the server that the caller did not name.
func SetVenueField(data *client.ItemData, field, value string) error {
	var dst **string
	switch field {
	case "publicationTitle":
		dst = &data.PublicationTitle
	case "bookTitle":
		dst = &data.BookTitle
	case "proceedingsTitle":
		dst = &data.ProceedingsTitle
	default:
		return fmt.Errorf("%q is not a venue field client.ItemData models", field)
	}
	data.PublicationTitle, data.BookTitle, data.ProceedingsTitle = nil, nil, nil
	v := value
	*dst = &v
	return nil
}

// VenueField returns the venue field itemType declares, fetching the type's
// template on first use and caching it for the life of the Client.
//
// An empty return is an ANSWER — the type has no venue field — and callers
// must turn it into a usage error naming the type. An error is a failed
// lookup, and must never be flattened into the empty case: that would drop
// a value the user explicitly passed.
//
// The cache matters because `item update` resolves per item, and /items/new
// is a static unauthenticated schema endpoint — refetching it once per key
// of a 50-item batch is pure waste.
func (c *Client) VenueField(ctx context.Context, itemType string) (string, error) {
	if v, ok := c.venueCache.Load(itemType); ok {
		return v.(string), nil
	}
	tmpl, err := c.ItemTemplate(ctx, itemType, "")
	if err != nil {
		return "", fmt.Errorf("look up fields for item type %q: %w", itemType, err)
	}
	field := VenueFieldOf(tmpl)
	c.venueCache.Store(itemType, field)
	return field, nil
}
