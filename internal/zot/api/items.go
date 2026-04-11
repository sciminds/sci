package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sciminds/cli/internal/zot/client"
)

// withVersionRetry runs fn once, and if it returns a 412 Precondition Failed
// error wrapped as *VersionConflictError, fetches the latest version via
// getVersion and retries fn once. Any other error is returned as-is.
//
// This handles the "object version has advanced since we read it" case. The
// middleware in retry.go intentionally does NOT handle 412 because recovering
// from a version conflict requires re-building the request payload with the
// new version — that's per-operation knowledge.
func withVersionRetry(fn func(version int) error, getVersion func() (int, error), initial int) error {
	err := fn(initial)
	if err == nil {
		return nil
	}
	if _, ok := err.(*VersionConflictError); !ok {
		return err
	}
	fresh, gerr := getVersion()
	if gerr != nil {
		return fmt.Errorf("refresh version after 412: %w", gerr)
	}
	return fn(fresh)
}

// VersionConflictError indicates a 412 Precondition Failed response.
type VersionConflictError struct {
	Path string
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict on %s (412 Precondition Failed)", e.Path)
}

// CreateItem creates a single item. data.Key and data.Version MUST be nil.
// Returns the server-assigned key.
func (c *Client) CreateItem(ctx context.Context, data client.ItemData) (string, error) {
	body := []client.ItemData{data}
	resp, err := c.Gen.CreateOrUpdateItemsWithResponse(ctx, c.UserID, &client.CreateOrUpdateItemsParams{}, body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("POST /items: %s: %s", resp.Status(), string(resp.Body))
	}
	mor, err := decodeMultiObject(resp.Body)
	if err != nil {
		return "", err
	}
	if len(mor.Failed) > 0 {
		return "", multiObjectFailure(mor)
	}
	// Successful is keyed by submission index ("0") → object with "key".
	obj, ok := mor.Successful["0"]
	if !ok {
		return "", fmt.Errorf("POST /items: no successful result")
	}
	k, _ := obj["key"].(string)
	if k == "" {
		return "", fmt.Errorf("POST /items: successful object has no key")
	}
	return k, nil
}

// GetItemRaw fetches an item by key and returns its parsed form + version.
func (c *Client) GetItemRaw(ctx context.Context, key string) (*client.Item, error) {
	resp, err := c.Gen.GetItemWithResponse(ctx, c.UserID, client.ItemKeyPath(key), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("item %s not found", key)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("GET /items/%s: %s", key, resp.Status())
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("GET /items/%s: empty body", key)
	}
	return resp.JSON200, nil
}

// UpdateItem patches a single item by key. The patch should contain only
// fields you want to change. Handles 412 by fetching the latest version and
// retrying the patch once.
func (c *Client) UpdateItem(ctx context.Context, key string, patch client.ItemData) error {
	getVersion := func() (int, error) {
		it, err := c.GetItemRaw(ctx, key)
		if err != nil {
			return 0, err
		}
		return it.Version, nil
	}

	// Initial version probe. We always need a starting version — PATCH
	// requires it in the body.
	current, err := getVersion()
	if err != nil {
		return err
	}

	apply := func(ver int) error {
		k := key
		patch.Key = &k
		patch.Version = &ver
		resp, err := c.Gen.UpdateItemWithResponse(ctx, c.UserID, client.ItemKeyPath(key), &client.UpdateItemParams{}, patch)
		if err != nil {
			return err
		}
		switch resp.StatusCode() {
		case http.StatusNoContent, http.StatusOK:
			return nil
		case http.StatusPreconditionFailed:
			return &VersionConflictError{Path: "/items/" + key}
		default:
			return fmt.Errorf("PATCH /items/%s: %s: %s", key, resp.Status(), string(resp.Body))
		}
	}

	return withVersionRetry(apply, getVersion, current)
}

// TrashItem moves a single item to the library trash via DELETE /items/{key}.
// Handles 412 by refreshing the version once.
func (c *Client) TrashItem(ctx context.Context, key string) error {
	getVersion := func() (int, error) {
		it, err := c.GetItemRaw(ctx, key)
		if err != nil {
			return 0, err
		}
		return it.Version, nil
	}
	current, err := getVersion()
	if err != nil {
		return err
	}

	apply := func(ver int) error {
		params := &client.DeleteItemParams{
			IfUnmodifiedSinceVersion: (*client.IfUnmodifiedSinceVersion)(&ver),
		}
		resp, err := c.Gen.DeleteItemWithResponse(ctx, c.UserID, client.ItemKeyPath(key), params)
		if err != nil {
			return err
		}
		switch resp.StatusCode() {
		case http.StatusNoContent:
			return nil
		case http.StatusPreconditionFailed:
			return &VersionConflictError{Path: "/items/" + key}
		default:
			return fmt.Errorf("DELETE /items/%s: %s", key, resp.Status())
		}
	}
	return withVersionRetry(apply, getVersion, current)
}

