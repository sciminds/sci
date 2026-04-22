package api

// Per-op scope dispatch. Each helper picks the generated user- or
// group-variant method based on c.Lib.Scope and returns the uniform
// (status, statusLine, body, error) tuple the callers consume.
//
// The generated Group methods return distinct response struct types
// (CreateOrUpdateItemsResponse vs CreateOrUpdateItemsGroupResponse) but
// the JSON body schemas are identical — the dispatch helpers unwrap
// into the common types defined in internal/zot/client.

import (
	"context"

	"github.com/sciminds/cli/internal/zot/client"
)

// createOrUpdateItems dispatches POST /items (personal) or POST /groups/.../items.
// Returns (HTTP status, status-line, response body, transport error).
//
//nolint:dupl // per-op dispatch is intrinsically symmetric across user/group generated types
func (c *Client) createOrUpdateItems(ctx context.Context, body []client.ItemData) (int, string, []byte, error) {
	if c.isShared() {
		r, err := c.Gen.CreateOrUpdateItemsGroupWithResponse(ctx, c.GroupID(), &client.CreateOrUpdateItemsGroupParams{}, body)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.Body, nil
	}
	r, err := c.Gen.CreateOrUpdateItemsWithResponse(ctx, c.UserID, &client.CreateOrUpdateItemsParams{}, body)
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.Body, nil
}

// updateItem dispatches PATCH /items/{key} (personal) or the group variant.
func (c *Client) updateItem(ctx context.Context, key string, patch client.ItemData) (int, string, []byte, error) {
	if c.isShared() {
		r, err := c.Gen.UpdateItemGroupWithResponse(ctx, c.GroupID(), client.ItemKeyPath(key), &client.UpdateItemGroupParams{}, patch)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.Body, nil
	}
	r, err := c.Gen.UpdateItemWithResponse(ctx, c.UserID, client.ItemKeyPath(key), &client.UpdateItemParams{}, patch)
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.Body, nil
}

// deleteItem dispatches DELETE /items/{key} (personal) or the group variant.
// ver is the If-Unmodified-Since-Version value.
//
//nolint:dupl // per-op dispatch is intrinsically symmetric across user/group generated types
func (c *Client) deleteItem(ctx context.Context, key string, ver int) (int, string, error) {
	ifVer := (*client.IfUnmodifiedSinceVersion)(&ver)
	if c.isShared() {
		params := &client.DeleteItemGroupParams{IfUnmodifiedSinceVersion: ifVer}
		r, err := c.Gen.DeleteItemGroupWithResponse(ctx, c.GroupID(), client.ItemKeyPath(key), params)
		if err != nil {
			return 0, "", err
		}
		return r.StatusCode(), r.Status(), nil
	}
	params := &client.DeleteItemParams{IfUnmodifiedSinceVersion: ifVer}
	r, err := c.Gen.DeleteItemWithResponse(ctx, c.UserID, client.ItemKeyPath(key), params)
	if err != nil {
		return 0, "", err
	}
	return r.StatusCode(), r.Status(), nil
}

// getItemChildren dispatches GET /items/{key}/children.
func (c *Client) getItemChildren(ctx context.Context, parentKey string) (int, string, []byte, *[]client.Item, error) {
	if c.isShared() {
		r, err := c.Gen.GetItemChildrenGroupWithResponse(ctx, c.GroupID(), client.ItemKeyPath(parentKey), nil)
		if err != nil {
			return 0, "", nil, nil, err
		}
		return r.StatusCode(), r.Status(), r.Body, r.JSON200, nil
	}
	r, err := c.Gen.GetItemChildrenWithResponse(ctx, c.UserID, client.ItemKeyPath(parentKey), nil)
	if err != nil {
		return 0, "", nil, nil, err
	}
	return r.StatusCode(), r.Status(), r.Body, r.JSON200, nil
}

// createOrUpdateCollections dispatches POST /collections.
//
//nolint:dupl // per-op dispatch is intrinsically symmetric across user/group generated types
func (c *Client) createOrUpdateCollections(ctx context.Context, body []client.CollectionData) (int, string, []byte, error) {
	if c.isShared() {
		r, err := c.Gen.CreateOrUpdateCollectionsGroupWithResponse(ctx, c.GroupID(), &client.CreateOrUpdateCollectionsGroupParams{}, body)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.Body, nil
	}
	r, err := c.Gen.CreateOrUpdateCollectionsWithResponse(ctx, c.UserID, &client.CreateOrUpdateCollectionsParams{}, body)
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.Body, nil
}

// listCollections dispatches GET /collections with pagination params.
//
//nolint:dupl // per-op dispatch is intrinsically symmetric across user/group generated types
func (c *Client) listCollections(ctx context.Context, start, limit int) (int, string, *[]client.Collection, error) {
	s := client.Start(start)
	l := client.Limit(limit)
	if c.isShared() {
		params := &client.ListCollectionsGroupParams{Start: &s, Limit: &l}
		r, err := c.Gen.ListCollectionsGroupWithResponse(ctx, c.GroupID(), params)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.JSON200, nil
	}
	params := &client.ListCollectionsParams{Start: &s, Limit: &l}
	r, err := c.Gen.ListCollectionsWithResponse(ctx, c.UserID, params)
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.JSON200, nil
}

