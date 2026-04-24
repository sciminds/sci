package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/client"
)

// GetTag fetches metadata for a single tag by name. Used for post-write
// verification ("did my tag land?") and for `--remote` spot-checks when
// the local SQLite cache may be stale. Normal tag reads belong in
// local.Reader; this is the Web API escape hatch.
//
// The tag name is the raw string — the generated client percent-encodes
// it for transport.
//
//nolint:dupl // single-object GET scaffolding is intrinsically symmetric across tag/item/etc
func (c *Client) GetTag(ctx context.Context, name string) (*client.TagWithMeta, error) {
	var status int
	var statusLine string
	var json200 *client.TagWithMeta
	if c.isShared() {
		r, err := c.Gen.GetTagGroupWithResponse(ctx, c.GroupID(), client.TagNamePath(name), nil)
		if err != nil {
			return nil, err
		}
		status, statusLine, json200 = r.StatusCode(), r.Status(), r.JSON200
	} else {
		r, err := c.Gen.GetTagWithResponse(ctx, c.UserID, client.TagNamePath(name), nil)
		if err != nil {
			return nil, err
		}
		status, statusLine, json200 = r.StatusCode(), r.Status(), r.JSON200
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("tag %q not found", name)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /tags/%s: %s", name, statusLine)
	}
	if json200 == nil {
		return nil, fmt.Errorf("GET /tags/%s: empty body", name)
	}
	return json200, nil
}

// DeleteTagsFromLibrary removes the given tags from ALL items in the library.
// The Zotero API accepts up to 50 tags per request, pipe-separated.
func (c *Client) DeleteTagsFromLibrary(ctx context.Context, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	// Batch into groups of 50 (API limit).
	const batchSize = 50
	for _, chunk := range lo.Chunk(tags, batchSize) {
		joined := strings.Join(chunk, " || ")
		status, statusLine, respBody, err := c.deleteTags(ctx, joined)
		if err != nil {
			return err
		}
		switch status {
		case http.StatusNoContent:
			continue
		case http.StatusPreconditionFailed:
			// Library has changed; the API docs say this can happen for
			// multi-object writes if library version was pinned. We did
			// not pin, so this is effectively "retry whole batch once".
			return fmt.Errorf("DELETE /tags: library has been modified since query — retry")
		default:
			return fmt.Errorf("DELETE /tags: %s: %s", statusLine, string(respBody))
		}
	}
	return nil
}