// AddItemToCollection appends collKey to the item's Collections array.
// No-op if the collection is already present.
func (c *Client) AddItemToCollection(ctx context.Context, itemKey, collKey string) error {
	it, err := c.GetItemRaw(ctx, itemKey)
	if err != nil {
		return err
	}
	var current []string
	if it.Data.Collections != nil {
		current = *it.Data.Collections
	}
	for _, k := range current {
		if k == collKey {
			return nil // already member
		}
	}
	updated := append(append([]string{}, current...), collKey)
	return c.UpdateItem(ctx, itemKey, client.ItemData{
		ItemType:    it.Data.ItemType,
		Collections: &updated,
	})
}

// RemoveItemFromCollection removes collKey from the item's Collections array.
func (c *Client) RemoveItemFromCollection(ctx context.Context, itemKey, collKey string) error {
	it, err := c.GetItemRaw(ctx, itemKey)
	if err != nil {
		return err
	}
	var current []string
	if it.Data.Collections != nil {
		current = *it.Data.Collections
	}
	updated := make([]string, 0, len(current))
	removed := false
	for _, k := range current {
		if k == collKey {
			removed = true
			continue
		}
		updated = append(updated, k)
	}
	if !removed {
		return nil
	}
	return c.UpdateItem(ctx, itemKey, client.ItemData{
		ItemType:    it.Data.ItemType,
		Collections: &updated,
	})
}

// AddTagToItem appends a tag to an item. No-op if already present.
func (c *Client) AddTagToItem(ctx context.Context, itemKey, tag string) error {
	it, err := c.GetItemRaw(ctx, itemKey)
	if err != nil {
		return err
	}
	var current []client.Tag
	if it.Data.Tags != nil {
		current = *it.Data.Tags
	}
	for _, t := range current {
		if t.Tag == tag {
			return nil
		}
	}
	updated := append(append([]client.Tag{}, current...), client.Tag{Tag: tag})
	return c.UpdateItem(ctx, itemKey, client.ItemData{
		ItemType: it.Data.ItemType,
		Tags:     &updated,
	})
}

// RemoveTagFromItem removes a tag from a single item.
func (c *Client) RemoveTagFromItem(ctx context.Context, itemKey, tag string) error {
	it, err := c.GetItemRaw(ctx, itemKey)
	if err != nil {
		return err
	}
	var current []client.Tag
	if it.Data.Tags != nil {
		current = *it.Data.Tags
	}
	updated := make([]client.Tag, 0, len(current))
	removed := false
	for _, t := range current {
		if t.Tag == tag {
			removed = true
			continue
		}
		updated = append(updated, t)
	}
	if !removed {
		return nil
	}
	return c.UpdateItem(ctx, itemKey, client.ItemData{
		ItemType: it.Data.ItemType,
		Tags:     &updated,
	})
}

// ItemPatch describes a single entry in a bulk item update. Key is required;
// Data holds the fields to change. ItemType and Version are filled in
// automatically by UpdateItemsBatch.
type ItemPatch struct {
	Key  string
	Data client.ItemData
}

// maxBatchItems is the Zotero Web API's per-request object cap for
// POST /items. Keep in sync with DeleteTagsFromLibrary's batch cap.
const maxBatchItems = 50

