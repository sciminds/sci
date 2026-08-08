// Package oacache builds the OpenAlex work cache that `zot` reads.
//
// # Why this lives in sci
//
// zot is the derived READ plane: it is deployed to an internet-facing VM
// and to a laptop, and it should hold no credentials and make no network
// calls anywhere. OpenAlex is now a metered API — an unauthenticated
// request draws on a small daily USD budget and a key is billable — so the
// machine that fetches is the machine that already has the key, which is
// this one.
//
// The two tools still meet at files: this writes an NDJSON body plus a
// sidecar into zot's staging directory, exactly as `zot export --format
// ndjson` does, and nothing here knows what zot will do with it.
//
// # This package fetches; it never decides identity
//
// Title lookups return CANDIDATES, all of them, unfiltered. Deciding which
// candidate is the same work as a library item is resolution, and
// resolution belongs to zot, where the normalization it depends on is
// defined exactly once. Filtering here would require a second definition
// of title_norm, and two definitions differing by one character produce
// two silently different corpora.
package oacache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/pkg/openalex"
)

// WorkSelect is the field mask requested for every work.
//
// It is explicit because the bootstrap caches this replaces were fetched
// with a narrower one — no publication_year, no type, no display_name —
// and the consumer had no way to tell a field that is absent from a field
// that is null. Ask for everything the schema has a column for.
var WorkSelect = []string{
	"id", "doi", "display_name", "title", "publication_year", "publication_date",
	"type", "language", "is_retracted", "cited_by_count", "fwci",
	"primary_location", "locations", "authorships", "topics", "keywords",
	"referenced_works", "updated_date",
}

// doiBatch is how many DOIs go into one `doi:a|b|c` filter. OpenAlex
// documents 50–100 as safe for the id filters; 50 keeps URLs short and
// matches what pkg/openalex uses for WorksByID.
const doiBatch = 50

// DefaultTitleCandidates caps how many results a title lookup keeps.
//
// A cap is a truncation, so it is reported (see Stats.TitlesTruncated)
// rather than applied quietly. Five is enough to hold the real work plus
// its preprint and a duplicate record or two; zot's uniqueness rule
// decides among them.
const DefaultTitleCandidates = 5

// Want is what the library needs looked up. Both lists come from the
// Zotero mirror and neither is normalized here.
type Want struct {
	DOIs   []string
	Titles []string
}

// Stats is what a sync did. Every number that could hide a loss is here:
// a DOI OpenAlex does not have, a title lookup that returned nothing, a
// candidate list that hit the cap.
type Stats struct {
	DOIsRequested int `json:"dois_requested"`
	DOIsFound     int `json:"dois_found"`
	DOIsNotFound  int `json:"dois_not_found"`
	// DOIsUnbatchable counts DOIs sent one at a time because they carry a
	// filter metacharacter. Reported because it is the difference between
	// 84 requests and 103, and because a spike means the library grew a
	// batch of bad metadata.
	DOIsUnbatchable int `json:"dois_unbatchable"`
	TitlesQueried   int `json:"titles_queried"`
	TitlesWithHits  int `json:"titles_with_hits"`
	TitlesTruncated int `json:"titles_truncated"`
	Works           int `json:"works"`
	Requests        int `json:"requests"`
}

// Searcher is what oacache needs from an OpenAlex client.
//
// ResolveWork is not an optimization — see [Batchable]. *openalex.Client
// satisfies both; a test satisfies them without a network.
type Searcher interface {
	SearchWorks(ctx context.Context, opts openalex.SearchOpts) (*openalex.Results[openalex.Work], error)
	ResolveWork(ctx context.Context, identifier string) (*openalex.Work, error)
}

// filterMeta are the characters that mean something to OpenAlex's filter
// DSL: `,` separates filters, `|` is OR, and `:` splits key from value.
const filterMeta = ",|:"

