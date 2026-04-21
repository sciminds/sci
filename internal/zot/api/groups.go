package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sciminds/cli/internal/zot"
)

// ListGroups returns the groups the Client's user ID has access to.
// Used by setup auto-detect + the lazy shared-library probe.
func (c *Client) ListGroups(ctx context.Context) ([]zot.GroupRef, error) {
	resp, err := c.Gen.ListGroupsWithResponse(ctx, c.UserID)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("GET /users/%d/groups: %s: %s", c.UserID, resp.Status(), string(resp.Body))
	}
	if resp.JSON200 == nil {
		return nil, nil
	}
	out := make([]zot.GroupRef, 0, len(*resp.JSON200))
	for _, g := range *resp.JSON200 {
		out = append(out, zot.GroupRef{
			ID:   strconv.Itoa(g.Id),
			Name: g.Data.Name,
		})
	}
	return out, nil
}
