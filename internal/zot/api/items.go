package api

import (
	"context"
	"maps"
	"slices"

	"github.com/samber/lo"
)

// itemPath is the single-item endpoint in this client's library.
func (c *Client) itemPath(key string) string {
	return c.libraryPrefix() + "/items/" + key
}

// GetItem fetches one item. A 404 is [ErrNotFound].
func (c *Client) GetItem(ctx context.Context, key string) (*Object, error) {
	return c.getObject(ctx, c.itemPath(key))
}

// ListChildren returns an item's child items — its notes and attachments.
func (c *Client) ListChildren(ctx context.Context, parentKey string) ([]Object, error) {
	return c.listObjects(ctx, c.itemPath(parentKey)+"/children")
}

// CreateItem creates one item from a data payload (which must carry
// itemType) and returns it as the server materialised it — key and version
// assigned. A refused payload is a [WriteFailedError].
func (c *Client) CreateItem(ctx context.Context, data map[string]any) (*Object, error) {
	return c.createObject(ctx, c.libraryPrefix()+"/items", data)
}

// PatchItem applies a partial update to an item. Zotero's item PATCH wants
// itemType in the body, so it is filled in from the server copy when the
// patch does not carry one. The write is versioned: a 412 triggers one
// re-read and retry, a second one is a [VersionConflictError].
//
// Array-valued fields (collections, tags, relations) are replaced whole by
// Zotero, never merged — use AddTag/AddToCollection and their Remove
// counterparts to edit membership safely.
func (c *Client) PatchItem(ctx context.Context, key string, patch map[string]any) error {
	_, err := c.patchDerived(ctx, c.itemPath(key), func(cur *Object) (map[string]any, bool) {
		full := maps.Clone(patch)
		if _, ok := full["itemType"]; !ok {
			full["itemType"] = cur.Data["itemType"]
		}
		return full, true
	})
	return err
}

// TrashItem moves an item to the trash. In Zotero the trash is item STATE,
// not a separate endpoint: an item is trashed by setting data.deleted to 1
// through an ordinary versioned PATCH, and the desktop app restores or
// empties it from there. Returns false with no write when the item is
// already in the trash. For the permanent delete, see [Client.DeleteItem].
func (c *Client) TrashItem(ctx context.Context, key string) (changed bool, err error) {
	return c.patchDerived(ctx, c.itemPath(key), func(cur *Object) (map[string]any, bool) {
		if isTrashed(cur) {
			return nil, false
		}
		return c.fieldPatch(cur, "deleted", 1), true
	})
}

// DeleteItem deletes an item permanently. Zotero's DELETE on an item does
// not go through the trash — the item is gone and only its key survives in
// the library's /deleted log — so this is the destructive verb and
// [Client.TrashItem] is the one a user-facing "remove" should reach for.
// Versioned like PatchItem: one re-read and retry on 412.
func (c *Client) DeleteItem(ctx context.Context, key string) error {
	return c.deleteVersioned(ctx, c.itemPath(key))
}

// isTrashed reads data.deleted, which Zotero returns as the number 1 and
// which some payloads spell as a boolean.
func isTrashed(cur *Object) bool {
	switch flag := cur.Data["deleted"].(type) {
	case bool:
		return flag
	case float64:
		return flag != 0
	default:
		return false
	}
}

// AddToCollection adds an item to a collection. The new membership list is
// derived from the server's current copy, so a concurrent edit is never
// overwritten. Returns false with no write when the item is already a
// member.
func (c *Client) AddToCollection(ctx context.Context, itemKey, collKey string) (changed bool, err error) {
	return c.patchDerived(ctx, c.itemPath(itemKey), func(cur *Object) (map[string]any, bool) {
		current := stringSlice(cur.Data["collections"])
		if slices.Contains(current, collKey) {
			return nil, false
		}
		return c.fieldPatch(cur, "collections", append(slices.Clone(current), collKey)), true
	})
}

// RemoveFromCollection removes an item from a collection. Returns false
// with no write when the item is not a member.
func (c *Client) RemoveFromCollection(ctx context.Context, itemKey, collKey string) (changed bool, err error) {
	return c.patchDerived(ctx, c.itemPath(itemKey), func(cur *Object) (map[string]any, bool) {
		current := stringSlice(cur.Data["collections"])
		updated := lo.Reject(current, func(k string, _ int) bool { return k == collKey })
		if len(updated) == len(current) {
			return nil, false
		}
		return c.fieldPatch(cur, "collections", updated), true
	})
}

// AddTag adds a tag to an item. Zotero stores tags as objects
// ({"tag": "x"} or {"tag": "x", "type": 1} for automatic tags); existing
// entries are sent back verbatim and the new one appended as a manual tag.
// Returns false with no write when the tag is already present.
func (c *Client) AddTag(ctx context.Context, itemKey, tag string) (changed bool, err error) {
	return c.patchDerived(ctx, c.itemPath(itemKey), func(cur *Object) (map[string]any, bool) {
		current := anySlice(cur.Data["tags"])
		if lo.ContainsBy(current, func(entry any) bool { return tagName(entry) == tag }) {
			return nil, false
		}
		updated := append(slices.Clone(current), map[string]any{"tag": tag})
		return c.fieldPatch(cur, "tags", updated), true
	})
}

// RemoveTag removes a tag from an item, leaving every other entry as it
// was. Returns false with no write when the tag is not present.
func (c *Client) RemoveTag(ctx context.Context, itemKey, tag string) (changed bool, err error) {
	return c.patchDerived(ctx, c.itemPath(itemKey), func(cur *Object) (map[string]any, bool) {
		current := anySlice(cur.Data["tags"])
		updated := lo.Reject(current, func(entry any, _ int) bool { return tagName(entry) == tag })
		if len(updated) == len(current) {
			return nil, false
		}
		return c.fieldPatch(cur, "tags", updated), true
	})
}

// fieldPatch builds the smallest PATCH body Zotero accepts for one field:
// the new value plus the itemType the endpoint insists on.
func (c *Client) fieldPatch(cur *Object, field string, value any) map[string]any {
	return map[string]any{"itemType": cur.Data["itemType"], field: value}
}

// tagName reads the "tag" of one entry in data.tags. A malformed entry
// (not an object) yields "", which never matches a real tag.
func tagName(entry any) string {
	obj, ok := entry.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := obj["tag"].(string)
	return name
}

// anySlice reads a JSON array field from a generic payload; a missing or
// non-array value is an empty slice.
func anySlice(v any) []any {
	items, _ := v.([]any)
	return items
}

// stringSlice reads a JSON array of strings from a generic payload,
// dropping any non-string entries.
func stringSlice(v any) []string {
	return lo.FilterMap(anySlice(v), func(entry any, _ int) (string, bool) {
		s, ok := entry.(string)
		return s, ok
	})
}
