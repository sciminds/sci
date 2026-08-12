package api

import (
	"context"
	"fmt"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot/client"
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
// /itemTypeFields declares it, so VenueFieldIn reads the answer off the
// schema rather than restating something that lives on the server.

// venueFields are the venue fields in priority order. Only one can ever be
// present on a given item type, so the order is a formality — but it makes
// the choice deterministic if Zotero ever ships a type carrying two.
var venueFields = []string{"publicationTitle", "bookTitle", "proceedingsTitle"}

// VenueFieldIn reports which venue field a type's field list contains, or
// "" when the type has no venue at all (a book is the volume; a thesis and
// a preprint have no container).
func VenueFieldIn(fields []string) string {
	found, _ := lo.Find(venueFields, func(v string) bool { return slices.Contains(fields, v) })
	return found
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

// SetField writes an arbitrary Zotero field by name, for the `--field
// key=value` escape hatch. The caller is responsible for having validated
// name against ItemTypeFields — this writes what it is told.
//
// It goes through the generated Set rather than a per-field table because
// the point of the escape hatch is reaching fields nothing here enumerates:
// place, edition, conferenceName, numPages, seriesNumber, and whatever
// Zotero adds next. ItemData.MarshalJSON applies AdditionalProperties LAST,
// so this wins over a typed member of the same name — which is the right
// precedence for an explicit flag.
func SetField(data *client.ItemData, name, value string) {
	data.Set(name, value)
}

// VenueField returns the venue field itemType declares.
//
// An empty return is an ANSWER — the type has no venue field — and callers
// must turn it into a usage error naming the type. An error is a failed
// lookup, and must never be flattened into the empty case: that would drop
// a value the user explicitly passed.
func (c *Client) VenueField(ctx context.Context, itemType string) (string, error) {
	fields, err := c.ItemTypeFields(ctx, itemType)
	if err != nil {
		return "", fmt.Errorf("look up fields for item type %q: %w", itemType, err)
	}
	return VenueFieldIn(fields), nil
}
