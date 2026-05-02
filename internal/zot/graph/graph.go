// Package graph traverses citation relationships between Zotero items
// and the wider OpenAlex universe.
//
// The package answers two questions an agent doing a literature review
// keeps wanting to ask:
//
//   - Refs:   what does this paper cite?            → openalex.Work.ReferencedWorks
//   - Cites:  what cites this paper?                → openalex.Client.CitedBy
//
// Both flows resolve the input Zotero item to an OpenAlex Work id (from
// the `OpenAlex: W…` line in the item's Extra field, with a DOI lookup
// fallback), pull the relevant id list from OpenAlex, then split the
// result into "already in your library" and "outside your library"
// buckets via local.Reader.ItemKeysByDOI. The agent-facing JSON shape is
// designed so the outside_library entries can be piped straight into
// `zot item add --openalex {openalex_id}`.
//
// The package depends on local.Reader (read-only DB), openalex.Client,
// and the enrich.OpenAlexID helper. It deliberately knows nothing about
// the CLI surface — wiring lives in internal/zot/cli/graph.go.
package graph

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/enrich"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// LibraryIndex resolves a list of DOIs to Zotero item keys for items in
// the user's library. The narrow interface lets callers swap between a
// local-DB-backed implementation (cheap, may be stale) and a Web-API
// pre-fetched implementation (slower first call, always current).
//
// Returns lowercase-DOI → key. DOIs not in the library are absent from
// the result map (no nil entries).
type LibraryIndex interface {
	LookupKeysByDOI(dois []string) (map[string]string, error)
}

// readerIndex adapts a local.Reader to the LibraryIndex interface.
type readerIndex struct{ r local.Reader }

// LocalIndex wraps a local.Reader — the cheap, default path. May be
// stale right after a write; use RemoteIndex for agent flows that act
// on items they just created.
func LocalIndex(r local.Reader) LibraryIndex { return readerIndex{r} }

func (i readerIndex) LookupKeysByDOI(dois []string) (map[string]string, error) {
	return i.r.ItemKeysByDOI(dois)
}

// Source identifies the parent paper a graph result describes.
type Source struct {
	Key      string `json:"key"`
	Title    string `json:"title,omitempty"`
	OpenAlex string `json:"openalex,omitempty"`
	DOI      string `json:"doi,omitempty"`
}