// listItems dispatches GET /items (top-level) or GET /collections/{key}/items
// with pagination + filter params. Both endpoints accept the same filter set
// on Zotero's side — itemType (`journalArticle`, `book || bookSection`,
// `-attachment`), q/qmode full-text search, start/limit pagination.
//
//nolint:dupl // per-op dispatch is intrinsically symmetric across user/group generated types
func (c *Client) listItems(ctx context.Context, opts ListItemsOptions) (int, string, *[]client.Item, error) {
	s := client.Start(opts.Start)
	l := client.Limit(opts.Limit)
	if c.isShared() {
		if opts.CollectionKey != "" {
			params := &client.ListCollectionItemsGroupParams{Start: &s, Limit: &l}
			if opts.ItemType != "" {
				t := client.ItemType(opts.ItemType)
				params.ItemType = &t
			}
			if opts.Query != "" {
				q := client.Query(opts.Query)
				params.Q = &q
				if opts.QMode != "" {
					m := client.ListCollectionItemsGroupParamsQmode(opts.QMode)
					params.Qmode = &m
				}
			}
			r, err := c.Gen.ListCollectionItemsGroupWithResponse(ctx, c.GroupID(), client.CollectionKeyPath(opts.CollectionKey), params)
			if err != nil {
				return 0, "", nil, err
			}
			return r.StatusCode(), r.Status(), r.JSON200, nil
		}
		params := &client.ListItemsGroupParams{Start: &s, Limit: &l}
		if opts.ItemType != "" {
			t := client.ItemType(opts.ItemType)
			params.ItemType = &t
		}
		if opts.Query != "" {
			q := client.Query(opts.Query)
			params.Q = &q
			if opts.QMode != "" {
				m := client.ListItemsGroupParamsQmode(opts.QMode)
				params.Qmode = &m
			}
		}
		r, err := c.Gen.ListItemsGroupWithResponse(ctx, c.GroupID(), params)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.JSON200, nil
	}
	if opts.CollectionKey != "" {
		params := &client.ListCollectionItemsParams{Start: &s, Limit: &l}
		if opts.ItemType != "" {
			t := client.ItemType(opts.ItemType)
			params.ItemType = &t
		}
		if opts.Query != "" {
			q := client.Query(opts.Query)
			params.Q = &q
			if opts.QMode != "" {
				m := client.ListCollectionItemsParamsQmode(opts.QMode)
				params.Qmode = &m
			}
		}
		r, err := c.Gen.ListCollectionItemsWithResponse(ctx, c.UserID, client.CollectionKeyPath(opts.CollectionKey), params)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.JSON200, nil
	}
	params := &client.ListItemsParams{Start: &s, Limit: &l}
	if opts.ItemType != "" {
		t := client.ItemType(opts.ItemType)
		params.ItemType = &t
	}
	if opts.Query != "" {
		q := client.Query(opts.Query)
		params.Q = &q
		if opts.QMode != "" {
			m := client.ListItemsParamsQmode(opts.QMode)
			params.Qmode = &m
		}
	}
	r, err := c.Gen.ListItemsWithResponse(ctx, c.UserID, params)
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.JSON200, nil
}

// getCollection dispatches GET /collections/{key}.
func (c *Client) getCollection(ctx context.Context, key string) (int, string, *client.Collection, error) {
	if c.isShared() {
		r, err := c.Gen.GetCollectionGroupWithResponse(ctx, c.GroupID(), client.CollectionKeyPath(key))
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.JSON200, nil
	}
	r, err := c.Gen.GetCollectionWithResponse(ctx, c.UserID, client.CollectionKeyPath(key))
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.JSON200, nil
}

// deleteCollection dispatches DELETE /collections/{key}.
//
//nolint:dupl // per-op dispatch is intrinsically symmetric across user/group generated types
func (c *Client) deleteCollection(ctx context.Context, key string, ver int) (int, string, error) {
	ifVer := (*client.IfUnmodifiedSinceVersion)(&ver)
	if c.isShared() {
		params := &client.DeleteCollectionGroupParams{IfUnmodifiedSinceVersion: ifVer}
		r, err := c.Gen.DeleteCollectionGroupWithResponse(ctx, c.GroupID(), client.CollectionKeyPath(key), params)
		if err != nil {
			return 0, "", err
		}
		return r.StatusCode(), r.Status(), nil
	}
	params := &client.DeleteCollectionParams{IfUnmodifiedSinceVersion: ifVer}
	r, err := c.Gen.DeleteCollectionWithResponse(ctx, c.UserID, client.CollectionKeyPath(key), params)
	if err != nil {
		return 0, "", err
	}
	return r.StatusCode(), r.Status(), nil
}

// deleteTags dispatches DELETE /tags?tag=....
func (c *Client) deleteTags(ctx context.Context, pipeJoined string) (int, string, []byte, error) {
	if c.isShared() {
		params := &client.DeleteTagsGroupParams{Tag: pipeJoined}
		r, err := c.Gen.DeleteTagsGroupWithResponse(ctx, c.GroupID(), params)
		if err != nil {
			return 0, "", nil, err
		}
		return r.StatusCode(), r.Status(), r.Body, nil
	}
	params := &client.DeleteTagsParams{Tag: pipeJoined}
	r, err := c.Gen.DeleteTagsWithResponse(ctx, c.UserID, params)
	if err != nil {
		return 0, "", nil, err
	}
	return r.StatusCode(), r.Status(), r.Body, nil
}
