package cli

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/pdffind"
	"github.com/urfave/cli/v3"
)

// pdfs flag destinations.
var (
	pdfsCollection string
	pdfsDownload   string
	pdfsLimit      int
	pdfsNoCache    bool
	pdfsRefresh    bool
	pdfsParallel   int
	pdfsAttach     bool
	pdfsYes        bool
)

// defaultPDFParallel is the concurrency cap for --download. PDFs come from
// mixed hosts (arxiv, biorxiv, publisher CDNs), so 5 in flight gives most
// of the wall-time win without looking like abuse to any single origin.
const defaultPDFParallel = 5

// defaultPDFCollection is the collection name we assume when --collection is
// not passed. Matches the convention in CLAUDE.md and the user's library.
const defaultPDFCollection = "missing-pdf"

func pdfsCommand() *cli.Command {
	return &cli.Command{
		Name:  "pdfs",
		Usage: "Find retrievable PDFs on OpenAlex for items in a collection",
		Description: `$ sci zot --library personal doctor pdfs                          # scans default 'missing-pdf' collection
$ sci zot --library personal doctor pdfs --collection ABCD1234    # by key
$ sci zot --library personal doctor pdfs --collection missing-pdf # by name
$ sci zot --library personal doctor pdfs --download ~/pdfs        # also retrieve each PDF (5-way parallel)
$ sci zot --library personal doctor pdfs --download ~/pdfs -P 10  # bump download concurrency
$ sci zot --library personal doctor pdfs --download ~/pdfs --attach       # upload downloaded PDFs as Zotero child attachments
$ sci zot --library personal doctor pdfs --download ~/pdfs --attach --yes # skip confirmation
$ sci zot --library personal doctor pdfs --refresh                # bypass cache, re-query all
$ sci zot --library personal doctor pdfs --json > missing.json

For each item in the target collection, queries OpenAlex:
  - by DOI if present (deterministic),
  - else by title (top search hit, flagged as 'title-match').

Reports what OpenAlex has that Zotero doesn't: PDF URL, landing-page
URL, DOI, open-access status. With --download, each finding's PDFURL
is fetched into DIR as <itemKey>.pdf. HTTP errors and non-PDF
content-types (paywall HTML) are recorded per-item; the batch continues.

Lookups are cached on disk at <user cache>/sci/zot/pdffind, so reruns
are effectively free. --refresh re-queries everything; --no-cache
disables cache reads AND writes for the current run.

--attach is destructive: for every successfully downloaded PDF, it
creates an 'imported_file' child attachment on the parent item and
uploads the file bytes via Zotero's 4-phase upload dance. Requires
--download. Confirmation is required unless --yes is passed.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "collection",
				Aliases:     []string{"c"},
				Usage:       "collection name or key (default: missing-pdf)",
				Destination: &pdfsCollection,
				Local:       true,
			},
			&cli.StringFlag{
				Name:        "download",
				Aliases:     []string{"d"},
				Usage:       "retrieve each PDF to DIR (skipped if empty)",
				Destination: &pdfsDownload,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "limit",
				Aliases:     []string{"n"},
				Value:       25,
				Usage:       "max findings to print (0 = all; does not cap the scan)",
				Destination: &pdfsLimit,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "refresh",
				Usage:       "bypass cache reads, force a fresh OpenAlex lookup for every item",
				Destination: &pdfsRefresh,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "no-cache",
				Usage:       "disable on-disk cache for this run (no reads, no writes)",
				Destination: &pdfsNoCache,
				Local:       true,
			},
			&cli.IntFlag{
				Name:        "parallel",
				Aliases:     []string{"P"},
				Value:       defaultPDFParallel,
				Usage:       "concurrent downloads (with --download); 1 = serial",
				Destination: &pdfsParallel,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "attach",
				Usage:       "upload downloaded PDFs as Zotero child attachments (requires --download)",
				Destination: &pdfsAttach,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "yes",
				Aliases:     []string{"y"},
				Usage:       "with --attach, skip confirmation before writing",
				Destination: &pdfsYes,
				Local:       true,
			},
		},
		Action: runPDFs,
	}
}

func runPDFs(ctx context.Context, cmd *cli.Command) error {
	if pdfsAttach && pdfsDownload == "" {
		return cmdutil.UsageErrorf(cmd, "--attach requires --download")
	}

	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	name := pdfsCollection
	if name == "" {
		name = defaultPDFCollection
	}
	collKey, resolvedName, err := resolveCollectionKey(db, name)
	if err != nil {
		return err
	}

	items, err := db.ListAll(local.ListFilter{CollectionKey: collKey})
	if err != nil {
		return fmt.Errorf("list items in %q: %w", resolvedName, err)
	}

	oa, err := openalexClient()
	if err != nil {
		return err
	}

	cache, err := resolvePDFCache(pdfsNoCache)
	if err != nil {
		return err
	}

	res, err := scanWithProgress(ctx, items, oa, cache, pdfsRefresh)
	if err != nil {
		return err
	}

	out := pdffind.CLIResult{
		Collection:  resolvedName,
		Scanned:     res.Scanned,
		CacheHits:   res.CacheHits,
		CacheMisses: res.CacheMisses,
		Findings:    res.Findings,
		Limit:       pdfsLimit,
	}

	if pdfsDownload != "" && len(res.Findings) > 0 {
		// Give PDFs a reasonable ceiling — they're usually a few MB but some
		// are 100+. 5 minutes accommodates the largest without wedging the CLI.
		httpClient := &http.Client{Timeout: 5 * time.Minute}
		fresh, derr := downloadWithProgress(ctx, httpClient, res.Findings, pdfsDownload, pdfsParallel)
		if derr != nil {
			return derr
		}
		out.Findings = fresh
		out.Downloaded = true
	}

	if pdfsAttach {
		attachable := lo.CountBy(out.Findings, func(f pdffind.Finding) bool { return f.DownloadedPath != "" })
		if attachable == 0 {
			// Render what we have; there's simply nothing to upload.
			outputScoped(ctx, cmd, out)
			return nil
		}
		prompt := fmt.Sprintf("Upload %d PDF(s) as Zotero child attachments?", attachable)
		if done, err := cmdutil.ConfirmOrSkip(pdfsYes, prompt); done || err != nil {
			return err
		}
		w, err := requireAPIClient(ctx)
		if err != nil {
			return err
		}
		fresh, aerr := attachWithProgress(ctx, w, out.Findings)
		if aerr != nil {
			return aerr
		}
		out.Findings = fresh
		out.Attached = true
	}

	outputScoped(ctx, cmd, out)
	return nil
}

// attachWithProgress wraps pdffind.Attach with a progress bar, mirroring the
// shape of downloadWithProgress. Counters: 'attached' for success,
// 'partial' when the item was created but the upload failed, and 'failed'
// for complete misses.
func attachWithProgress(
	ctx context.Context,
	w pdffind.Attacher,
	findings []pdffind.Finding,
) ([]pdffind.Finding, error) {
	total := lo.CountBy(findings, func(f pdffind.Finding) bool { return f.DownloadedPath != "" })
	if total == 0 {
		return findings, nil
	}
	var out []pdffind.Finding
	err := uikit.RunWithProgress("Attaching to Zotero", func(t *uikit.ProgressTracker) error {
		t.SetTotal(total)
		opts := pdffind.AttachOptions{
			OnStart: func(_, _ int, f pdffind.Finding) {
				t.Status(fmt.Sprintf("%s  %s", f.ItemKey, truncateMiddle(f.Title, 70)))
			},
			OnDone: func(_, _ int, f pdffind.Finding) {
				counter := "attached"
				switch {
				case f.AttachError != "" && f.AttachmentKey != "":
					counter = "partial"
				case f.AttachError != "":
					counter = "failed"
				}
				t.Advance(counter, f.ItemKey)
			},
		}
		fresh, aerr := pdffind.Attach(ctx, w, findings, opts)
		out = fresh
		return aerr
	})
	return out, err
}

// resolvePDFCache returns the shared on-disk cache, or nil when disabled.
// A failure to resolve the user cache dir downgrades gracefully to "no cache"
// rather than aborting — caching is an optimization, not a correctness knob.
func resolvePDFCache(disabled bool) (*pdffind.Cache, error) {
	if disabled {
		return nil, nil
	}
	dir, err := pdffind.DefaultCacheDir()
	if err != nil {
		return nil, nil //nolint:nilerr // soft failure; downgrade to no-cache
	}
	return &pdffind.Cache{Dir: dir}, nil
}

// scanWithProgress wraps pdffind.Scan in a uikit progress bar. Counters:
// 'hit' for cache hits, 'miss' for live lookups, 'fail' for lookup errors.
// The progress bar shares the terminal line so --json users (stderr-only)
// don't get mixed in with their JSON.
func scanWithProgress(
	ctx context.Context,
	items []local.Item,
	oa pdffind.Lookup,
	cache *pdffind.Cache,
	refresh bool,
) (*pdffind.Result, error) {
	var out *pdffind.Result
	err := uikit.RunWithProgress("Looking up PDFs on OpenAlex", func(t *uikit.ProgressTracker) error {
		t.SetTotal(len(items))
		var inner error
		out, inner = pdffind.Scan(ctx, items, oa, pdffind.ScanOptions{
			Cache:   cache,
			Refresh: refresh,
			OnItem: func(_, _ int, f pdffind.Finding, hit bool) {
				// One dimension for the progress bar: cache hit vs. miss.
				// Lookup success/failure is orthogonal and shows in the
				// final result table — don't try to cram it in here (cached
				// failures would otherwise mis-count as misses).
				counter := "fetched"
				if hit {
					counter = "cached"
				}
				t.Advance(counter, f.ItemKey)
			},
		})
		return inner
	})
	return out, err
}

// downloadWithProgress wraps pdffind.Download with a progress bar. Uses
// DownloadOptions callbacks so the current item/URL is shown live — a slow
// server doesn't look like a hang.
func downloadWithProgress(
	ctx context.Context,
	httpClient *http.Client,
	findings []pdffind.Finding,
	dir string,
	parallel int,
) ([]pdffind.Finding, error) {
	total := lo.CountBy(findings, func(f pdffind.Finding) bool { return f.PDFURL != "" })
	if total == 0 {
		return findings, nil
	}
	var out []pdffind.Finding
	err := uikit.RunWithProgress("Downloading PDFs", func(t *uikit.ProgressTracker) error {
		t.SetTotal(total)
		opts := pdffind.DownloadOptions{
			Parallel: parallel,
			OnStart: func(_, _ int, f pdffind.Finding) {
				// Last-called-wins on the status line — with parallelism, this
				// shows whichever fetch most recently started. Good enough for
				// "is it alive" feedback.
				t.Status(fmt.Sprintf("%s  %s", f.ItemKey, truncateMiddle(f.PDFURL, 70)))
			},
			OnDone: func(_, _ int, f pdffind.Finding) {
				counter := "saved"
				if f.DownloadError != "" {
					counter = "failed"
				}
				t.Advance(counter, f.ItemKey)
			},
		}
		fresh, derr := pdffind.Download(ctx, httpClient, findings, dir, opts)
		out = fresh
		return derr
	})
	return out, err
}

// truncateMiddle shortens a URL for the progress status line while keeping
// both the host prefix and filename suffix visible — "https://cdn.example.
// org/…/paper.pdf" reads better than a left-truncated tail.
func truncateMiddle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max/2 - 1
	tail := max - head - 1
	return s[:head] + "…" + s[len(s)-tail:]
}

// zoteroKeyRE matches an 8-character Zotero-style key. Used to decide whether
// --collection should be looked up as a name or used verbatim as a key.
var zoteroKeyRE = regexp.MustCompile(`^[A-Z0-9]{8}$`)

// resolveCollectionKey accepts either a Zotero collection key or a collection
// name (case-insensitive) and returns (key, displayName, err).
//
// Name collisions (two collections with the same name, e.g. nested) are
// flagged as an error with both keys listed so the user can disambiguate.
func resolveCollectionKey(db local.Reader, input string) (key, displayName string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("collection is required")
	}
	if zoteroKeyRE.MatchString(input) {
		// Treat as key; look up the name for a friendlier display, but don't
		// fail if the key isn't found locally — let downstream ListAll surface
		// an empty result set if the user typo'd.
		cols, lerr := db.ListCollections()
		if lerr != nil {
			return input, input, nil // best-effort; return the key as display.
		}
		if c, ok := lo.Find(cols, func(c local.Collection) bool { return c.Key == input }); ok {
			return c.Key, c.Name, nil
		}
		return input, input, nil
	}

	cols, lerr := db.ListCollections()
	if lerr != nil {
		return "", "", fmt.Errorf("list collections: %w", lerr)
	}
	matches := lo.Filter(cols, func(c local.Collection, _ int) bool {
		return strings.EqualFold(c.Name, input)
	})
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("collection %q not found (use 'sci zot collection list' to see names)", input)
	case 1:
		return matches[0].Key, matches[0].Name, nil
	default:
		keys := lo.Map(matches, func(c local.Collection, _ int) string { return c.Key })
		return "", "", fmt.Errorf("collection name %q is ambiguous — multiple matches: %s (pass --collection <key> instead)", input, strings.Join(keys, ", "))
	}
}
