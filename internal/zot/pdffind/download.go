package pdffind

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Download fetches every finding's PDFURL to dir, mutating each Finding's
// DownloadedPath or DownloadError in place. Returns the mutated slice.
//
// Per-item failures (HTTP 404, non-PDF content type, write errors) are
// recorded on the Finding and do NOT abort the batch. Context cancellation
// DOES abort, returning ctx.Err().
//
// Findings with an empty PDFURL are passed through untouched — "no URL to
// try" is handled upstream in Scan via LookupError.
func Download(ctx context.Context, httpClient *http.Client, findings []Finding, dir string) ([]Finding, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("pdffind: create download dir: %w", err)
	}
	for i := range findings {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		if findings[i].PDFURL == "" {
			continue
		}
		path, derr := downloadOne(ctx, httpClient, findings[i], dir)
		if derr != nil {
			findings[i].DownloadError = derr.Error()
			continue
		}
		findings[i].DownloadedPath = path
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