// Batchable reports whether a DOI can safely ride in a `doi:a|b|c` filter.
//
// It usually cannot look like a problem and usually is not one — until it
// is catastrophic. A single DOI containing a colon makes OpenAlex parse
// the REST of the batch as a second filter and reject the whole request,
// so ONE bad value loses the other 49 lookups.
//
// And these are not corrupt DOIs. Kluwer minted `10.1023/a:1007465528199`,
// Wiley minted the SICI form `10.1002/(sici)…5:5<329::aid-hbm1>3.0.co;2-5`,
// Oxford mints `10.1093/acprof:oso/…`. They are perfectly resolvable — just
// not through a filter — so they go one at a time down the path route
// rather than being dropped as malformed.
func Batchable(doi string) bool { return !strings.ContainsAny(doi, filterMeta) }

// TitleFilter wraps a title for `title.search:`.
//
// Percent-encoding is NOT enough: OpenAlex's edge decodes the filter param
// and only then splits on commas, so a `%2C` still becomes a filter
// separator and the request is rejected. Their documented escape is to
// quote the whole value, which also keeps it a phrase rather than a bag of
// words. Academic titles are full of commas and colons, so without this
// the very first title lookup fails and takes the run with it.
//
// Embedded double quotes would close the wrapper early, so they become
// spaces. A stemmed full-text search does not care.
func TitleFilter(title string) string {
	return `"` + strings.ReplaceAll(title, `"`, " ") + `"`
}

// Options tunes a sync.
type Options struct {
	// TitleCandidates caps results kept per title lookup. Zero means
	// DefaultTitleCandidates.
	TitleCandidates int
	// Progress, when set, is called after each request.
	Progress func(done, total int)
}

// Result is the fetched cache, deduplicated by OpenAlex id.
type Result struct {
	Works []openalex.Work
	Stats Stats
	// NotFound lists the DOIs OpenAlex returned nothing for.
	//
	// It is a list, not a count, because "we asked and it is not there" is
	// a claim someone may want to check. It is also NOT a claim that the
	// paper does not exist: OpenAlex 404s on plenty of real monographs and
	// preprints, and sci learned the expensive way that rendering that as
	// "not found" reads as "likely fabricated" on a real manuscript.
	NotFound []string
}

