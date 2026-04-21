// Package pdffind queries OpenAlex for Zotero items that are missing their
// PDF attachments and reports what's retrievable — PDF URL, landing-page URL,
// DOI, open-access status — plus (optionally) downloads the PDF.
//
// Read-only on the Zotero side: this package only reads item metadata and
// emits findings. Writing discovered metadata back to Zotero (fill DOI, fill
// URL) is a follow-up that can reuse enrich.Apply; attaching the downloaded
// file as a child attachment is a separate workstream requiring new Writer
// methods for Zotero's 3-step file-upload dance.
package pdffind

import (
	"context"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// Finding is the per-item output of a Scan. One Finding is emitted for every
// input item, even ones we couldn't resolve — failures are recorded in
// LookupError so the caller (a human-readable report) can show the full set.
type Finding struct {
	ItemKey string `json:"item_key"`
	Title   string `json:"title,omitempty"`

	// Local state we used to drive the lookup.
	LocalDOI string `json:"local_doi,omitempty"`

	// How we reached OpenAlex: "doi" (deterministic), "title" (top search hit,
	// not verified), or "" (didn't attempt — no DOI + no title).
	LookupMethod string `json:"lookup_method,omitempty"`

	// OpenAlex-side facts.
	OpenAlexID     string `json:"openalex_id,omitempty"`
	OADOI          string `json:"oa_doi,omitempty"` // DOI per OpenAlex, sans https://doi.org/ prefix
	PDFURL         string `json:"pdf_url,omitempty"`
	LandingPageURL string `json:"landing_page_url,omitempty"`
	IsOA           bool   `json:"is_oa"`
	OAStatus       string `json:"oa_status,omitempty"`
	HasFulltext    bool   `json:"has_fulltext"`

	// FallbackURLs are additional PDF URLs to try if PDFURL fails. OpenAlex
	// often returns a paywall URL as best_oa_location while also listing
	// an arxiv/PMC preprint in locations[]; Download walks PDFURL then
	// each FallbackURL until one succeeds. Ranked so known-friendly hosts
	// (preprint servers, PMC, institutional repos) come first.
	FallbackURLs []string `json:"fallback_urls,omitempty"`

	// Set by Download after --download; non-empty means the file is on disk.
	DownloadedPath string `json:"downloaded_path,omitempty"`
	DownloadError  string `json:"download_error,omitempty"`

	// Set by Attach after --attach. AttachmentKey is the Zotero key of the
	// newly-created child attachment; AttachError is set on any failure
	// during the 4-phase upload dance.
	AttachmentKey string `json:"attachment_key,omitempty"`
	AttachError   string `json:"attach_error,omitempty"`

	// Set when lookup itself failed (404, timeout, no title match, etc.).
	LookupError string `json:"lookup_error,omitempty"`
}

// Lookup is the narrow OpenAlex contract — *openalex.Client satisfies it.
// Kept as an interface so tests can stub without an HTTP server.
type Lookup interface {
	ResolveWork(ctx context.Context, identifier string) (*openalex.Work, error)
	SearchWorks(ctx context.Context, opts openalex.SearchOpts) (*openalex.Results[openalex.Work], error)
}

// Result aggregates per-item findings from one Scan call.
type Result struct {
	Scanned  int       `json:"scanned"`
	Findings []Finding `json:"findings"`

	// CacheHits / CacheMisses describe how many items resolved from the cache
	// vs. a fresh OpenAlex call. Authoritative, regardless of whether the
	// progress bar managed to render intermediate updates.
	CacheHits   int `json:"cache_hits"`
	CacheMisses int `json:"cache_misses"`
}

// titleSearchSelect trims the /works response to fields we actually consume,
// mirroring the pattern in cli/find.go — saves ~80% of response bytes.
var titleSearchSelect = []string{
	"id", "doi", "title", "display_name",
	"is_oa", "has_fulltext", "open_access",
	"primary_location", "best_oa_location", "locations",
}

// friendlyHostSubstrings match PDF hosts that reliably serve files to bot
// UAs — preprint servers, PMC, OSF, and other open repositories. PDF URLs
// matching any of these get promoted ahead of commercial-publisher URLs
// in the try-order so a paper with both an OA preprint and a Wiley paywall
// tries the preprint first.
var friendlyHostSubstrings = []string{
	"arxiv.org",
	"biorxiv.org",
	"medrxiv.org",
	"chemrxiv.org",
	"psyarxiv.com",
	"osf.io",
	"pmc.ncbi.nlm.nih.gov",
	"europepmc.org",
	"ncbi.nlm.nih.gov",
	"repository.", // repository.upenn.edu, repository.cam.ac.uk, etc.
	"dspace.",     // dspace.mit.edu, etc.
}

func isFriendlyHost(url string) bool {
	lu := strings.ToLower(url)
	return lo.ContainsBy(friendlyHostSubstrings, func(sub string) bool {
		return strings.Contains(lu, sub)
	})
}

// ScanOptions configures Scan. Zero value is valid — no cache, no progress.
type ScanOptions struct {
	// Cache, if non-nil, is consulted before hitting OpenAlex. Both hits and
	// misses (including lookup errors) are written through so retries are
	// free and failures aren't re-tried until --refresh.
	Cache *Cache

	// Refresh, when true, skips the cache read but still writes results back.
	// Used by --refresh to force-refetch known-stale findings.
	Refresh bool

	// OnItem is called after each item resolves (cache hit or live lookup),
	// with cacheHit=true for cache hits. Use it to drive a progress bar.
	// Safe to be nil.
	OnItem func(i, total int, f Finding, cacheHit bool)
}

// Scan queries OpenAlex once per item. Items with a local DOI use
// ResolveWork (deterministic); others fall back to a title search and we
// take the top hit. The caller should inspect Finding.LookupMethod to
// distinguish high-confidence DOI hits from approximate title matches.
//
// Failures are recorded per-item — the whole scan never aborts on a single
// 404 or network blip. Context cancellation DOES abort, returning ctx.Err().
//
// When opts.Cache is set, every item's lookup result is persisted so reruns
// skip the network entirely; pass opts.Refresh=true to bypass cache reads.
func Scan(ctx context.Context, items []local.Item, oa Lookup, opts ScanOptions) (*Result, error) {
	findings := make([]Finding, 0, len(items))
	var hits, misses int
	for i, it := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, hit := resolveOne(ctx, it, oa, opts)
		findings = append(findings, f)
		if hit {
			hits++
		} else {
			misses++
		}
		if opts.OnItem != nil {
			opts.OnItem(i, len(items), f, hit)
		}
	}
	return &Result{
		Scanned:     len(items),
		Findings:    findings,
		CacheHits:   hits,
		CacheMisses: misses,
	}, nil
}

