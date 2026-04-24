package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sciminds/cli/internal/zot/client"
)

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
