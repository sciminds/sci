// Package selfupdate checks for and applies binary updates from GitHub releases.
//
// The sci binary uses a rolling "latest" release tag. [Check] compares the
// current build's commit SHA against the latest release. If they differ,
// [Update] downloads the new binary and atomically replaces the running
// executable.
//
// Dev builds (commit = "unknown") are blocked from updating to avoid
// overwriting debug binaries.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/version"
)

// releaseURL is a var so tests can redirect to a local httptest server.
var releaseURL = "https://api.github.com/repos/sciminds/sci/releases/tags/latest"

const (
	// commitPattern extracts a commit SHA from the release body.
	// Matches "**Commit:** <sha>" from the release notes.
	commitPattern = `\*\*Commit:\*\*\s+([0-9a-f]{7,40})`
)

// CheckResult holds the outcome of checking for updates.
type CheckResult struct {
	Available     bool   `json:"available"`
	CurrentSHA    string `json:"currentCommit"`
	LatestSHA     string `json:"latestCommit,omitempty"`
	DownloadURL   string `json:"downloadUrl,omitempty"`
	LatestVersion string `json:"latestVersion,omitempty"`
	Error         string `json:"error,omitempty"`
}

// releaseResponse is the subset of the GitHub release API we need.
type releaseResponse struct {
	Body   string         `json:"body"`
	Assets []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Check queries the GitHub releases API and compares the remote commit SHA
// against the compiled-in commit. Returns quickly — intended to run as a
// background goroutine or tea.Cmd.
func Check() CheckResult {
	result := CheckResult{CurrentSHA: version.Commit}

	if version.Commit == "unknown" {
		result.Error = "dev build — no commit SHA to compare"
		return result
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", releaseURL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := ghToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = netutil.Wrap("checking for updates", err).Error()
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("GitHub API returned %d", resp.StatusCode)
		return result
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		result.Error = err.Error()
		return result
	}

	// Extract commit SHA from release body.
	re := regexp.MustCompile(commitPattern)
	matches := re.FindStringSubmatch(release.Body)
	if len(matches) < 2 {
		result.Error = "could not find commit SHA in release notes"
		return result
	}
	result.LatestSHA = matches[1]

	// Extract version from release body (optional).
	versionRe := regexp.MustCompile(`\*\*Version:\*\*\s+(\S+)`)
	if vm := versionRe.FindStringSubmatch(release.Body); len(vm) >= 2 {
		result.LatestVersion = vm[1]
	}

	// Find download URL for our platform.
	assetName := fmt.Sprintf("sci-%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, a := range release.Assets {
		if a.Name == assetName || a.Name == "sci" {
			result.DownloadURL = a.BrowserDownloadURL
			break
		}
	}

	// Compare: if the current commit is a prefix of the latest (or vice versa), we're up-to-date.
	result.Available = commitsDiffer(version.Commit, result.LatestSHA)

	return result
}

// commitsDiffer reports whether current and latest refer to different commits.
// It handles short/long SHA comparisons by checking prefix in both directions.
func commitsDiffer(current, latest string) bool {
	return !strings.HasPrefix(latest, current) && !strings.HasPrefix(current, latest)
}

// ShortSHA returns the first 7 characters of a SHA, or the full string if shorter.
func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Download fetches the binary from downloadURL and writes it to dest.
// progressFn is called with bytes written so far (can be nil).
func Download(downloadURL string, dest *os.File, progressFn func(int64)) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return netutil.Wrap("download", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if progressFn != nil {
		reader = &progressReader{r: resp.Body, fn: progressFn}
	}

	if _, err := io.Copy(dest, reader); err != nil {
		return fmt.Errorf("write binary: %w", err)
	}
	return nil
}

// Update downloads the latest binary and replaces the running executable.
// Returns the path of the replaced binary.
func Update(downloadURL string) (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}

	// Download to a temp file in the same directory (for atomic rename).
	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, "sci-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // clean up on error

	if err := Download(downloadURL, tmp, nil); err != nil {
		_ = tmp.Close()
		return "", err
	}
	_ = tmp.Close()

	// Make executable.
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod: %w", err)
	}

	// Atomic replace.
	if err := os.Rename(tmpPath, execPath); err != nil {
		return "", fmt.Errorf("replace binary: %w", err)
	}

	return execPath, nil
}

// ghToken returns a GitHub token, checking environment variables first then
// falling back to `gh auth token` for keyring-based auth.
func ghToken() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type progressReader struct {
	r       io.Reader
	fn      func(int64)
	written int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.written += int64(n)
	pr.fn(pr.written)
	return n, err
}
