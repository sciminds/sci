package api

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot/client"
)

// RelatedPredicate is the RDF predicate Zotero uses for "related items" —
// the link the desktop UI shows in its Related pane. Zotero's own docs name
// connecting a standalone note to the items it discusses as the use case.
//
// The other two predicates a Zotero library carries (owl:sameAs, which
// links a group copy to a personal one, and dc:replaces, for merged items)
// are Zotero-generated. sci reads them but never writes them.
const RelatedPredicate = "dc:relation"

// itemURIRe extracts the item key from a Zotero item URI. The library
// segment is deliberately loose (users/N or groups/N) because a relation
// may point into a different library than the one holding it — that is
// exactly how owl:sameAs links a group copy to its personal twin.
var itemURIRe = regexp.MustCompile(`^https?://zotero\.org/(?:users|groups)/\d+/items/([A-Z0-9]+)$`)

// itemURI builds the relation object URI for an item key. apiPath is a
// [zot.LibraryRef.APIPath] — "users/17450224" or "groups/6506098".
//
// The scheme is http, not https, because that is the literal form Zotero
// writes and compares. The URI is an opaque identifier, never fetched;
// "upgrading" it to https would make sci's relations fail to match the
// ones already in the library.
func itemURI(apiPath, key string) string {
	return fmt.Sprintf("http://zotero.org/%s/items/%s", apiPath, key)
}

// keyFromURI returns the item key inside a Zotero item URI, or "" if the
// string is not one. Callers skip the empty result rather than erroring: a
// library can contain relation objects pointing at things that are not
// items (attachments on the web, arbitrary URIs), and one unrecognized
// entry must not fail a whole listing.
func keyFromURI(uri string) string {
	m := itemURIRe.FindStringSubmatch(uri)
	if m == nil {
		return ""
	}
	return m[1]
}

// decodeRelations flattens Zotero's predicate → (string | []string) union
// into a plain predicate → []string map.
//
// The schema types each value as string-or-array and both forms occur in
// real libraries — a predicate with exactly one object is usually written
// as a bare string — so a decoder that assumes the array form silently
// loses single relations.
func decodeRelations(m map[string]client.ItemData_Relations_AdditionalProperties) map[string][]string {
	out := make(map[string][]string, len(m))
	for pred, v := range m {
		if arr, err := v.AsItemDataRelations1(); err == nil && len(arr) > 0 {
			out[pred] = arr
			continue
		}
		if s, err := v.AsItemDataRelations0(); err == nil && s != "" {
			out[pred] = []string{s}
		}
	}
	return out
}

// encodeRelations is the inverse of [decodeRelations], always emitting the
// array form — it is valid for any arity, so we never have to decide
// whether a one-element predicate should collapse to a scalar.
//
// The returned map is non-nil even when empty: a PATCH that clears the last
// relation must send `relations: {}`, and a nil map would vanish under
// omitempty, leaving the server's copy untouched.
func encodeRelations(m map[string][]string) map[string]client.ItemData_Relations_AdditionalProperties {
	out := make(map[string]client.ItemData_Relations_AdditionalProperties, len(m))
	for pred, uris := range m {
		var v client.ItemData_Relations_AdditionalProperties
		if err := v.FromItemDataRelations1(uris); err != nil {
			continue
		}
		out[pred] = v
	}
	return out
}

// ItemRelations returns the relations stored on one item, as
// predicate → item keys. Objects that are not Zotero item URIs are skipped.
func (c *Client) ItemRelations(ctx context.Context, key string) (map[string][]string, error) {
	it, err := c.getItemRaw(ctx, key)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for pred, uris := range c.relationsOf(it) {
		keys := lo.FilterMap(uris, func(uri string, _ int) (string, bool) {
			k := keyFromURI(uri)
			return k, k != ""
		})
		if len(keys) > 0 {
			out[pred] = keys
		}
	}
	return out, nil
}

// relationsOf decodes an item's relations, tolerating the nil pointer a
// relation-free item carries.
func (c *Client) relationsOf(it *client.Item) map[string][]string {
	if it.Data.Relations == nil {
		return nil
	}
	return decodeRelations(*it.Data.Relations)
}

