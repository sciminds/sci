package pdffind

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownload_SavesPDFToDir(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4\nhello"))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	findings := []Finding{
		{ItemKey: "ABC123", PDFURL: srv.URL + "/a.pdf"},
	}
	out, err := Download(context.Background(), srv.Client(), findings, dir, DownloadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 finding, got %d", len(out))
	}
	if out[0].DownloadedPath == "" {
		t.Fatal("downloaded_path not set")
	}
	if !strings.HasPrefix(filepath.Base(out[0].DownloadedPath), "ABC123") {
		t.Errorf("expected filename to lead with item key, got %q", filepath.Base(out[0].DownloadedPath))
	}
	body, err := os.ReadFile(out[0].DownloadedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "%PDF") {
		t.Errorf("file doesn't look like a pdf: %q", body[:min(20, len(body))])
	}
}

func TestDownload_SkipsFindingsWithoutPDFURL(t *testing.T) {
	t.Parallel()
	findings := []Finding{{ItemKey: "ABC"}}
	dir := t.TempDir()
	out, err := Download(context.Background(), http.DefaultClient, findings, dir, DownloadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].DownloadedPath != "" || out[0].DownloadError != "" {
		t.Errorf("skipped finding must be untouched, got %+v", out[0])
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("no files should be written, got %d", len(entries))
	}
}

func TestDownload_RecordsHTTPErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	findings := []Finding{{ItemKey: "ABC", PDFURL: srv.URL + "/a.pdf"}}
	out, err := Download(context.Background(), srv.Client(), findings, dir, DownloadOptions{})
	if err != nil {
		t.Fatal(err) // per-item errors must not abort the whole batch
	}
	if out[0].DownloadError == "" {
		t.Error("want download_error set on HTTP 404")
	}
	if out[0].DownloadedPath != "" {
		t.Error("downloaded_path must stay empty on error")
	}
}

func TestDownload_RejectsNonPDFContentType(t *testing.T) {
	t.Parallel()
	// Publisher landing-page redirects that return HTML are the common trap
	// — we want a clear error rather than a "pdf" file full of <html>.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>paywall</html>"))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	findings := []Finding{{ItemKey: "ABC", PDFURL: srv.URL + "/a.pdf"}}
	out, err := Download(context.Background(), srv.Client(), findings, dir, DownloadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].DownloadError == "" {
		t.Error("want download_error on non-PDF content-type")
	}
}

func TestDownload_ParallelOverlapsRequests(t *testing.T) {
	t.Parallel()
	// Server blocks until N concurrent requests are in flight at once.
	// Serial execution would deadlock; parallelism releases everyone.
	const N = 4
	var mu sync.Mutex
	var inflight int
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inflight++
		reached := inflight == N
		mu.Unlock()
		if reached {
			close(released)
		}
		select {
		case <-released:
		case <-time.After(2 * time.Second):
			t.Error("timed out waiting for parallel fan-in — downloads not actually parallel")
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF"))
	}))
	t.Cleanup(srv.Close)

	findings := make([]Finding, N)
	for i := range findings {
		findings[i] = Finding{
			ItemKey: fmt.Sprintf("K%d", i),
			PDFURL:  srv.URL + fmt.Sprintf("/%d.pdf", i),
		}
	}
	out, err := Download(context.Background(), srv.Client(), findings, t.TempDir(), DownloadOptions{Parallel: N})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out {
		if f.DownloadedPath == "" {
			t.Errorf("expected every file downloaded, got %+v", f)
		}
	}
}

func TestDownload_FiresCallbacksPerItemAndSkipsEmptyURLs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF"))
	}))
	t.Cleanup(srv.Close)

	findings := []Finding{
		{ItemKey: "A", PDFURL: srv.URL + "/a.pdf"},
		{ItemKey: "B"}, // no URL — callbacks must skip
		{ItemKey: "C", PDFURL: srv.URL + "/c.pdf"},
	}
	var starts, dones []string
	opts := DownloadOptions{
		OnStart: func(_, total int, f Finding) {
			starts = append(starts, f.ItemKey)
			if total != 2 {
				t.Errorf("total should count only fetchable findings, got %d", total)
			}
		},
		OnDone: func(_, _ int, f Finding) {
			dones = append(dones, f.ItemKey)
		},
	}
	_, err := Download(context.Background(), srv.Client(), findings, t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(starts, ","), "A,C"; got != want {
		t.Errorf("starts: got %q, want %q", got, want)
	}
	if got, want := strings.Join(dones, ","), "A,C"; got != want {
		t.Errorf("dones: got %q, want %q", got, want)
	}
}
