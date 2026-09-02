package api

import (
	"context"
	"maps"
)

// collectionPath is the single-collection endpoint in this client's library.
func (c *Client) collectionPath(key string) string {
	return c.libraryPrefix() + "/collections/" + key
}

// GetCollection fetches one collection. A 404 is [ErrNotFound].
func (c *Client) GetCollection(ctx context.Context, key string) (*Object, error) {
	return c.getObject(ctx, c.collectionPath(key))
}

// CreateCollection creates a collection named name. parentKey nests it
// under that collection; empty makes it top-level, which Zotero spells as
// the literal false rather than an absent field.
func (c *Client) CreateCollection(ctx context.Context, name, parentKey string) (*Object, error) {
	return c.createObject(ctx, c.libraryPrefix()+"/collections", map[string]any{
		"name":             name,
		"parentCollection": parentCollectionValue(parentKey),
	})
}

// PatchCollection applies a partial update to a collection — "name", and
// "parentCollection" as a key string or the literal false for top-level.
// Versioned like PatchItem: one re-read and retry on 412, then a
// [VersionConflictError].
func (c *Client) PatchCollection(ctx context.Context, key string, patch map[string]any) error {
	_, err := c.patchDerived(ctx, c.collectionPath(key), func(_ *Object) (map[string]any, bool) {
		return maps.Clone(patch), true
	})
	return err
}

// DeleteCollection deletes a collection (its items stay in the library).
// Versioned like TrashItem.
func (c *Client) DeleteCollection(ctx context.Context, key string) error {
	return c.deleteVersioned(ctx, c.collectionPath(key))
}

// parentCollectionValue is how Zotero encodes a parent: the key, or false
// for none.
func parentCollectionValue(parentKey string) any {
	if parentKey == "" {
		return false
	}
	return parentKey
}
