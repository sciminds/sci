package api

import (
	"context"
	"net/url"
	"slices"

	"github.com/samber/lo"
)

// itemTypeField is one entry of GET /itemTypeFields: the API field name
// and its display label (which sci does not use).
type itemTypeField struct {
	Field     string `json:"field"`
	Localized string `json:"localized"`
}

// itemTypeCreatorType is one entry of GET /itemTypeCreatorTypes: the
// creator role name and its display label (which sci does not use).
type itemTypeCreatorType struct {
	CreatorType string `json:"creatorType"`
	Localized   string `json:"localized"`
}

// ItemTypeFields returns the field names Zotero accepts for an item type,
// in the API's order — what a caller needs to check a payload before a
// write, or to know which keys of data are real fields. The schema
// endpoint is global (no library prefix) and effectively static, so the
// answer is memoized per client; a second call for the same type makes no
// request. The returned slice is the caller's to keep or edit.
func (c *Client) ItemTypeFields(ctx context.Context, itemType string) ([]string, error) {
	return c.cachedSchemaList(c.fieldsCache, itemType, func() ([]string, error) {
		var entries []itemTypeField
		path := "/itemTypeFields?" + url.Values{"itemType": {itemType}}.Encode()
		if err := c.getJSON(ctx, path, &entries); err != nil {
			return nil, err
		}
		return lo.Map(entries, func(e itemTypeField, _ int) string { return e.Field }), nil
	})
}

// ItemTypeCreatorTypes returns the creator roles Zotero accepts for an item
// type (author, editor, contributor, …), in the API's order — what a
// caller needs to validate a `creators` list before a write. Memoized per
// client like [Client.ItemTypeFields], in its own cache.
func (c *Client) ItemTypeCreatorTypes(ctx context.Context, itemType string) ([]string, error) {
	return c.cachedSchemaList(c.creatorTypesCache, itemType, func() ([]string, error) {
		var entries []itemTypeCreatorType
		path := "/itemTypeCreatorTypes?" + url.Values{"itemType": {itemType}}.Encode()
		if err := c.getJSON(ctx, path, &entries); err != nil {
			return nil, err
		}
		return lo.Map(entries, func(e itemTypeCreatorType, _ int) string { return e.CreatorType }), nil
	})
}

// cachedSchemaList answers a per-item-type schema question from cache,
// fetching it once per client otherwise. Every caller gets its own copy, so
// an edit never reaches the cache.
func (c *Client) cachedSchemaList(cache map[string][]string, itemType string, fetch func() ([]string, error)) ([]string, error) {
	c.mu.Lock()
	cached, ok := cache[itemType]
	c.mu.Unlock()
	if ok {
		return slices.Clone(cached), nil
	}
	names, err := fetch()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	cache[itemType] = names
	c.mu.Unlock()
	return slices.Clone(names), nil
}
