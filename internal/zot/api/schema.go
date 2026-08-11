package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/client"
)

// Zotero's schema is per item type, and the CLI has to respect it or the
// write simply fails: a bookSection has no publicationTitle, a book has no
// bookTitle, and a journalArticle has no bookAuthor. None of that is
// hardcoded here — these two endpoints declare it, so an item type gaining
// a field needs no code change.
//
// Both are root, unauthenticated, and static, so results are cached for the
// life of the Client. That matters because `item update` resolves per key:
// refetching the same schema once per key of a 50-item batch is pure waste.

// ItemTypeFields returns the field names an item type accepts, in Zotero's
// own order (which is the order its UI shows them, so it is the right order
// for an error message too).
func (c *Client) ItemTypeFields(ctx context.Context, itemType string) ([]string, error) {
	return c.cachedSchema(ctx, "fields:"+itemType, "field", func(ctx context.Context) (*[]client.SchemaLabelled, string, int, error) {
		r, err := c.Gen.GetItemTypeFieldsWithResponse(ctx, &client.GetItemTypeFieldsParams{ItemType: itemType})
		if err != nil {
			return nil, "", 0, err
		}
		return r.JSON200, r.Status(), r.StatusCode(), nil
	})
}

// ItemTypes returns every item type Zotero declares, in the API's own
// order.
//
// This is the authority `item update --type` validates against, for the
// same reason ItemTypeFields is the authority for --field: a hardcoded list
// goes stale the moment Zotero adds a type, and the cost of guessing is a
// 400 after the round trip — which on a multi-key update lands after some
// items have already been patched.
func (c *Client) ItemTypes(ctx context.Context) ([]string, error) {
	return c.cachedSchema(ctx, "itemTypes", "itemType", func(ctx context.Context) (*[]client.SchemaLabelled, string, int, error) {
		r, err := c.Gen.GetItemTypesWithResponse(ctx, &client.GetItemTypesParams{})
		if err != nil {
			return nil, "", 0, err
		}
		return r.JSON200, r.Status(), r.StatusCode(), nil
	})
}

// ItemTypeCreatorTypes returns the creator types an item type accepts — a
// bookSection takes editor and bookAuthor, a journalArticle takes neither.
func (c *Client) ItemTypeCreatorTypes(ctx context.Context, itemType string) ([]string, error) {
	return c.cachedSchema(ctx, "creators:"+itemType, "creatorType", func(ctx context.Context) (*[]client.SchemaLabelled, string, int, error) {
		r, err := c.Gen.GetItemTypeCreatorTypesWithResponse(ctx, &client.GetItemTypeCreatorTypesParams{ItemType: itemType})
		if err != nil {
			return nil, "", 0, err
		}
		return r.JSON200, r.Status(), r.StatusCode(), nil
	})
}

// cachedSchema runs one of the /itemType* lookups and memoizes its result.
//
// The rows come back as {"<key>": "...", "localized": "..."} and the
// generated SchemaLabelled models only `localized`, so the name itself
// lives in AdditionalProperties under a per-endpoint key.
func (c *Client) cachedSchema(
	ctx context.Context,
	cacheKey, nameKey string,
	fetch func(context.Context) (*[]client.SchemaLabelled, string, int, error),
) ([]string, error) {
	if v, ok := c.schemaCache.Load(cacheKey); ok {
		return v.([]string), nil
	}
	rows, statusLine, status, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /%s: %s", cacheKey, statusLine)
	}
	if rows == nil {
		return nil, fmt.Errorf("GET /%s: empty body", cacheKey)
	}
	names := lo.FilterMap(*rows, func(r client.SchemaLabelled, _ int) (string, bool) {
		v, ok := r.Get(nameKey)
		s, isStr := v.(string)
		return s, ok && isStr && s != ""
	})
	c.schemaCache.Store(cacheKey, names)
	return names, nil
}

// ItemTemplate fetches a blank `ItemData` skeleton for the given item type.
// Unlocks "I want to create a book but don't know which fields it takes" —
// the server returns a ready-to-submit structure prefilled with Zotero's
// default values. Caller mutates the template and POSTs it back via
// `CreateItem`.
//
// linkMode is required when itemType=="attachment" (valid values:
// "imported_file", "imported_url", "linked_file", "linked_url"). Pass "" for
// any other item type — the server ignores it.
//
// No library scope: `/items/new` is a root, unauthenticated endpoint in the
// Zotero API. The request still carries our API key (no cost to do so,
// simplifies the client), but the server doesn't require it here.
func (c *Client) ItemTemplate(ctx context.Context, itemType, linkMode string) (*client.ItemData, error) {
	params := &client.GetItemTemplateParams{ItemType: itemType}
	if linkMode != "" {
		lm := client.LinkMode(linkMode)
		params.LinkMode = &lm
	}
	r, err := c.Gen.GetItemTemplateWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	if r.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("GET /items/new?itemType=%s: %s", itemType, r.Status())
	}
	if r.JSON200 == nil {
		return nil, fmt.Errorf("GET /items/new?itemType=%s: empty body", itemType)
	}
	return r.JSON200, nil
}
