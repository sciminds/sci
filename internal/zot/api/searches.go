package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sciminds/cli/internal/zot/client"
)

// CreateSavedSearch creates a new saved search with the supplied conditions.
// Returns the full created search as the server materialized it — the
// response's successful[0] slot already carries the complete Search JSON,
// so callers don't need a follow-up GET to hydrate.
func (c *Client) CreateSavedSearch(ctx context.Context, name string, conditions []client.SearchCondition) (*client.Search, error) {
	if name == "" {
		return nil, fmt.Errorf("saved search name is required")
	}
	if len(conditions) == 0 {
		return nil, fmt.Errorf("saved search needs at least one condition")
	}
	body := []client.SearchData{{Name: name, Conditions: conditions}}
	return c.submitSavedSearch(ctx, body)
}

// UpdateSavedSearch replaces a saved search's name and conditions. Both fields
// are required by the API on every POST (saved-search updates are full
// replacements rather than per-field PATCH). 412 conflicts retry once with the
// fresh version.
func (c *Client) UpdateSavedSearch(ctx context.Context, key, name string, conditions []client.SearchCondition) error {
	if name == "" {
		return fmt.Errorf("saved search name is required")
	}
	if len(conditions) == 0 {
		return fmt.Errorf("saved search needs at least one condition")
	}
	getVersion := func() (int, error) {
		s, err := c.getSearchRaw(ctx, key)
		if err != nil {
			return 0, err
		}
		return s.Version, nil
	}
	cur, err := getVersion()
	if err != nil {
		return err
	}
	apply := func(ver int) error {
		k := key
		v := ver
		body := []client.SearchData{{
			Key:        &k,
			Version:    &v,
			Name:       name,
			Conditions: conditions,
		}}
		_, err := c.submitSavedSearch(ctx, body)
		if vc, ok := err.(*VersionConflictError); ok {
			return vc
		}
		return err
	}
	return withVersionRetry(apply, getVersion, cur)
}

// submitSavedSearch posts a single-element saved-search batch and parses
// the successful[0] slot. Shared between Create and Update so the
// MultiObjectResult handling stays in one place.
func (c *Client) submitSavedSearch(ctx context.Context, body []client.SearchData) (*client.Search, error) {
	status, statusLine, respBody, err := c.createOrUpdateSearches(ctx, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("POST /searches: %s: %s", statusLine, string(respBody))
	}
	mor, err := decodeMultiObject(respBody)
	if err != nil {
		return nil, err
	}
	if len(mor.Failed) > 0 {
		// Surface 412 as VersionConflictError so withVersionRetry can recover.
		for _, f := range mor.Failed {
			if f.Code != nil && *f.Code == http.StatusPreconditionFailed {
				return nil, &VersionConflictError{Path: "/searches"}
			}
		}
		return nil, multiObjectFailure(mor)
	}
	obj, ok := mor.Successful["0"]
	if !ok {
		// Unchanged updates (no-op) report into Unchanged with the existing
		// key; treat that as success but without a hydrated payload.
		if _, isUnchanged := mor.Unchanged["0"]; isUnchanged {
			return nil, nil
		}
		return nil, fmt.Errorf("POST /searches: no successful result")
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("re-encode successful saved search: %w", err)
	}
	var s client.Search
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse successful saved search: %w", err)
	}
	if s.Key == "" {
		return nil, fmt.Errorf("POST /searches: successful object has no key")
	}
	return &s, nil
}

// GetSavedSearch fetches one saved search by key from the Web API. Used by
// `saved-search show` and as the version source for update/delete retries.
func (c *Client) GetSavedSearch(ctx context.Context, key string) (*client.Search, error) {
	return c.getSearchRaw(ctx, key)
}

// getSearchRaw fetches a saved search by key. Internal (412 version-retry +
// public hydration share the same code path).
func (c *Client) getSearchRaw(ctx context.Context, key string) (*client.Search, error) {
	status, statusLine, json200, err := c.getSearch(ctx, key)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("saved search %s not found", key)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /searches/%s: %s", key, statusLine)
	}
	if json200 == nil {
		return nil, fmt.Errorf("GET /searches/%s: empty body", key)
	}
	return json200, nil
}

// ListSavedSearches fetches every saved search in the target library,
// paginating at the Web API's 100-per-page cap. Returns the raw client.Search
// slice; callers convert to a CLI-facing shape themselves.
//
// Agent use case: immediately after CreateSavedSearch the local SQLite
// hasn't synced yet, so a local-DB list would miss what was just created.
// This method sidesteps that.
func (c *Client) ListSavedSearches(ctx context.Context) ([]client.Search, error) {
	var all []client.Search
	start := 0
	for {
		status, statusLine, page, err := c.listSearches(ctx, start, defaultListPageSize)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("GET /searches: %s", statusLine)
		}
		if page == nil || len(*page) == 0 {
			break
		}
		all = append(all, *page...)
		if len(*page) < defaultListPageSize {
			break
		}
		start += len(*page)
	}
	return all, nil
}

// DeleteSavedSearch deletes one saved search. Items the search matched are
// untouched — saved searches don't own items. Handles 412 with one retry.
//
//nolint:dupl // 412-retry scaffolding is per-operation by design (see CLAUDE.md)
func (c *Client) DeleteSavedSearch(ctx context.Context, key string) error {
	return versionedDelete(
		func() (int, error) {
			s, err := c.getSearchRaw(ctx, key)
			if err != nil {
				return 0, err
			}
			return s.Version, nil
		},
		func(ver int) error {
			status, statusLine, err := c.deleteSearch(ctx, key, ver)
			if err != nil {
				return err
			}
			switch status {
			case http.StatusNoContent:
				return nil
			case http.StatusPreconditionFailed:
				return &VersionConflictError{Path: "/searches/" + key}
			default:
				return fmt.Errorf("DELETE /searches/%s: %s", key, statusLine)
			}
		},
	)
}