// Fetch resolves a Want into work records.
func Fetch(ctx context.Context, c Searcher, w Want, opts Options) (Result, error) {
	cap := opts.TitleCandidates
	if cap <= 0 {
		cap = DefaultTitleCandidates
	}
	var res Result
	seen := map[string]bool{}

	batchable, solo := lo.FilterReject(w.DOIs, func(d string, _ int) bool { return Batchable(d) })
	total := batches(len(batchable), doiBatch) + len(solo) + len(w.Titles)
	done := 0

	keep := func(works []openalex.Work) int {
		var n int
		for _, work := range works {
			id := shortID(work.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			res.Works = append(res.Works, work)
			n++
		}
		return n
	}
	tick := func() {
		done++
		res.Stats.Requests++
		if opts.Progress != nil {
			opts.Progress(done, total)
		}
	}

	// DOIs, batched. A DOI that comes back with nothing is recorded by
	// difference: OpenAlex returns the works it has and says nothing at all
	// about the ones it does not, so the only way to know is to ask what we
	// sent and compare.
	res.Stats.DOIsRequested = len(w.DOIs)
	res.Stats.DOIsUnbatchable = len(solo)
	for _, batch := range lo.Chunk(batchable, doiBatch) {
		out, err := c.SearchWorks(ctx, openalex.SearchOpts{
			Filter:  map[string]string{"doi": strings.Join(batch, "|")},
			PerPage: len(batch),
			Select:  WorkSelect,
		})
		tick()
		if err != nil {
			return res, fmt.Errorf("openalex doi batch: %w", err)
		}
		got := map[string]bool{}
		for _, work := range out.Results {
			if work.DOI != nil {
				got[normDOI(*work.DOI)] = true
			}
		}
		res.NotFound = append(res.NotFound,
			lo.Reject(batch, func(d string, _ int) bool { return got[normDOI(d)] })...)
		keep(out.Results)
	}
	// The filter-hostile DOIs, one path request each. ResolveWork goes
	// through /works/{id}, which never sees the filter DSL.
	for _, d := range solo {
		work, err := c.ResolveWork(ctx, d)
		tick()
		if err != nil || work == nil {
			// A lookup that failed is a lookup that found nothing to say.
			// It joins the not-found list, which is a statement about
			// OpenAlex's coverage and never about whether the paper exists.
			res.NotFound = append(res.NotFound, d)
			continue
		}
		keep([]openalex.Work{*work})
	}

	res.Stats.DOIsNotFound = len(res.NotFound)
	res.Stats.DOIsFound = res.Stats.DOIsRequested - res.Stats.DOIsNotFound

	// Titles, one lookup each. Every candidate is kept; nothing here judges
	// whether a candidate IS the item.
	res.Stats.TitlesQueried = len(w.Titles)
	for _, title := range w.Titles {
		out, err := c.SearchWorks(ctx, openalex.SearchOpts{
			Filter:  map[string]string{"title.search": TitleFilter(title)},
			PerPage: cap,
			Select:  WorkSelect,
		})
		tick()
		if err != nil {
			return res, fmt.Errorf("openalex title lookup: %w", err)
		}
		if len(out.Results) > 0 {
			res.Stats.TitlesWithHits++
		}
		if out.Meta.Count > cap {
			res.Stats.TitlesTruncated++
		}
		keep(out.Results)
	}

	res.Stats.Works = len(res.Works)
	return res, nil
}

// Meta is the sidecar written beside the cache body.
type Meta struct {
	ProducedAt   string   `json:"produced_at"`
	ProducedBy   string   `json:"produced_by"`
	Scope        string   `json:"scope"`
	RecordsTotal int      `json:"records_total"`
	Select       []string `json:"select"`
	Stats        Stats    `json:"stats"`
	NotFound     []string `json:"not_found,omitempty"`
	SHA256       string   `json:"sha256"`
}

// CacheFile is the body's name inside a staging directory.
const CacheFile = "openalex-works.ndjson"

// Write writes the cache body and then its sidecar.
//
// The order is the contract: the sidecar goes LAST and carries a digest of
// the bytes that actually landed, so a consumer finding a body without a
// matching sidecar knows it caught a partial write rather than guessing.
// This mirrors `zot export --format ndjson` exactly, because the consumer
// is the same one.
func Write(dir, scope string, res Result) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body := filepath.Join(dir, CacheFile)

	f, err := os.Create(body) //nolint:gosec // path is the caller's own --out
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	for i := range res.Works {
		if err := enc.Encode(res.Works[i]); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("encode work %s: %w", res.Works[i].ID, err)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	sum, err := digest(body)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(Meta{
		ProducedAt:   time.Now().UTC().Format(time.RFC3339),
		ProducedBy:   "sci zot openalex sync",
		Scope:        scope,
		RecordsTotal: len(res.Works),
		Select:       WorkSelect,
		Stats:        res.Stats,
		NotFound:     res.NotFound,
		SHA256:       sum,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	//nolint:gosec // sidecar mirrors the body's perms
	if err := os.WriteFile(body+".meta.json", append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	return body, nil
}

func digest(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is the body just written
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// normDOI lowercases and strips the resolver prefix so a DOI stored as a
// URL by Zotero compares equal to OpenAlex's own URL form. This is only
// ever used to match a response against a request inside this file — it is
// NOT the corpus's DOI normalization, which lives in zot's SQL macros.
func normDOI(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "doi:"} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func batches(total, size int) int {
	if total == 0 {
		return 0
	}
	return (total + size - 1) / size
}
