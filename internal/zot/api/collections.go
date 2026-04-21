package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sciminds/cli/internal/zot/client"
)

// CreateCollection creates a new collection under an optional parent.
// parentKey may be "" for a top-level collection. Returns the assigned key.
func (c *Client) CreateCollection(ctx context.Context, name, parentKey string) (string, error) {
	data := client.CollectionData{Name: name}
	if parentKey != "" {
		// ParentCollection is a oneof(string, false) union. Wrap the string.
		raw, err := json.Marshal(parentKey)
		if err != nil {
			return "", err
		}
		data.ParentCollection = &client.CollectionData_ParentCollection{}
		if err := data.ParentCollection.UnmarshalJSON(raw); err != nil {
			return "", fmt.Errorf("marshal parent collection: %w", err)
		}
	}

	body := []client.CollectionData{data}
	status, statusLine, respBody, err := c.createOrUpdateCollections(ctx, body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("POST /collections: %s: %s", statusLine, string(respBody))
	}
	mor, err := decodeMultiObject(respBody)
	if err != nil {
		return "", err
	}
	if len(mor.Failed) > 0 {
		return "", multiObjectFailure(mor)
	}
	obj, ok := mor.Successful["0"]
	if !ok {
		return "", fmt.Errorf("POST /collections: no successful result")
	}
	k, _ := obj["key"].(string)
	if k == "" {
		return "", fmt.Errorf("POST /collections: successful object has no key")
	}
	return k, nil
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
