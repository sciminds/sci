package pdffind

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	out, err := Download(context.Background(), srv.Client(), findings, dir)
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
	out, err := Download(context.Background(), http.DefaultClient, findings, dir)
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
	out, err := Download(context.Background(), srv.Client(), findings, dir)
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
	out, err := Download(context.Background(), srv.Client(), findings, dir)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].DownloadError == "" {
		t.Error("want download_error on non-PDF content-type")
	}
}