// Neighbor is one paper in the graph adjacent to the source. When the
// paper is in the user's library, Key is set; otherwise it carries the
// OpenAlex id + DOI so callers can `zot item add --openalex` it.
type Neighbor struct {
	Key          string   `json:"key,omitempty"`
	OpenAlex     string   `json:"openalex,omitempty"`
	DOI          string   `json:"doi,omitempty"`
	Title        string   `json:"title"`
	Year         int      `json:"year,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	CitedByCount int      `json:"cited_by_count,omitempty"`
	OAStatus     string   `json:"oa_status,omitempty"`
}

// Stats summarizes a graph result for human + JSON consumers.
type Stats struct {
	Total          int `json:"total"`
	InLibrary      int `json:"in_library"`
	OutsideLibrary int `json:"outside_library"`
	// MissingMetadata counts neighbors whose OpenAlex record carried no
	// DOI — we still surface them but can't intersect with the library.
	MissingMetadata int `json:"missing_metadata,omitempty"`
}

// Result is the shared shape returned by Refs and Cites. JSON-tagged for
// direct cmdutil.Output.
type Result struct {
	Item           Source     `json:"item"`
	Direction      string     `json:"direction"` // "refs" | "cites"
	InLibrary      []Neighbor `json:"in_library"`
	OutsideLibrary []Neighbor `json:"outside_library"`
	Stats          Stats      `json:"stats"`
}

// CitesOpts narrows the Cites query.
type CitesOpts struct {
	Limit   int               // max neighbors (default 25)
	YearMin int               // optional from_publication_date floor
	Filter  map[string]string // additional OpenAlex filters
}

// RefsOpts narrows the Refs query. Refs always fetches the full bibliography
// from OpenAlex (it's a single batched lookup); Limit then caps how many
// neighbors are surfaced in the response. in_library entries are kept first
// since they're the high-signal subset agents care about most; remaining
// slots are filled with outside_library in OpenAlex order. Stats keeps the
// pre-truncation totals so callers can detect truncation happened.
//
// Limit <= 0 disables truncation (full bibliography).
type RefsOpts struct {
	Limit int
}

// ErrNoOpenAlexAnchor is returned when an item lacks both an OpenAlex
// id (in Extra) and a DOI we can use to look one up. The graph commands
// surface this so the user knows enrichment is needed first.
var ErrNoOpenAlexAnchor = errors.New("graph: item has no OpenAlex id or DOI to anchor traversal")

// Refs returns the works that the given Zotero item cites.
//
// opts.Limit caps the number of neighbors emitted in the response (Stats
// retains the pre-truncation totals). See RefsOpts for the truncation
// policy. Pass RefsOpts{} for unlimited output.
func Refs(ctx context.Context, idx LibraryIndex, oa *openalex.Client, item *local.Item, opts RefsOpts) (*Result, error) {
	work, err := resolveWork(ctx, oa, item)
	if err != nil {
		return nil, err
	}
	src := sourceFrom(item, work)

	if len(work.ReferencedWorks) == 0 {
		return &Result{Item: src, Direction: "refs"}, nil
	}

	hydrated, err := oa.WorksByID(ctx, work.ReferencedWorks)
	if err != nil {
		return nil, fmt.Errorf("hydrate referenced works: %w", err)
	}
	in, out, missing := splitByLibrary(idx, hydrated)
	stats := Stats{
		Total:           len(hydrated),
		InLibrary:       len(in),
		OutsideLibrary:  len(out),
		MissingMetadata: missing,
	}
	in, out = truncateNeighbors(in, out, opts.Limit)
	return &Result{
		Item:           src,
		Direction:      "refs",
		InLibrary:      in,
		OutsideLibrary: out,
		Stats:          stats,
	}, nil
}

// truncateNeighbors caps in_library + outside_library to limit total
// neighbors. in_library entries are preserved first; remaining slots are
// filled with outside_library in input order. limit <= 0 returns the
// inputs unchanged.
func truncateNeighbors(in, out []Neighbor, limit int) ([]Neighbor, []Neighbor) {
	if limit <= 0 {
		return in, out
	}
	if len(in) >= limit {
		return in[:limit], nil
	}
	remaining := limit - len(in)
	if len(out) > remaining {
		out = out[:remaining]
	}
	return in, out
}

// Cites returns the works that cite the given Zotero item.
func Cites(ctx context.Context, idx LibraryIndex, oa *openalex.Client, item *local.Item, opts CitesOpts) (*Result, error) {
	work, err := resolveWork(ctx, oa, item)
	if err != nil {
		return nil, err
	}
	src := sourceFrom(item, work)

	id := shortID(work.ID)
	if id == "" {
		return nil, fmt.Errorf("resolved work has no OpenAlex id (raw: %q)", work.ID)
	}
	search := openalex.SearchOpts{
		Filter:  map[string]string{},
		PerPage: opts.Limit,
	}
	maps.Copy(search.Filter, opts.Filter)
	if opts.YearMin > 0 {
		search.Filter["from_publication_date"] = fmt.Sprintf("%04d-01-01", opts.YearMin)
	}
	res, err := oa.CitedBy(ctx, id, search)
	if err != nil {
		return nil, fmt.Errorf("cited_by lookup: %w", err)
	}
	in, out, missing := splitByLibrary(idx, res.Results)
	return &Result{
		Item:           src,
		Direction:      "cites",
		InLibrary:      in,
		OutsideLibrary: out,
		Stats: Stats{
			Total:           len(res.Results),
			InLibrary:       len(in),
			OutsideLibrary:  len(out),
			MissingMetadata: missing,
		},
	}, nil
}

// resolveWork picks the cheapest path to OpenAlex metadata: in-library
// `OpenAlex: W…` line first (offline), then DOI lookup. Items lacking both
// fall through to ErrNoOpenAlexAnchor — DOI-less items can't be anchored
// without a fuzzy title search we don't trust enough to do silently.
func resolveWork(ctx context.Context, oa *openalex.Client, it *local.Item) (*openalex.Work, error) {
	if it == nil {
		return nil, fmt.Errorf("graph: nil item")
	}
	if id := enrich.OpenAlexID(it); id != "" {
		return oa.ResolveWork(ctx, id)
	}
	if it.DOI != "" {
		return oa.ResolveWork(ctx, it.DOI)
	}
	return nil, ErrNoOpenAlexAnchor
}

// sourceFrom builds the Source descriptor returned alongside every graph
// result. Pulls the OpenAlex id from the resolved Work (so it's correct
// even when the item itself didn't carry an `OpenAlex:` line).
func sourceFrom(it *local.Item, w *openalex.Work) Source {
	src := Source{Key: it.Key, Title: it.Title, DOI: it.DOI}
	src.OpenAlex = shortID(w.ID)
	if src.Title == "" && w.Title != nil {
		src.Title = *w.Title
	}
	return src
}

// splitByLibrary partitions the OpenAlex Work list into "already in the
// user's library" vs "outside" using a single batched DOI lookup. Returns
// the count of works whose DOI is missing — these end up in the outside
// bucket since we can't prove they're in the library.
func splitByLibrary(idx LibraryIndex, works []openalex.Work) (in, out []Neighbor, missingMeta int) {
	if len(works) == 0 {
		return nil, nil, 0
	}
	dois := lo.FilterMap(works, func(w openalex.Work, _ int) (string, bool) {
		if w.DOI == nil || *w.DOI == "" {
			return "", false
		}
		return strings.ToLower(stripDOIScheme(*w.DOI)), true
	})

	hits := map[string]string{}
	if len(dois) > 0 {
		// Best-effort: an index error would leave every neighbor in the
		// outside bucket. We accept that — strictly safer than falsely
		// claiming items aren't in the library when they are.
		if got, err := idx.LookupKeysByDOI(dois); err == nil {
			hits = got
		}
	}

	for _, w := range works {
		n := neighborFromWork(w)
		var doi string
		if w.DOI != nil {
			doi = strings.ToLower(stripDOIScheme(*w.DOI))
		}
		if doi == "" {
			missingMeta++
		}
		if key, ok := hits[doi]; ok && doi != "" {
			n.Key = key
			in = append(in, n)
		} else {
			out = append(out, n)
		}
	}
	return in, out, missingMeta
}

// neighborFromWork distills an OpenAlex Work into the compact agent-facing
// shape. Mirrors the philosophy in zot find: ~10 flat fields per entity,
// just enough to rank and pick.
func neighborFromWork(w openalex.Work) Neighbor {
	n := Neighbor{
		OpenAlex:     shortID(w.ID),
		CitedByCount: w.CitedByCount,
	}
	if w.DOI != nil {
		n.DOI = stripDOIScheme(*w.DOI)
	}
	if w.Title != nil {
		n.Title = *w.Title
	} else if w.DisplayName != nil {
		n.Title = *w.DisplayName
	}
	if w.PublicationYear != nil {
		n.Year = *w.PublicationYear
	}
	if w.OpenAccess != nil {
		n.OAStatus = w.OpenAccess.OAStatus
	}
	n.Authors = lo.Map(w.Authorships, func(a openalex.Authorship, _ int) string {
		return a.Author.DisplayName
	})
	return n
}

// shortID extracts the short W-id from an OpenAlex full URL. Empty in,
// empty out — keeps the call sites unguarded.
func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// stripDOIScheme removes the doi.org URL prefix from a DOI string, returning
// the bare "10.xxxx/yyy" form. OpenAlex stores DOIs with the URL prefix;
// Zotero stores them bare. Normalize before intersection.
func stripDOIScheme(doi string) string {
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/"} {
		if strings.HasPrefix(strings.ToLower(doi), p) {
			return doi[len(p):]
		}
	}
	return doi
}
