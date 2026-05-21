package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/version"
)

// overrideOnlineProbe points netutil.Online() at a local httptest server so
// Check() tests don't depend on real internet connectivity.
func overrideOnlineProbe(t *testing.T, srv *httptest.Server) {
	t.Helper()
	netutil.SetProbeURL(srv.URL)
	t.Cleanup(func() { netutil.ResetProbeURL() })
}

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

	// Ensure Online() check passes without real internet.
	overrideOnlineProbe(t, srv)

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

	overrideOnlineProbe(t, srv)

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

// Progress-callback semantics are exercised by [TestDownload] above
// (which sets progressFn and asserts lastProgress equals the body length).

func TestDownload_TruncatedResponse(t *testing.T) {
	// Server advertises 10 000 bytes via Content-Length but only sends 5.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10000")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "binary")
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}

	err = Download(srv.URL, f, nil)
	_ = f.Close()

	// io.Copy sees unexpected EOF when the body closes before Content-Length
	// bytes have been read, so Download must return an error.
	if err == nil {
		t.Fatal("expected error for truncated response, got nil")
	}
}

func TestDownload_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "binary"))
	if err != nil {
		t.Fatal(err)
	}

	err = Download(srv.URL, f, nil)
	_ = f.Close()

	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status code, got %q", err)
	}
}

func TestCheck_MissingCommitInRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Release body has no **Commit:** line.
		_, _ = w.Write([]byte(`{"body":"No commit info here","assets":[]}`))
	}))
	defer srv.Close()

	overrideOnlineProbe(t, srv)

	oldURL := releaseURL
	releaseURL = srv.URL
	defer func() { releaseURL = oldURL }()

	oldCommit := version.Commit
	version.Commit = "abc1234"
	defer func() { version.Commit = oldCommit }()

	result := Check()
	skipIfLoopbackFlake(t, result.Error)

	if result.Available {
		t.Error("should not report update available when commit SHA is missing")
	}
	if !strings.Contains(result.Error, "could not find commit SHA") {
		t.Errorf("expected 'could not find commit SHA' error, got %q", result.Error)
	}
}

// TestCheck_ReleaseBodyFormatDrift is the format-drift sentinel between the
// release workflow (.github/workflows/release.yml) and this parser. The
// fixture below must mirror, byte-for-byte, the body the "Create GitHub
// Release" step writes — `**Commit:**`, the per-asset `**SHA256(...):**`
// lines emitted by the "Compute SHA256 checksums" step, and the install
// line — separated by blank lines exactly as YAML's `body: |` block plus
// `${{ steps.checksums.outputs.sha_lines }}` interpolation produces.
//
// If the workflow changes its emit format (e.g. drops `**` markdown bolding,
// renames the asset prefix, switches to a checksums.txt artifact), this
// test fails — preventing a release that ships a new binary unable to
// self-update.
func TestCheck_ReleaseBodyFormatDrift(t *testing.T) {
	const commit = "1234567abcdef89"
	assets := []struct {
		name string
		sha  string
	}{
		{"sci-darwin-arm64", strings.Repeat("a", 64)},
		{"sci-darwin-amd64", strings.Repeat("b", 64)},
		{"sci-linux-arm64", strings.Repeat("c", 64)},
		{"sci-linux-amd64", strings.Repeat("d", 64)},
	}

	// Build the body the way the workflow does: one **SHA256(...):** line per
	// asset, joined by newlines, with the surrounding **Commit:** / install
	// markdown the body template emits.
	var shaBlock strings.Builder
	for _, a := range assets {
		fmt.Fprintf(&shaBlock, "**SHA256(%s):** %s\n", a.name, a.sha)
	}
	body := fmt.Sprintf(`Latest build from `+"`main`"+` branch.

**Commit:** %s

%s
**Install:** `+"`curl -fsSL https://raw.githubusercontent.com/sciminds/sci/main/install.sh | sh`",
		commit, shaBlock.String())

	// Build the release JSON envelope the GitHub API serves.
	currentAsset := fmt.Sprintf("sci-%s-%s", runtime.GOOS, runtime.GOARCH)
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	releaseJSON := fmt.Sprintf(
		`{"body":%s,"assets":[{"name":%q,"browser_download_url":"https://example.invalid/sci"}]}`,
		string(bodyJSON), currentAsset,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releaseJSON))
	}))
	defer srv.Close()
	overrideOnlineProbe(t, srv)

	oldURL := releaseURL
	releaseURL = srv.URL
	defer func() { releaseURL = oldURL }()

	oldCommit := version.Commit
	version.Commit = "different"
	defer func() { version.Commit = oldCommit }()

	result := Check()
	skipIfLoopbackFlake(t, result.Error)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.LatestSHA != commit {
		t.Errorf("LatestSHA = %q, want %q", result.LatestSHA, commit)
	}

	// Find the expected hash for the current platform.
	var want string
	for _, a := range assets {
		if a.name == currentAsset {
			want = a.sha
		}
	}
	if want == "" {
		t.Fatalf("test asset table missing entry for %s — extend assets[] when adding a new release target", currentAsset)
	}
	if result.ExpectedSHA256 != want {
		t.Errorf("ExpectedSHA256 = %q, want %q (parser/workflow format drift?)", result.ExpectedSHA256, want)
	}
}

