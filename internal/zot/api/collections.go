package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sciminds/cli/internal/zot/client"
)

// CreateCollection creates a new collection under an optional parent.
// parentKey may be "" for a top-level collection. Returns the full
// created collection as the server materialized it — the response's
// successful[0] slot carries the complete Collection JSON, so callers
// don't need a follow-up GET to hydrate.
func (c *Client) CreateCollection(ctx context.Context, name, parentKey string) (*client.Collection, error) {
	data := client.CollectionData{Name: name}
	if parentKey != "" {
		// ParentCollection is a oneof(string, false) union. Wrap the string.
		raw, err := json.Marshal(parentKey)
		if err != nil {
			return nil, err
		}
		data.ParentCollection = &client.CollectionData_ParentCollection{}
		if err := data.ParentCollection.UnmarshalJSON(raw); err != nil {
			return nil, fmt.Errorf("marshal parent collection: %w", err)
		}
	}

	body := []client.CollectionData{data}
	status, statusLine, respBody, err := c.createOrUpdateCollections(ctx, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("POST /collections: %s: %s", statusLine, string(respBody))
	}
	mor, err := decodeMultiObject(respBody)
	if err != nil {
		return nil, err
	}
	if len(mor.Failed) > 0 {
		return nil, multiObjectFailure(mor)
	}
	obj, ok := mor.Successful["0"]
	if !ok {
		return nil, fmt.Errorf("POST /collections: no successful result")
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("re-encode successful collection: %w", err)
	}
	var coll client.Collection
	if err := json.Unmarshal(raw, &coll); err != nil {
		return nil, fmt.Errorf("parse successful collection: %w", err)
	}
	if coll.Key == "" {
		return nil, fmt.Errorf("POST /collections: successful object has no key")
	}
	return &coll, nil
}

// getCollectionRaw fetches a collection by key. Used internally for
// 412 version-retry in DeleteCollection.
func (c *Client) getCollectionRaw(ctx context.Context, key string) (*client.Collection, error) {
	status, statusLine, json200, err := c.getCollection(ctx, key)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("collection %s not found", key)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /collections/%s: %s", key, statusLine)
	}
	if json200 == nil {
		return nil, fmt.Errorf("GET /collections/%s: empty body", key)
	}
	return json200, nil
}

// DeleteCollection deletes a collection. Sub-collections become top-level;
// items remain in the library. Handles 412 with one retry.
//
//nolint:dupl // 412-retry scaffolding is per-operation by design (see CLAUDE.md)
func (c *Client) DeleteCollection(ctx context.Context, key string) error {
	return versionedDelete(
		func() (int, error) {
			coll, err := c.getCollectionRaw(ctx, key)
			if err != nil {
				return 0, err
			}
			return coll.Version, nil
		},
		func(ver int) error {
			status, statusLine, err := c.deleteCollection(ctx, key, ver)
			if err != nil {
				return err
			}
			switch status {
			case http.StatusNoContent:
				return nil
			case http.StatusPreconditionFailed:
				return &VersionConflictError{Path: "/collections/" + key}
			default:
				return fmt.Errorf("DELETE /collections/%s: %s", key, statusLine)
			}
		},
	)
}
