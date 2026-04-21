package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/samber/lo"
)

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