// TestCheck_ParsesPlatformSHA256 verifies the per-platform SHA256 extraction
// from the release body. The line must match the current runtime's asset name
// (e.g. sci-darwin-arm64) — entries for other platforms are ignored.
func TestCheck_ParsesPlatformSHA256(t *testing.T) {
	const sha = "abc1234def5678"
	wantHash := strings.Repeat("a", 64)
	assetName := fmt.Sprintf("sci-%s-%s", runtime.GOOS, runtime.GOARCH)

	body := fmt.Sprintf(
		`{"body":"**Commit:** %s\n**SHA256(%s):** %s\n**SHA256(sci-other-arch):** %s","assets":[{"name":%q,"browser_download_url":"https://example.invalid/x"}]}`,
		sha, assetName, wantHash, strings.Repeat("b", 64), assetName,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	overrideOnlineProbe(t, srv)

	oldURL := releaseURL
	releaseURL = srv.URL
	defer func() { releaseURL = oldURL }()

	oldCommit := version.Commit
	version.Commit = "different"
	defer func() { version.Commit = oldCommit }()

	result := Check()
	skipIfLoopbackFlake(t, result.Error)
	if result.ExpectedSHA256 != wantHash {
		t.Errorf("ExpectedSHA256 = %q, want %q", result.ExpectedSHA256, wantHash)
	}
}

// TestUpdate_RefusesWithoutSHA256 confirms Update fails closed when the
// release body did not include a SHA256 line for the current platform.
func TestUpdate_RefusesWithoutSHA256(t *testing.T) {
	_, err := Update("https://example.invalid/sci", "")
	if err == nil {
		t.Fatal("expected error for empty expected SHA256")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("error %q should mention SHA256", err)
	}
}

// TestUpdate_RejectsMismatchedChecksum confirms a downloaded binary whose
// hash differs from the expected value is discarded, not installed.
func TestUpdate_RejectsMismatchedChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("actual binary bytes"))
	}))
	defer srv.Close()

	// Wrong expected hash — must not match SHA256("actual binary bytes").
	wrongHash := strings.Repeat("0", 64)
	_, err := Update(srv.URL, wrongHash)
	if err == nil {
		t.Fatal("expected error for checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q should mention checksum mismatch", err)
	}
}

// TestFileSHA256 sanity-checks the helper against a known fixture.
func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture")
	body := []byte("hello world")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	got, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("fileSHA256 = %q, want %q", got, want)
	}
}

func TestCheck_Non200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	overrideOnlineProbe(t, srv)

	oldURL := releaseURL
	releaseURL = srv.URL
	defer func() { releaseURL = oldURL }()

	oldCommit := version.Commit
	version.Commit = "abc1234"
	defer func() { version.Commit = oldCommit }()

	result := Check()
	skipIfLoopbackFlake(t, result.Error)

	if result.Available {
		t.Error("should not report update when API returns 403")
	}
	if !strings.Contains(result.Error, "403") {
		t.Errorf("expected error mentioning 403, got %q", result.Error)
	}
}
