package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/version"
)

// skipIfLoopbackFlake skips the test when result.Error indicates a transient
// loopback connectivity failure (macOS firewall, VPN interference, etc.).
func skipIfLoopbackFlake(t *testing.T, resultErr string) {
	t.Helper()
	if resultErr == "" {
		return
	}
	for _, sig := range []string{"operation timed out", "context deadline exceeded", "connection refused"} {
		if strings.Contains(resultErr, sig) {
			t.Skipf("loopback httptest server unreachable: %s", resultErr)
		}
	}
}

func TestCheckDevBuild(t *testing.T) {
	old := version.Commit
	version.Commit = "unknown"
	defer func() { version.Commit = old }()

	result := Check()
	if result.Available {
		t.Error("dev build should not report update available")
	}
	if result.Error == "" {
		t.Error("expected error for dev build")
	}
}

// TestCommitsDiffer exercises the pure SHA-comparison logic that Check() uses
// to decide whether an update is available. This covers the logic without any
// network calls.
func TestCommitsDiffer(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool // true = update available
	}{
		{
			name:    "identical full SHAs",
			current: "abc1234def5678",
			latest:  "abc1234def5678",
			want:    false,
		},
		{
			name:    "short current is prefix of full latest",
			current: "abc1234",
			latest:  "abc1234def5678",
			want:    false,
		},
		{
			name:    "full current is prefix of short latest",
			current: "abc1234def5678",
			latest:  "abc1234",
			want:    false,
		},
		{
			name:    "different SHAs",
			current: "abc1234",
			latest:  "def5678",
			want:    true,
		},
		{
			name:    "completely different full SHAs",
			current: "aaaaaaa1111111",
			latest:  "bbbbbbb2222222",
			want:    true,
		},
		{
			name:    "empty current vs non-empty latest",
			current: "",
			latest:  "abc1234",
			want:    false, // empty string is prefix of everything
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commitsDiffer(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("commitsDiffer(%q, %q) = %v, want %v",
					tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// TestCheckUpToDate uses a local HTTP server to simulate a GitHub release
// response where the release SHA matches the running binary's commit.
// It verifies that Check() returns Available=false in that case.
func TestCheckUpToDate(t *testing.T) {
	const sha = "abc1234def5678"

	// Serve a minimal GitHub-releases-API-shaped response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := `{"body":"**Commit:** ` + sha + `","assets":[]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// Point the package at the test server.
	oldURL := releaseURL
	releaseURL = srv.URL
	defer func() { releaseURL = oldURL }()

	// Set the binary commit to match the release.
	oldCommit := version.Commit
	version.Commit = sha
	defer func() { version.Commit = oldCommit }()

	result := Check()
	skipIfLoopbackFlake(t, result.Error)
	if result.Available {
		t.Errorf("Available = true; want false when binary commit matches release commit")
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.CurrentSHA != sha {
		t.Errorf("CurrentSHA = %q, want %q", result.CurrentSHA, sha)
	}
	if result.LatestSHA != sha {
		t.Errorf("LatestSHA = %q, want %q", result.LatestSHA, sha)
	}
}

// TestCheckUpdateAvailable uses a local HTTP server to simulate a GitHub
// release with a different SHA than the running binary.
func TestCheckUpdateAvailable(t *testing.T) {
	const latestSHA = "bbbbbbb2222222"
	const currentSHA = "aaaaaaa1111111"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := `{"body":"**Commit:** ` + latestSHA + `","assets":[]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	oldURL := releaseURL
	releaseURL = srv.URL
	defer func() { releaseURL = oldURL }()

	oldCommit := version.Commit
	version.Commit = currentSHA
	defer func() { version.Commit = oldCommit }()

	result := Check()
	skipIfLoopbackFlake(t, result.Error)
	if !result.Available {
		t.Errorf("Available = false; want true when binary commit differs from release commit")
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.LatestSHA != latestSHA {
		t.Errorf("LatestSHA = %q, want %q", result.LatestSHA, latestSHA)
	}
}

func TestDownload(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(binaryContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "sci-test")
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}

	var lastProgress int64
	err = Download(srv.URL, f, func(n int64) { lastProgress = n })
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(binaryContent) {
		t.Errorf("got %q, want %q", string(data), string(binaryContent))
	}
	if lastProgress != int64(len(binaryContent)) {
		t.Errorf("progress = %d, want %d", lastProgress, len(binaryContent))
	}
}

func TestProgressReader(t *testing.T) {
	data := []byte("hello world")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "test")
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}

	var calls int
	err = Download(srv.URL, f, func(_ int64) { calls++ })
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Error("progressFn was never called")
	}
}
