package pdffind

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

// DownloadOptions configures Download. Zero value is valid — no callbacks,
// serial execution.
type DownloadOptions struct {
	// OnStart fires just before each HTTP fetch begins. Use it to surface
	// "currently fetching X" so a slow server doesn't look like a hang.
	// i is 0-based into the fetchable subset; total counts only findings
	// with a non-empty PDFURL (skipped items don't advance i or total).
	//
	// SAFETY: when Parallel > 1, callbacks may fire concurrently from any
	// goroutine. Callers are responsible for synchronization if they
	// mutate shared state — the bundled uikit.ProgressTracker is safe.
	OnStart func(i, total int, f Finding)

	// OnDone fires after each fetch completes (success or error).
	// The Finding passed in carries the final DownloadedPath or
	// DownloadError — use it to advance a progress counter.
	OnDone func(i, total int, f Finding)

	// Parallel caps the number of concurrent fetches. 0 or 1 = serial,
	// preserving deterministic order; higher values use errgroup with
	// SetLimit(N). PDFs come from independent hosts in practice, so 5-8
	// is a polite default that gives most of the speedup.
	Parallel int
}

// Download fetches every finding's PDFURL to dir, mutating each Finding's
// DownloadedPath or DownloadError in place. Returns the mutated slice.
//
// Per-item failures (HTTP 404, non-PDF content type, write errors) are
// recorded on the Finding and do NOT abort the batch. Context cancellation
// DOES abort, returning ctx.Err().
//
// Findings with an empty PDFURL are passed through untouched — "no URL to
// try" is handled upstream in Scan via LookupError.
func Download(
	ctx context.Context,
	httpClient *http.Client,
	findings []Finding,
	dir string,
	opts DownloadOptions,
) ([]Finding, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("pdffind: create download dir: %w", err)
	}
	total := lo.CountBy(findings, func(f Finding) bool { return f.PDFURL != "" })

	// Build a parallel task list: (findingIndex, fetchIndex) for every
	// finding with a PDFURL. fetchIndex is the 0..total-1 position used by
	// the callbacks; findingIndex writes back into the original slice.
	type job struct{ findingIdx, fetchIdx int }
	var jobs []job
	fetchIdx := 0
	for i := range findings {
		if findings[i].PDFURL == "" {
			continue
		}
		jobs = append(jobs, job{findingIdx: i, fetchIdx: fetchIdx})
		fetchIdx++
	}

	runJob := func(j job) {
		if opts.OnStart != nil {
			opts.OnStart(j.fetchIdx, total, findings[j.findingIdx])
		}
		path, derr := downloadOne(ctx, httpClient, findings[j.findingIdx], dir)
		if derr != nil {
			findings[j.findingIdx].DownloadError = derr.Error()
		} else {
			findings[j.findingIdx].DownloadedPath = path
		}
		if opts.OnDone != nil {
			opts.OnDone(j.fetchIdx, total, findings[j.findingIdx])
		}
	}

	if opts.Parallel <= 1 {
		for _, j := range jobs {
			if err := ctx.Err(); err != nil {
				return findings, err
			}
			runJob(j)
		}
		return findings, nil
	}

	// Parallel path. Use WithContext so ctx cancellation propagates cleanly
	// to every in-flight http request. Per-item errors are recorded on the
	// Finding, not returned — so SetLimit's "first-error-cancels-rest"
	// semantics never trigger on a per-PDF 404.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Parallel)
	for _, j := range jobs {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			runJob(j)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return findings, err
	}
	return findings, nil
}

func downloadOne(ctx context.Context, httpClient *http.Client, f Finding, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", f.PDFURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/pdf")
	req.Header.Set("User-Agent", "sci-zot (+https://github.com/sciminds/cli)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	// Some CDNs send "application/pdf; charset=binary" or octet-stream. Accept
	// both; reject obvious HTML paywalls.
	if !strings.Contains(ct, "application/pdf") && !strings.Contains(ct, "application/octet-stream") {
		return "", fmt.Errorf("unexpected content-type %q (got HTML / paywall?)", ct)
	}

	name := filepath.Join(dir, sanitizeFilename(f.ItemKey)+".pdf")
	out, err := os.Create(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = os.Remove(name) // don't leave a half-written file masquerading as a PDF
		return "", err
	}
	return name, nil
}

// sanitizeFilename trims to chars safe for common filesystems. Zotero keys
// are [A-Z0-9]{8} so this is defense-in-depth, not hygiene — but we also
// want to tolerate future callers that pass titles through.
func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unnamed"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}, s)
}
