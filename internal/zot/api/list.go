package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sciminds/cli/internal/zot/client"
)

// ListItemsOptions is the filter/pagination payload for Client.ListItems.
// Zero-value fields are passed through as "no filter".
type ListItemsOptions struct {
	// CollectionKey, if set, narrows the listing to one collection's members
	// (via GET /collections/{key}/items). Empty = whole library.
	CollectionKey string
	// ItemType accepts Zotero's filter grammar: "journalArticle",
	// "book || bookSection", "-attachment".
	ItemType string
	// Query is a free-text search term. Zotero's Web API scans title,
	// creators, and year by default; set QMode = "everything" to also
	// match abstract, fulltext, and notes.
	Query string
	// QMode selects the search mode. Valid values: "titleCreatorYear"
	// (default), "everything". Only consulted when Query is non-empty.
	QMode string
	// Start is the zero-indexed pagination offset.
	Start int
	// Limit is the per-page cap (Zotero max is 100).
	Limit int
}

// defaultListPageSize is the Zotero API's max page size for most endpoints.
const defaultListPageSize = 100

// ListCollections fetches every collection in the target library,
// paginating at the Web API's 100-per-page cap. Returns the raw
// client.Collection slice; callers convert via CollectionFromClient
// when they want the local.Collection shape.
//
// Agent use case: immediately after CreateCollection, the local SQLite
// hasn't synced yet, so `zot collection list` would miss what was just
// created. This method sidesteps that.
func (c *Client) ListCollections(ctx context.Context) ([]client.Collection, error) {
	var all []client.Collection
	start := 0
	for {
		status, statusLine, page, err := c.listCollections(ctx, start, defaultListPageSize)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("GET /collections: %s", statusLine)
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

// ListItems fetches items matching opts, paginating as needed.
// When opts.Limit > 0, stops after that many results (single page only for
// small limits; paginates when Limit > defaultListPageSize).
func (c *Client) ListItems(ctx context.Context, opts ListItemsOptions) ([]client.Item, error) {
	var all []client.Item
	want := opts.Limit
	start := opts.Start
	for {
		pageLimit := defaultListPageSize
		if want > 0 && want-len(all) < pageLimit {
			pageLimit = want - len(all)
		}
		pageOpts := opts
		pageOpts.Start = start
		pageOpts.Limit = pageLimit

		status, statusLine, page, err := c.listItems(ctx, pageOpts)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("GET /items: %s", statusLine)
		}
		if page == nil || len(*page) == 0 {
			break
		}
		all = append(all, *page...)
		if want > 0 && len(all) >= want {
			break
		}
		if len(*page) < pageLimit {
			break
		}
		start += len(*page)
	}
	return all, nil
}