// LinkItems relates two items with [RelatedPredicate], writing BOTH
// directions.
//
// Reciprocity is the caller's job, not the server's. Zotero's own UI does
// exactly this — relatedBox.js calls addRelatedItem on each item and saves
// each separately — so the Web API stores precisely what it is given.
// Writing one side only produces a link the desktop shows on one item and
// not the other, which reads as data corruption to the user.
//
// Linking is idempotent per side: a side that already carries the relation
// is left alone, so a retry after a partial failure completes the pair
// rather than duplicating the URI.
func (c *Client) LinkItems(ctx context.Context, aKey, bKey string) error {
	if aKey == bKey {
		return fmt.Errorf("cannot relate %s to itself", aKey)
	}
	if err := c.addRelation(ctx, aKey, bKey); err != nil {
		return err
	}
	// The reverse side is what makes the link real in Zotero's UI. If it
	// fails, say so explicitly — a silent half-link is worse than an error,
	// because the forward direction looks like it worked.
	if err := c.addRelation(ctx, bKey, aKey); err != nil {
		return fmt.Errorf("linked %s → %s but failed the reverse (%s → %s), so Zotero will show the relation on only one item: %w",
			aKey, bKey, bKey, aKey, err)
	}
	return nil
}

// UnlinkItems removes the [RelatedPredicate] relation in both directions.
// Like [Client.LinkItems] it is idempotent per side.
func (c *Client) UnlinkItems(ctx context.Context, aKey, bKey string) error {
	if err := c.removeRelation(ctx, aKey, bKey); err != nil {
		return err
	}
	if err := c.removeRelation(ctx, bKey, aKey); err != nil {
		return fmt.Errorf("unlinked %s → %s but failed the reverse (%s → %s), leaving a one-sided relation: %w",
			aKey, bKey, bKey, aKey, err)
	}
	return nil
}

// addRelation adds objKey to subjKey's related items. Returns nil without
// writing when the relation is already present.
func (c *Client) addRelation(ctx context.Context, subjKey, objKey string) error {
	it, err := c.getItemRaw(ctx, subjKey)
	if err != nil {
		return err
	}
	rels := c.relationsOf(it)
	if rels == nil {
		rels = map[string][]string{}
	}
	uri := itemURI(c.Lib.APIPath, objKey)
	if slices.Contains(rels[RelatedPredicate], uri) {
		return nil
	}
	rels[RelatedPredicate] = append(slices.Clone(rels[RelatedPredicate]), uri)
	return c.patchRelations(ctx, subjKey, it, rels)
}

// removeRelation drops objKey from subjKey's related items. Returns nil
// without writing when the relation is not there.
func (c *Client) removeRelation(ctx context.Context, subjKey, objKey string) error {
	it, err := c.getItemRaw(ctx, subjKey)
	if err != nil {
		return err
	}
	rels := c.relationsOf(it)
	uri := itemURI(c.Lib.APIPath, objKey)
	if !slices.Contains(rels[RelatedPredicate], uri) {
		return nil
	}
	kept := lo.Reject(rels[RelatedPredicate], func(u string, _ int) bool { return u == uri })
	if len(kept) == 0 {
		// Drop the predicate entirely rather than leaving an empty array —
		// that is the shape Zotero writes for a relation-free item.
		delete(rels, RelatedPredicate)
	} else {
		rels[RelatedPredicate] = kept
	}
	return c.patchRelations(ctx, subjKey, it, rels)
}

// patchRelations writes the whole relations object back.
//
// A PATCH replaces the field wholesale, so rels must carry every predicate
// the item had — including owl:sameAs and dc:replaces, which sci never
// writes but must not delete. Both callers build rels from the item's own
// decoded relations for exactly that reason.
func (c *Client) patchRelations(ctx context.Context, key string, it *client.Item, rels map[string][]string) error {
	encoded := encodeRelations(rels)
	return c.UpdateItem(ctx, key, client.ItemData{
		ItemType:  it.Data.ItemType,
		Relations: &encoded,
	})
}