// resolveOne combines cache lookup with live OpenAlex fallback. Cache keys
// mirror the identifier we'd send to OpenAlex — DOI when present, else the
// title — so cache entries stay useful even if the caller later adds a DOI
// to a previously title-matched item (different key, clean miss).
func resolveOne(ctx context.Context, it local.Item, oa Lookup, opts ScanOptions) (Finding, bool) {
	key := cacheKeyFor(it)
	if !opts.Refresh && key != "" {
		if cached, ok := opts.Cache.Get(key); ok {
			// Re-overlay the local ItemKey/Title on the cached payload — the
			// OpenAlex-side fields are what we paid for, but local identifiers
			// might have drifted (renames, re-imports).
			cached.ItemKey = it.Key
			cached.Title = it.Title
			cached.LocalDOI = it.DOI
			return cached, true
		}
	}
	f := lookupOne(ctx, it, oa)
	if key != "" {
		opts.Cache.Put(key, f)
	}
	return f, false
}

// cacheKeyFor returns the query we'd send to OpenAlex for this item.
// Empty string means "no usable identifier" — such items are never cached.
func cacheKeyFor(it local.Item) string {
	if it.DOI != "" {
		return "doi:" + it.DOI
	}
	if strings.TrimSpace(it.Title) != "" {
		return "title:" + it.Title
	}
	return ""
}

