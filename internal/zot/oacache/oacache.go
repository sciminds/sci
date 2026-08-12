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
	"bufio"
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
	"github.com/sciminds/sci/pkg/openalex"
)

// WorkSelect is the field mask requested for every work. It must name
// exactly the json tags [openalex.Work] decodes, no more and no less —
// TestWorkSelectCoversEveryFieldTheWorkStructDecodes enforces both halves.
//
// It is explicit because the bootstrap caches this replaces were fetched
// with a narrower one — no publication_year, no type, no display_name —
// and the consumer had no way to tell a field that is absent from a field
// that is null. Ask for everything the schema has a column for.
//
// That rule was stated here and then quietly broken in both directions:
// eight fields the struct decodes were never requested, and one requested
// field the struct has no home for was decoded by nobody. The costly one
// was abstract_inverted_index — a full sync wrote 6,618 works with zero
// abstracts, and since OpenAlex genuinely lacks abstracts for some works,
// nothing downstream could tell the omission from the truth. A field mask
// is not free to get wrong: this API is metered, so a field left out is
// re-fetched at full price or not at all.
//
// The test enforces mask == struct. It cannot enforce struct == API, and
// that gap cuts both ways. biblio and ids were absent from the struct, so
// the mask was "complete" while never asking for them and 1,991 papers
// stayed without a volume. In the other direction, is_oa was IN the struct
// and is not selectable at all — OpenAlex 400s the whole request for it —
// which is why the response shape and the selectable set have to be
// treated as two different lists. The same fact is reachable as
// open_access.is_oa.
//
// A rejected select field fails loudly on the first request, so the cost
// of getting this wrong is one round trip. A missing one fails silently
// and costs a whole re-sync. Before paying for a sync, check the Work
// docs for fields this type does not model yet.
var WorkSelect = []string{
	"id", "doi", "display_name", "title", "publication_year", "publication_date",
	"type", "language", "is_retracted", "cited_by_count", "referenced_works_count",
	"fwci", "citation_normalized_percentile", "has_fulltext",
	"open_access", "primary_location", "best_oa_location", "locations",
	"authorships", "topics", "keywords", "mesh",
	"referenced_works", "abstract_inverted_index",
	"biblio", "ids",
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
	// FallbackTitles maps a DOI to the title of the item carrying it, for
	// the case where OpenAlex has no record of that DOI.
	//
	// An item goes down the DOI path OR the title path, never both,
	// because a DOI is the stronger identifier. That holds only while the
	// index actually has it. When it does not, a DOI is strictly WORSE
	// than none: the lookup returns nothing and the title lookup that
	// would have matched was never made. Backfilling 709 inferred DOIs
	// made this concrete -- 20 landed on records OpenAlex does not hold,
	// and 10 items that used to resolve by title stopped resolving at all.
	FallbackTitles map[string]string
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
	// Fallback lookups are counted apart from TitlesQueried, which is the
	// number of items that had no DOI at all. Fusing them would leave the
	// run unable to report how much of itself was spent recovering from
	// DOIs the index does not hold — which is the number that says whether
	// the library's identifiers are getting better or worse.
	FallbackTitlesQueried  int `json:"fallback_titles_queried"`
	FallbackTitlesWithHits int `json:"fallback_titles_with_hits"`
	Works                  int `json:"works"`
	Requests               int `json:"requests"`
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

	// Recover the misses by title. Bounded by the not-found list, so a
	// library whose DOIs are all good pays nothing for this.
	//
	// The DOI stays in NotFound either way: the fallback recovers a WORK,
	// it does not improve OpenAlex's coverage of that identifier, and a
	// sidecar that dropped it would overstate the index.
	for _, d := range res.NotFound {
		title := w.FallbackTitles[d]
		if title == "" {
			continue
		}
		res.Stats.FallbackTitlesQueried++
		out, err := c.SearchWorks(ctx, openalex.SearchOpts{
			Filter:  map[string]string{"title.search": TitleFilter(title)},
			PerPage: cap,
			Select:  WorkSelect,
		})
		res.Stats.Requests++
		if err != nil {
			// One unrecoverable title is not a reason to lose the whole
			// sync; the DOI is already recorded as not found.
			continue
		}
		if len(out.Results) > 0 {
			res.Stats.FallbackTitlesWithHits++
		}
		keep(out.Results)
	}

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
//
// Fields are only ever ADDED. Two live consumers parse this file — zot's
// build and the unattended pipeline runner's spend leash — and neither
// version-negotiates, so a renamed key is a silent zero somewhere.
type Meta struct {
	ProducedAt   string   `json:"produced_at"`
	ProducedBy   string   `json:"produced_by"`
	Scope        string   `json:"scope"`
	RecordsTotal int      `json:"records_total"`
	Select       []string `json:"select"`
	Stats        Stats    `json:"stats"`
	NotFound     []string `json:"not_found,omitempty"`
	SHA256       string   `json:"sha256"`
	// Delta is set only when the body was MERGED into rather than replaced.
	// Its absence is what says "this file is everything the run fetched".
	Delta *Delta `json:"delta,omitempty"`
	// FullSyncStats is the last FULL sync's accounting, carried across
	// every delta since. Stats above describes THIS run — two requests for
	// a targeted one — and a caller pricing a full re-sync from that number
	// would conclude it is free. See carriedFullSyncStats.
	FullSyncStats *Stats `json:"full_sync_stats,omitempty"`
}

// CitedSelect is the field mask for works the library CITES rather than
// owns. It is deliberately narrow, and that is a different thing from the
// accidental narrowness WorkSelect's comment warns about: these records
// exist to NAME an edge's target and to let the title tiers disambiguate
// it, not to describe a paper. Whole records for ~186k cited works is a
// different product with a very different footprint.
//
// Deliberate still has to be recorded. Write publishes this list into the
// sidecar, because a mask's omissions are invisible in the data it
// produces -- rows parse, counts hold, and the corpus is stale in SHAPE.
var CitedSelect = []string{"id", "display_name", "publication_year", "doi", "type"}

// CitedFile is the reference title pool's name inside a staging directory.
// It keeps the name the zen-ingest artifact used, so the consumer's staging
// plumbing does not move when the producer changes underneath it.
const CitedFile = "openalex-titles.ndjson"

// CitedResult is what FetchCited answers.
type CitedResult struct {
	Works []openalex.Work
	// Asked is how many distinct ids went out, so a caller can report the
	// difference between what was requested and what came back rather than
	// presenting a short result as a complete one.
	Asked int
	// Requests is what this arm actually spent. Fetch counts its own; a
	// targeted run has to report the sum, because the cap it lives under is
	// on the whole run.
	Requests int
}

// FetchCited hydrates the works named by the referenced_works of works
// already fetched -- the reference title pool.
//
// The set is derived from `have` rather than queried, so the pool and the
// works that define it are produced in one pass and cannot disagree about
// which works are cited. Ids already present in `have` are skipped: a full
// record is strictly more than this narrow one, and re-fetching it would
// pay for a downgrade.
//
// `known` is the pool a targeted run is merging INTO -- every id the cache
// already holds. A full sync passes nil, because the file it is about to
// replace holds nothing it can keep. A delta passes the base's ids, and
// that is most of what makes a delta cheap: a new paper's references are
// overwhelmingly works this corpus already cites.
func FetchCited(ctx context.Context, c Searcher, have []openalex.Work, known map[string]bool, opts Options) (CitedResult, error) {
	owned := make(map[string]bool, len(have))
	for i := range have {
		owned[shortID(have[i].ID)] = true
	}
	seen := map[string]bool{}
	var want []string
	for i := range have {
		for _, ref := range have[i].ReferencedWorks {
			id := shortID(ref)
			if id == "" || owned[id] || known[id] || seen[id] {
				continue
			}
			seen[id] = true
			want = append(want, id)
		}
	}
	out := CitedResult{Asked: len(want)}
	for _, batch := range lo.Chunk(want, doiBatch) {
		res, err := c.SearchWorks(ctx, openalex.SearchOpts{
			Filter:  map[string]string{"openalex_id": strings.Join(batch, "|")},
			PerPage: len(batch),
			Select:  CitedSelect,
		})
		out.Requests++
		if err != nil {
			return out, err
		}
		out.Works = append(out.Works, res.Results...)
		if opts.Progress != nil {
			opts.Progress(len(out.Works), len(want))
		}
	}
	return out, nil
}

// WriteCited writes the reference title pool and its sidecar, in the same
// body-then-sidecar order as Write.
func WriteCited(dir, scope string, res CitedResult) (string, error) {
	return writeNDJSON(dir, CitedFile, res.Works, Meta{
		ProducedAt:   time.Now().UTC().Format(time.RFC3339),
		ProducedBy:   "sci zot openalex sync (cited works)",
		Scope:        scope,
		RecordsTotal: len(res.Works),
		Select:       CitedSelect,
	})
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
	return writeNDJSON(dir, CacheFile, res.Works, Meta{
		ProducedAt:   time.Now().UTC().Format(time.RFC3339),
		ProducedBy:   "sci zot openalex sync",
		Scope:        scope,
		RecordsTotal: len(res.Works),
		Select:       WorkSelect,
		Stats:        res.Stats,
		NotFound:     res.NotFound,
	})
}

// writeNDJSON writes works as newline-delimited JSON and then the sidecar
// describing them.
func writeNDJSON(dir, name string, works []openalex.Work, meta Meta) (string, error) {
	lines := make([][]byte, 0, len(works))
	for i := range works {
		raw, err := json.Marshal(works[i])
		if err != nil {
			return "", fmt.Errorf("encode work %s: %w", works[i].ID, err)
		}
		lines = append(lines, raw)
	}
	return writeLines(dir, name, lines, meta)
}

// writeLines writes already-encoded NDJSON lines and then the sidecar
// describing them. The sidecar goes LAST and carries a digest of the bytes
// that actually landed, so a consumer finding a body without a matching
// sidecar knows it caught a partial write rather than guessing. meta's
// SHA256 is filled here -- a caller cannot know it before the write.
//
// Lines rather than works is what lets a delta merge: a record this run did
// not fetch is copied through as the bytes it already was, so nothing about
// it -- including fields this build's Work type does not model -- can be
// narrowed by a merge that never touched it.
func writeLines(dir, name string, lines [][]byte, meta Meta) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body := filepath.Join(dir, name)

	f, err := os.Create(body) //nolint:gosec // path is the caller's own --out
	if err != nil {
		return "", err
	}
	w := bufio.NewWriter(f)
	for _, line := range lines {
		// The newline is written separately, never appended to the line: a
		// base's lines are subslices of one buffer, and append() into their
		// spare capacity would scribble on the neighbouring record.
		if _, err := w.Write(line); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := w.WriteByte('\n'); err != nil {
			_ = f.Close()
			return "", err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	sum, err := digest(body)
	if err != nil {
		return "", err
	}
	meta.SHA256 = sum
	raw, err := json.MarshalIndent(meta, "", "  ")
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
