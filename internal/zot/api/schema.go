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

// ItemTypeFields returns the field names Zotero accepts for an item type,
// in the API's order — what a caller needs to check a payload before a
// write, or to know which keys of data are real fields. The schema
// endpoint is global (no library prefix) and effectively static, so the
// answer is memoized per client; a second call for the same type makes no
// request. The returned slice is the caller's to keep or edit.
func (c *Client) ItemTypeFields(ctx context.Context, itemType string) ([]string, error) {
	c.mu.Lock()
	cached, ok := c.fieldsCache[itemType]
	c.mu.Unlock()
	if ok {
		return slices.Clone(cached), nil
	}

	var entries []itemTypeField
	path := "/itemTypeFields?" + url.Values{"itemType": {itemType}}.Encode()
	if err := c.getJSON(ctx, path, &entries); err != nil {
		return nil, err
	}
	fields := lo.Map(entries, func(e itemTypeField, _ int) string { return e.Field })

	c.mu.Lock()
	c.fieldsCache[itemType] = fields
	c.mu.Unlock()
	return slices.Clone(fields), nil
}