func lookupOne(ctx context.Context, it local.Item, oa Lookup) Finding {
	f := Finding{ItemKey: it.Key, Title: it.Title, LocalDOI: it.DOI}

	switch {
	case it.DOI != "":
		w, err := oa.ResolveWork(ctx, it.DOI)
		if err != nil {
			f.LookupError = "doi lookup: " + err.Error()
			return f
		}
		f.LookupMethod = "doi"
		populateFromWork(&f, w)
	case strings.TrimSpace(it.Title) != "":
		res, err := oa.SearchWorks(ctx, openalex.SearchOpts{
			Search:  it.Title,
			PerPage: 1,
			Select:  titleSearchSelect,
		})
		if err != nil {
			f.LookupError = "title search: " + err.Error()
			return f
		}
		if len(res.Results) == 0 {
			f.LookupError = "no title match"
			return f
		}
		f.LookupMethod = "title"
		populateFromWork(&f, &res.Results[0])
	default:
		f.LookupError = "no doi or title to look up"
	}
	return f
}

// populateFromWork copies the fields we care about out of an OpenAlex Work
// onto a Finding.
//
// PDF URL selection: we gather every pdf_url OpenAlex knows about (from
// locations[], best_oa_location, primary_location), deduplicate, and rank
// friendly-host URLs ahead of unknown ones. The first ranked URL becomes
// PDFURL; the rest are FallbackURLs for the downloader to try on failure.
// This matters because OpenAlex's best_oa_location is often a paywalled
// publisher URL when there's ALSO an arxiv/PMC preprint listed in
// locations[] — without the reordering, we'd hit the paywall and give up.
func populateFromWork(f *Finding, w *openalex.Work) {
	f.OpenAlexID = openalexShortID(w.ID)
	if w.DOI != nil {
		f.OADOI = stripDOIPrefix(*w.DOI)
	}
	f.IsOA = w.IsOA
	f.HasFulltext = w.HasFulltext
	if w.OpenAccess != nil {
		f.OAStatus = w.OpenAccess.OAStatus
	}
	if w.PrimaryLocation != nil && w.PrimaryLocation.LandingPageURL != nil {
		f.LandingPageURL = *w.PrimaryLocation.LandingPageURL
	}

	urls := gatherPDFURLs(w)
	if len(urls) == 0 {
		return
	}
	f.PDFURL = urls[0]
	if len(urls) > 1 {
		f.FallbackURLs = urls[1:]
	}
}

// gatherPDFURLs returns every PDF URL OpenAlex lists for this Work,
// deduplicated and ranked with friendly hosts first. Order-preserving
// within each rank so OpenAlex's own preference breaks ties.
func gatherPDFURLs(w *openalex.Work) []string {
	var all []string
	add := func(loc *openalex.Location) {
		if loc != nil && loc.PDFURL != nil && *loc.PDFURL != "" {
			all = append(all, *loc.PDFURL)
		}
	}
	// Order here seeds the intra-rank order: best_oa first, then primary,
	// then the full locations[] array.
	add(w.BestOALocation)
	add(w.PrimaryLocation)
	for i := range w.Locations {
		add(&w.Locations[i])
	}
	all = lo.Uniq(all)

	// Stable partition: friendly hosts first, unknown hosts after.
	friendly := lo.Filter(all, func(u string, _ int) bool { return isFriendlyHost(u) })
	unknown := lo.Filter(all, func(u string, _ int) bool { return !isFriendlyHost(u) })
	return append(friendly, unknown...)
}

// openalexShortID pulls "W12345" out of "https://openalex.org/W12345".
// Duplicated (rather than imported) from internal/zot/enrich to avoid an
// internal cycle — enrich depends on api + client, pdffind deliberately
// doesn't.
func openalexShortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// stripDOIPrefix removes the "https://doi.org/" prefix OpenAlex returns on
// its doi field, leaving the bare "10.xxx/yyy" form Zotero stores.
func stripDOIPrefix(doi string) string {
	for _, p := range []string{"https://doi.org/", "http://doi.org/"} {
		if strings.HasPrefix(doi, p) {
			return doi[len(p):]
		}
	}
	return doi
}

// CountWithPDF returns the number of findings with a non-empty PDFURL.
// Handy for the summary line in the human renderer.
func CountWithPDF(r *Result) int {
	return lo.CountBy(r.Findings, func(f Finding) bool { return f.PDFURL != "" })
}
