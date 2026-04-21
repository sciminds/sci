package openalex

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

const (
	defaultPerPage = 25
	maxPerPage     = 200
)

// SearchOpts shapes a query against a list endpoint (/works, /authors, …).
//
// Page and Cursor are mutually exclusive: set Cursor to "*" to begin a cursor
// walk and follow Meta.NextCursor; otherwise use Page (default 1, max page
// depth 10_000 / PerPage per OpenAlex).
type SearchOpts struct {
	Search  string            // free-text search
	Filter  map[string]string // OpenAlex filter DSL — joined "key:value,key:value"
	PerPage int               // 1–200; defaults to 25
	Page    int               // page-based paging
	Cursor  string            // cursor-based paging ("*" to start)
	Sort    string            // e.g. "cited_by_count:desc"
	Select  []string          // response field mask; joined into "select" param
}

func buildSearchParams(o SearchOpts) url.Values {
	v := url.Values{}
	if o.Search != "" {
		v.Set("search", o.Search)
	}
	perPage := o.PerPage
	if perPage <= 0 {
		perPage = defaultPerPage
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	v.Set("per_page", strconv.Itoa(perPage))
	if o.Page > 0 {
		v.Set("page", strconv.Itoa(o.Page))
	}
	if o.Cursor != "" {
		v.Set("cursor", o.Cursor)
	}
	if o.Sort != "" {
		v.Set("sort", o.Sort)
	}
	if len(o.Select) > 0 {
		v.Set("select", strings.Join(o.Select, ","))
	}
	if len(o.Filter) > 0 {
		keys := lo.Keys(o.Filter)
		slices.Sort(keys) // stable ordering — keeps tests and request logs deterministic
		parts := lo.Map(keys, func(k string, _ int) string { return k + ":" + o.Filter[k] })
		v.Set("filter", strings.Join(parts, ","))
	}
	return v
}

// SearchWorks performs a single GET /works request.
func (c *Client) SearchWorks(ctx context.Context, opts SearchOpts) (*Results[Work], error) {
	var out Results[Work]
	if err := c.Get(ctx, "/works", buildSearchParams(opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchAuthors performs a single GET /authors request.
func (c *Client) SearchAuthors(ctx context.Context, opts SearchOpts) (*Results[Author], error) {
	var out Results[Author]
	if err := c.Get(ctx, "/authors", buildSearchParams(opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IterateWorks walks all pages of a Works search via cursor pagination,
// invoking fn for each page. Stops early when fn returns a non-nil error
// or when the server stops returning a next_cursor.
func (c *Client) IterateWorks(ctx context.Context, opts SearchOpts, fn func([]Work) error) error {
	return iterateCursor(ctx, opts, func(o SearchOpts) (*Results[Work], error) {
		return c.SearchWorks(ctx, o)
	}, fn)
}

// IterateAuthors walks all pages of an Authors search via cursor pagination.
func (c *Client) IterateAuthors(ctx context.Context, opts SearchOpts, fn func([]Author) error) error {
	return iterateCursor(ctx, opts, func(o SearchOpts) (*Results[Author], error) {
		return c.SearchAuthors(ctx, o)
	}, fn)
}

func iterateCursor[T any](
	ctx context.Context,
	opts SearchOpts,
	fetch func(SearchOpts) (*Results[T], error),
	fn func([]T) error,
) error {
	// Cursor paging starts at "*". Page and Cursor are mutually exclusive —
	// force cursor mode for the walk.
	opts.Page = 0
	if opts.Cursor == "" {
		opts.Cursor = "*"
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := fetch(opts)
		if err != nil {
			return err
		}
		if err := fn(res.Results); err != nil {
			return err
		}
		if res.Meta.NextCursor == nil || *res.Meta.NextCursor == "" {
			return nil
		}
		opts.Cursor = *res.Meta.NextCursor
	}
}
