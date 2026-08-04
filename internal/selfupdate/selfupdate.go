// Package selfupdate checks for and applies binary updates from GitHub releases.
//
// Releases are immutable CalVer tags (vYYYY.MM.DD, with a .N suffix for
// same-day follow-ups); [Check] queries GitHub's "latest release" endpoint
// and compares the release tag against the compiled-in [version.Version] by
// CalVer ordering — an update is available only when the release is strictly
// newer, so a binary ahead of the published release is never advised to
// downgrade. When either side lacks a parseable CalVer tag (pre-CalVer
// binaries in the wild, or the transitional "latest" bridge release), the
// comparison falls back to commit-SHA inequality. If an update is available,
// [Update] downloads the new binary and atomically replaces the running
// executable.
//
// Dev builds (commit = "unknown") are blocked from updating to avoid
// overwriting debug binaries.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/version"
)

// releaseURL is a var so tests can redirect to a local httptest server.
// The /releases/latest endpoint resolves to whichever release is marked
// "latest" (the newest CalVer release; make_latest in the workflow).
var releaseURL = "https://api.github.com/repos/sciminds/sci/releases/latest"

const (
	// commitPattern extracts a commit SHA from the release body.
	// Matches "**Commit:** <sha>" from the release notes.
	commitPattern = `\*\*Commit:\*\*\s+([0-9a-f]{7,40})`

	// sha256PatternFmt matches "**SHA256(sci-<os>-<arch>):** <64-hex>" in the
	// release body. The release workflow emits one line per platform asset;
	// Check() picks the one that matches runtime.GOOS/GOARCH.
	sha256PatternFmt = `\*\*SHA256\(%s\):\*\*\s+([0-9a-f]{64})`
)

// CheckResult holds the outcome of checking for updates.
//
// LastCheckedAt and LastShownAt are bookkeeping fields stamped by the
// cache layer (see internal/selfupdate/background.go). They are not set
// by Check() itself — Check() is a pure network read.
type CheckResult struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	CurrentSHA     string `json:"currentCommit"`
	LatestSHA      string `json:"latestCommit,omitempty"`
	DownloadURL    string `json:"downloadUrl,omitempty"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
	Error          string `json:"error,omitempty"`
	// omitempty has no effect on time.Time (a struct is never "empty"), so
	// these always serialize — a zero value means "never checked / shown".
	LastCheckedAt time.Time `json:"lastCheckedAt"`
	LastShownAt   time.Time `json:"lastShownAt"`
}

// releaseResponse is the subset of the GitHub release API we need.
type releaseResponse struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Check queries the GitHub releases API and compares the remote commit SHA
// against the compiled-in commit. Returns quickly — intended to run as a
// background goroutine or tea.Cmd.
func Check() CheckResult {
	result := CheckResult{CurrentSHA: version.Commit, CurrentVersion: version.Version}

	if version.Commit == "unknown" {
		result.Error = "dev build — no commit SHA to compare"
		return result
	}

	if !netutil.Online() {
		result.Error = "offline"
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
	result.LatestVersion = release.TagName

	// Extract commit SHA from release body.
	re := regexp.MustCompile(commitPattern)
	matches := re.FindStringSubmatch(release.Body)
	if len(matches) < 2 {
		result.Error = "could not find commit SHA in release notes"
		return result
	}
	result.LatestSHA = matches[1]

	// Find download URL for our platform.
	assetName := fmt.Sprintf("sci-%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, a := range release.Assets {
		if a.Name == assetName || a.Name == "sci" {
			result.DownloadURL = a.BrowserDownloadURL
			break
		}
	}

	// Extract the matching per-platform SHA256 from the release body. Missing
	// is non-fatal here — Update() is the one that refuses to proceed without
	// it, so callers can still surface "available" with an explanation.
	sha256Re := regexp.MustCompile(fmt.Sprintf(sha256PatternFmt, regexp.QuoteMeta(assetName)))
	if m := sha256Re.FindStringSubmatch(release.Body); len(m) == 2 {
		result.ExpectedSHA256 = m[1]
	}

	result.Available = updateAvailable(result.CurrentVersion, version.Commit, result.LatestVersion, result.LatestSHA)

	return result
}

// calverTagRe matches the release workflow's tag format: vYYYY.MM.DD with an
// optional .N suffix for same-day follow-up releases. Month and day are
// zero-padded (the workflow uses `date -u +%Y.%m.%d`), so an unpadded tag is
// not one of ours and fails the match.
var calverTagRe = regexp.MustCompile(`^v(\d{4})\.(\d{2})\.(\d{2})(?:\.(\d+))?$`)

// updateAvailable decides whether the published release is newer than the
// running binary. When both sides carry a CalVer tag the release must be
// strictly newer — a binary ahead of the published release is never advised
// to downgrade. When either side lacks a parseable tag (pre-CalVer binaries,
// the transitional "latest" bridge release), it falls back to commit-SHA
// inequality, the pre-CalVer semantics.
func updateAvailable(currentVersion, currentSHA, latestVersion, latestSHA string) bool {
	if newer, ok := calverNewer(latestVersion, currentVersion); ok {
		return newer
	}
	return commitsDiffer(currentSHA, latestSHA)
}

// calverNewer reports whether latest is a strictly newer CalVer tag than
// current. ok is false unless both tags parse; callers fall back to a
// commit-SHA comparison in that case.
func calverNewer(latest, current string) (newer, ok bool) {
	l, lok := calverParts(latest)
	c, cok := calverParts(current)
	if !lok || !cok {
		return false, false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i], true
		}
	}
	return false, true
}

// calverParts extracts the numeric components [year, month, day, n] of a
// CalVer tag; a missing same-day suffix counts as 0. Components compare
// numerically — a lexical compare would rank the tenth same-day release
// (.10) below the ninth (.9).
func calverParts(tag string) ([4]int, bool) {
	m := calverTagRe.FindStringSubmatch(tag)
	if m == nil {
		return [4]int{}, false
	}
	var parts [4]int
	for i, s := range m[1:] {
		if s == "" {
			continue // no same-day suffix
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return [4]int{}, false
		}
		parts[i] = n
	}
	return parts, true
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

// Update downloads the latest binary, verifies its SHA256 against expectedSHA256,
// and atomically replaces the running executable. Returns the path of the
// replaced binary.
//
// expectedSHA256 must be the 64-char hex digest emitted in the release body
// alongside the binary (see CheckResult.ExpectedSHA256). An empty value is
// refused — Update will not write an unverified binary over the running
// executable.
func Update(downloadURL, expectedSHA256 string) (string, error) {
	if expectedSHA256 == "" {
		return "", fmt.Errorf("refusing to update: release notes did not include a SHA256 for this platform")
	}

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

	gotSHA, err := fileSHA256(tmpPath)
	if err != nil {
		return "", fmt.Errorf("hash downloaded binary: %w", err)
	}
	if !strings.EqualFold(gotSHA, expectedSHA256) {
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s — release asset may be corrupted or tampered with", expectedSHA256, gotSHA)
	}

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

// fileSHA256 returns the hex-encoded SHA-256 digest of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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
