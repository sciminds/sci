package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/samber/lo"
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
// versionedDelete fetches the current version and runs a delete with one
// 412-retry. Used by TrashItem and DeleteCollection — the two operations
// whose scaffolding is structurally identical.
func versionedDelete(getVersion func() (int, error), apply func(ver int) error) error {
	current, err := getVersion()
	if err != nil {
		return err
	}
	return withVersionRetry(apply, getVersion, current)
}

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
	status, statusLine, respBody, err := c.createOrUpdateItems(ctx, body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("POST /items: %s: %s", statusLine, string(respBody))
	}
	mor, err := decodeMultiObject(respBody)
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

// getItemRaw fetches an item by key and returns its parsed form + version.
// Used internally for 412 version-retry and to fill in ItemType on patches.
func (c *Client) getItemRaw(ctx context.Context, key string) (*client.Item, error) {
	var status int
	var statusLine string
	var json200 *client.Item
	if c.isShared() {
		r, err := c.Gen.GetItemGroupWithResponse(ctx, c.GroupID(), client.ItemKeyPath(key), nil)
		if err != nil {
			return nil, err
		}
		status, statusLine, json200 = r.StatusCode(), r.Status(), r.JSON200
	} else {
		r, err := c.Gen.GetItemWithResponse(ctx, c.UserID, client.ItemKeyPath(key), nil)
		if err != nil {
			return nil, err
		}
		status, statusLine, json200 = r.StatusCode(), r.Status(), r.JSON200
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("item %s not found", key)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /items/%s: %s", key, statusLine)
	}
	if json200 == nil {
		return nil, fmt.Errorf("GET /items/%s: empty body", key)
	}
	return json200, nil
}

// UpdateItem patches a single item by key. The patch should contain only
// fields you want to change. If patch.ItemType is empty, it is filled in
// from the current item state (avoiding an extra GET by the caller).
// Handles 412 by fetching the latest version and retrying the patch once.
func (c *Client) UpdateItem(ctx context.Context, key string, patch client.ItemData) error {
	getVersion := func() (int, error) {
		it, err := c.getItemRaw(ctx, key)
		if err != nil {
			return 0, err
		}
		return it.Version, nil
	}

	// Initial fetch: we always need a starting version — PATCH requires
	// it in the body. Also fill in ItemType if the caller didn't supply
	// it, so callers don't need a separate GET.
	cur, err := c.getItemRaw(ctx, key)
	if err != nil {
		return err
	}
	current := cur.Version
	if patch.ItemType == "" {
		patch.ItemType = cur.Data.ItemType
	}

	apply := func(ver int) error {
		k := key
		patch.Key = &k
		patch.Version = &ver
		status, statusLine, body, err := c.updateItem(ctx, key, patch)
		if err != nil {
			return err
		}
		switch status {
		case http.StatusNoContent, http.StatusOK:
			return nil
		case http.StatusPreconditionFailed:
			return &VersionConflictError{Path: "/items/" + key}
		default:
			return fmt.Errorf("PATCH /items/%s: %s: %s", key, statusLine, string(body))
		}
	}

	return withVersionRetry(apply, getVersion, current)
}

// TrashItem moves a single item to the library trash via DELETE /items/{key}.
// Handles 412 by refreshing the version once.
//
//nolint:dupl // 412-retry scaffolding is per-operation by design (see CLAUDE.md)
func (c *Client) TrashItem(ctx context.Context, key string) error {
	return versionedDelete(
		func() (int, error) {
			it, err := c.getItemRaw(ctx, key)
			if err != nil {
				return 0, err
			}
			return it.Version, nil
		},
		func(ver int) error {
			status, statusLine, err := c.deleteItem(ctx, key, ver)
			if err != nil {
				return err
			}
			switch status {
			case http.StatusNoContent:
				return nil
			case http.StatusPreconditionFailed:
				return &VersionConflictError{Path: "/items/" + key}
			default:
				return fmt.Errorf("DELETE /items/%s: %s", key, statusLine)
			}
		},
	)
}

// AddItemToCollection appends collKey to the item's Collections array.
// No-op if the collection is already present.
func (c *Client) AddItemToCollection(ctx context.Context, itemKey, collKey string) error {
	it, err := c.getItemRaw(ctx, itemKey)
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
	updated := append(slices.Clone(current), collKey)
	return c.UpdateItem(ctx, itemKey, client.ItemData{
		ItemType:    it.Data.ItemType,
		Collections: &updated,
	})
}

// RemoveItemFromCollection removes collKey from the item's Collections array.
func (c *Client) RemoveItemFromCollection(ctx context.Context, itemKey, collKey string) error {
	it, err := c.getItemRaw(ctx, itemKey)
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
	it, err := c.getItemRaw(ctx, itemKey)
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
	updated := append(slices.Clone(current), client.Tag{Tag: tag})
	return c.UpdateItem(ctx, itemKey, client.ItemData{
		ItemType: it.Data.ItemType,
		Tags:     &updated,
	})
}

// RemoveTagFromItem removes a tag from a single item.
func (c *Client) RemoveTagFromItem(ctx context.Context, itemKey, tag string) error {
	it, err := c.getItemRaw(ctx, itemKey)
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
// Data holds the fields to change.
//
// When Version and ItemType are both non-zero/non-empty, UpdateItemsBatch
// skips the per-item GET that normally fetches them from the API. This is
// the fast path for callers that already have fresh metadata from the local
// Zotero sqlite. If either is missing, the slow path (one GET per item)
// is used as before.
type ItemPatch struct {
	Key      string
	Version  int    // optional: skip GET if set together with ItemType
	ItemType string // optional: skip GET if set together with Version
	Data     client.ItemData
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

	// Build initial payloads. When a patch carries Version + ItemType
	// (typically from the local sqlite), skip the per-item GET entirely.
	// This is the difference between 5000 HTTP calls and zero for a
	// full-library citekey fix.
	type built struct {
		patch ItemPatch
		body  client.ItemData
	}
	initial := make([]built, 0, len(patches))
	for _, p := range patches {
		body := p.Data
		k := p.Key
		body.Key = &k

		if p.Version > 0 && p.ItemType != "" {
			// Fast path: caller supplied metadata from the local DB.
			v := p.Version
			body.Version = &v
			body.ItemType = client.ItemDataItemType(p.ItemType)
		} else {
			// Slow path: fetch version + itemType from the API.
			cur, err := c.getItemRaw(ctx, p.Key)
			if err != nil {
				results[p.Key] = err
				continue
			}
			v := cur.Version
			body.Version = &v
			body.ItemType = cur.Data.ItemType
		}
		initial = append(initial, built{patch: p, body: body})
	}

	// submit POSTs `group` in batches of maxBatchItems. Per-item outcomes are
	// written into `results` (success → nil, failure → error). Returns the
	// subset of entries that failed with a 412 version conflict so the caller
	// can refresh + retry them once.
	submit := func(group []built) ([]built, error) {
		var retryable []built
		for _, slice := range lo.Chunk(group, maxBatchItems) {
			bodies := lo.Map(slice, func(b built, _ int) client.ItemData {
				return b.body
			})
			status, statusLine, respBody, err := c.createOrUpdateItems(ctx, bodies)
			if err != nil {
				return nil, err
			}
			if status != http.StatusOK {
				return nil, fmt.Errorf("POST /items: %s: %s", statusLine, string(respBody))
			}
			mor, err := decodeMultiObject(respBody)
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
			cur, gerr := c.getItemRaw(ctx, b.patch.Key)
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
