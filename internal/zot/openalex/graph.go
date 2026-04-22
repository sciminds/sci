package openalex

import (
	"context"
	"strings"

	"github.com/samber/lo"
)

// citedByPageSize caps how many citing works we ask OpenAlex for in a
// single Cites call. The /works list endpoint allows up to 200 per page,
// but the citation-graph use case (an agent skimming top-N citing papers)
// rarely benefits from more than a few dozen — so default to 25 and let
// callers raise it via opts.PerPage when needed.
const citedByPageSize = 25

// CitedBy returns works that cite the given OpenAlex Work id, sorted by
// citation count desc (most influential first). Pass opts.Filter to add
// extra filters (e.g. "from_publication_date=2020-01-01" via the
// SearchOpts.Filter map). The cited_by filter is added automatically.
//
// Pagination: when opts.PerPage == 0 the page is sized to citedByPageSize.
// Walk subsequent pages via opts.Cursor or use IterateWorks for a full
// crawl.
func (c *Client) CitedBy(ctx context.Context, id string, opts SearchOpts) (*Results[Work], error) {
	if opts.Filter == nil {
		opts.Filter = map[string]string{}
	}
	opts.Filter["cited_by"] = id
	if opts.PerPage == 0 {
		opts.PerPage = citedByPageSize
	}
	if opts.Sort == "" {
		opts.Sort = "cited_by_count:desc"
	}
	return c.SearchWorks(ctx, opts)
}

// worksByIDBatchSize caps how many ids we pack into a single
// `openalex_id:W1|W2|…` filter. OpenAlex documents 50–100 as a safe limit
// per /works call; we go conservative at 50 to keep URLs short.
const worksByIDBatchSize = 50

// WorksByID fetches Work records for a list of OpenAlex Work ids in
// one (or a few) batched calls. Use this to hydrate the
// referenced_works array of a parent Work without N round-trips.
//
// Returns the works in OpenAlex's response order (which is the natural
// order /works returns them — usually NOT the order of the input). When
// you need stable ordering, key by ID at the call site:
//
//	got, _ := c.WorksByID(ctx, ids)
//	byID := lo.KeyBy(got, func(w Work) string { return shortID(w.ID) })
//
// Empty input → empty result, no HTTP call.
func (c *Client) WorksByID(ctx context.Context, ids []string) ([]Work, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Normalize to OpenAlex short IDs (W…). The filter accepts either
	// short ids or full URLs, but short ids keep the URL compact.
	short := lo.Map(ids, func(id string, _ int) string { return shortIDFromAny(id) })
	short = lo.Filter(short, func(id string, _ int) bool { return id != "" })

	out := make([]Work, 0, len(short))
	for _, batch := range lo.Chunk(short, worksByIDBatchSize) {
		filter := map[string]string{"openalex_id": strings.Join(batch, "|")}
		// per_page must be ≥ batch size so a single request returns all
		// matches; we use len(batch) directly to avoid the default 25 cap.
		res, err := c.SearchWorks(ctx, SearchOpts{Filter: filter, PerPage: len(batch)})
		if err != nil {
			return nil, err
		}
		out = append(out, res.Results...)
	}
	return out, nil
}

// shortIDFromAny normalizes an OpenAlex id to its bare short form
// ("W12345"). Accepts short ids, full URLs, and lowercase variants. Mirrors
// extractOpenAlexShortID in enrich/mapping.go but lives here to avoid
// pulling enrich into the openalex package (which must stay leaf).
func shortIDFromAny(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return strings.ToUpper(id)
}
