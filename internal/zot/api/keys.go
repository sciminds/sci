package api

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/zot/client"
)

// CurrentKey fetches metadata about the configured API key, including the
// library owner's user ID, username, and scoped permissions. Useful as a
// round-trip auth sanity check.
func (c *Client) CurrentKey(ctx context.Context) (*client.KeyInfo, error) {
	resp, err := c.Gen.GetCurrentKeyWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("GET /keys/current: %s", resp.Status())
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("GET /keys/current: empty response body")
	}
	return resp.JSON200, nil
}