// UpdateItemsBatch applies patches to many items efficiently.
//
// Zotero's POST /items endpoint accepts up to 50 items per request and will
// UPDATE rather than create when each element carries its own Key+Version.
// We fetch the current version + item type for every key (one GET each —
// required for the payload) and then POST in groups of 50.
//
// The return map is keyed by item key: nil means success, non-nil is the
// per-item error. A non-nil second return value indicates a whole-request
// failure (network/HTTP/malformed response) that was not recoverable.
//
// Per-item 412 Precondition Failed conflicts are retried once: a fresh
// version is fetched and the failing items are resubmitted in a second
// batch round. More than one retry round would indicate hot contention
// we'd rather surface.
func (c *Client) UpdateItemsBatch(ctx context.Context, patches []ItemPatch) (map[string]error, error) {
	results := make(map[string]error, len(patches))
	if len(patches) == 0 {
		return results, nil
	}

	// Build initial payloads: fetch version + itemType for each key.
	type built struct {
		patch ItemPatch
		body  client.ItemData
	}
	initial := make([]built, 0, len(patches))
	for _, p := range patches {
		cur, err := c.GetItemRaw(ctx, p.Key)
		if err != nil {
			results[p.Key] = err
			continue
		}
		body := p.Data
		k := p.Key
		v := cur.Version
		body.Key = &k
		body.Version = &v
		body.ItemType = cur.Data.ItemType
		initial = append(initial, built{patch: p, body: body})
	}

	// submit POSTs `group` in batches of maxBatchItems. Per-item outcomes are
	// written into `results` (success → nil, failure → error). Returns the
	// subset of entries that failed with a 412 version conflict so the caller
	// can refresh + retry them once.
	submit := func(group []built) ([]built, error) {
		var retryable []built
		for start := 0; start < len(group); start += maxBatchItems {
			end := start + maxBatchItems
			if end > len(group) {
				end = len(group)
			}
			slice := group[start:end]
			bodies := make([]client.ItemData, len(slice))
			for i, b := range slice {
				bodies[i] = b.body
			}
			resp, err := c.Gen.CreateOrUpdateItemsWithResponse(ctx, c.UserID, &client.CreateOrUpdateItemsParams{}, bodies)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode() != http.StatusOK {
				return nil, fmt.Errorf("POST /items: %s: %s", resp.Status(), string(resp.Body))
			}
			mor, err := decodeMultiObject(resp.Body)
			if err != nil {
				return nil, err
			}
			// Successful + unchanged both mean "no error for this key".
			for idxStr := range mor.Successful {
				if i, ok := batchIndex(idxStr, len(slice)); ok {
					results[slice[i].patch.Key] = nil
				}
			}
			for idxStr := range mor.Unchanged {
				if i, ok := batchIndex(idxStr, len(slice)); ok {
					results[slice[i].patch.Key] = nil
				}
			}
			for idxStr, f := range mor.Failed {
				i, ok := batchIndex(idxStr, len(slice))
				if !ok {
					continue
				}
				msg := ""
				if f.Message != nil {
					msg = *f.Message
				}
				code := 0
				if f.Code != nil {
					code = *f.Code
				}
				if code == http.StatusPreconditionFailed {
					retryable = append(retryable, slice[i])
					continue
				}
				results[slice[i].patch.Key] = fmt.Errorf("batch item %s failed (code %d): %s", slice[i].patch.Key, code, msg)
			}
		}
		return retryable, nil
	}

	retry, err := submit(initial)
	if err != nil {
		return results, err
	}

	// Refresh versions for 412 items and run one more round.
	if len(retry) > 0 {
		refreshed := make([]built, 0, len(retry))
		for _, b := range retry {
			cur, gerr := c.GetItemRaw(ctx, b.patch.Key)
			if gerr != nil {
				results[b.patch.Key] = fmt.Errorf("refresh after 412: %w", gerr)
				continue
			}
			v := cur.Version
			b.body.Version = &v
			b.body.ItemType = cur.Data.ItemType
			refreshed = append(refreshed, b)
		}
		leftover, err := submit(refreshed)
		if err != nil {
			return results, err
		}
		for _, b := range leftover {
			results[b.patch.Key] = &VersionConflictError{Path: "/items/" + b.patch.Key}
		}
	}

	// Any patch whose key is still absent from results means we never managed
	// to submit it (shouldn't happen, but be defensive).
	for _, p := range patches {
		if _, ok := results[p.Key]; !ok {
			results[p.Key] = fmt.Errorf("item %s: no result reported", p.Key)
		}
	}
	return results, nil
}

// batchIndex parses a MultiObjectResult map key (zero-indexed decimal string)
// and bounds-checks it against the submitted slice length.
func batchIndex(s string, n int) (int, bool) {
	i := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		i = i*10 + int(r-'0')
	}
	if i < 0 || i >= n {
		return 0, false
	}
	return i, true
}

// decodeMultiObject unmarshals a POST /items or POST /collections response
// body into a MultiObjectResult.
func decodeMultiObject(body []byte) (*client.MultiObjectResult, error) {
	var mor client.MultiObjectResult
	if err := json.Unmarshal(body, &mor); err != nil {
		return nil, fmt.Errorf("parse MultiObjectResult: %w", err)
	}
	return &mor, nil
}

func multiObjectFailure(mor *client.MultiObjectResult) error {
	for idx, f := range mor.Failed {
		msg := ""
		if f.Message != nil {
			msg = *f.Message
		}
		return fmt.Errorf("batch item %s failed: %s", idx, msg)
	}
	return fmt.Errorf("batch write reported no successes")
}
