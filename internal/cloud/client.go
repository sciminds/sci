package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/samber/lo"
)

// Client wraps `hf buckets` subprocess calls scoped to a single bucket.
type Client struct {
	Username string
	Org      string
	Bucket   string // BucketPublic or BucketPrivate

	// run executes `hf <args>` and returns combined stdout. Overridable in tests.
	run func(ctx context.Context, args ...string) ([]byte, error)
	// stream executes `hf <args>` with stdin/stdout piped through. Overridable in tests.
	stream func(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error
}

// ErrNotAuthenticated is returned by [Verify] when `hf auth whoami` indicates
// the user has not yet logged in. Callers can use this to distinguish "needs
// login" from other failures (network, parsing, etc.) and trigger
// `hf auth login`.
var ErrNotAuthenticated = errors.New("not authenticated with Hugging Face")

// Verify confirms the user is authenticated with `hf` and is a member of the
// SciMinds org.
func Verify() (*Config, error) {
	user, orgs, err := hfWhoami()
	if err != nil {
		if isNotLoggedIn(err) {
			return nil, ErrNotAuthenticated
		}
		return nil, fmt.Errorf("checking Hugging Face auth: %w", err)
	}
	if !lo.Contains(orgs, DefaultOrg) {
		return nil, fmt.Errorf("@%s is not a member of the %s org on Hugging Face", user, DefaultOrg)
	}
	return &Config{Username: user, Org: DefaultOrg}, nil
}

// isNotLoggedIn checks whether an hf error indicates the user simply hasn't
// run `hf auth login` yet (vs. a transient or parsing failure).
func isNotLoggedIn(err error) bool {
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "not logged in") ||
		strings.Contains(low, "not authenticated") ||
		strings.Contains(low, "no token")
}

// Setup verifies HF auth and returns a [Client] scoped to the named bucket
// ([BucketPublic] or [BucketPrivate]).
func Setup(bucket string) (*Config, *Client, error) {
	cfg, err := Verify()
	if err != nil {
		return nil, nil, err
	}
	return cfg, NewClient(cfg.Username, cfg.Org, bucket), nil
}

// NewClient builds a client targeting the given bucket.
func NewClient(username, org, bucket string) *Client {
	return &Client{
		Username: username,
		Org:      org,
		Bucket:   bucket,
		run:      runHF,
		stream:   streamHF,
	}
}

// IsPublic reports whether this client targets a public-readable bucket.
func (c *Client) IsPublic() bool { return c.Bucket == BucketPublic }

// bucketHandle returns the `hf://buckets/<org>/<bucket>` prefix.
func (c *Client) bucketHandle() string {
	return "hf://buckets/" + c.Org + "/" + c.Bucket
}

// objectKey returns "<username>/<filename>" — the per-user-prefixed key.
func (c *Client) objectKey(filename string) string {
	return c.Username + "/" + filename
}

// objectHandle returns the full hf:// URL for a filename owned by the current user.
func (c *Client) objectHandle(filename string) string {
	return c.bucketHandle() + "/" + c.objectKey(filename)
}

// keyHandle returns the full hf:// URL for an arbitrary key (may include another user's prefix).
func (c *Client) keyHandle(key string) string {
	return c.bucketHandle() + "/" + key
}

// PublicObjectURL returns the HTTPS resolve URL for a file owned by the
// current user. Empty for private buckets (no public download endpoint).
func (c *Client) PublicObjectURL(filename string) string {
	if !c.IsPublic() {
		return ""
	}
	return c.publicURLForKey(c.objectKey(filename))
}

// publicURLForKey returns the HTTPS resolve URL for an arbitrary key.
// Empty for private buckets.
func (c *Client) publicURLForKey(key string) string {
	if !c.IsPublic() {
		return ""
	}
	return "https://huggingface.co/buckets/" + c.Org + "/" + c.Bucket + "/resolve/" + key
}

// Upload streams body to <bucket>/<username>/<filename>.
func (c *Client) Upload(ctx context.Context, filename string, body io.Reader) error {
	return c.stream(ctx, body, io.Discard, "buckets", "cp", "-", c.objectHandle(filename))
}

// Download streams <bucket>/<username>/<filename> to dst.
func (c *Client) Download(ctx context.Context, filename string, dst io.Writer) error {
	return c.stream(ctx, nil, dst, "buckets", "cp", c.objectHandle(filename), "-")
}

// DownloadByKey streams an object addressed by full key (including the owner's
// prefix) to dst. Used for cross-user downloads from the public bucket.
func (c *Client) DownloadByKey(ctx context.Context, key string, dst io.Writer) error {
	return c.stream(ctx, nil, dst, "buckets", "cp", c.keyHandle(key), "-")
}

// List returns objects under the current user's prefix.
func (c *Client) List(ctx context.Context) ([]ObjectInfo, error) {
	return c.ListPrefix(ctx, c.Username+"/")
}

// ListPrefix returns objects under an arbitrary prefix (empty = whole bucket).
func (c *Client) ListPrefix(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	arg := c.Org + "/" + c.Bucket
	if prefix != "" {
		arg = arg + "/" + strings.TrimSuffix(prefix, "/")
	}
	out, err := c.run(ctx, "buckets", "ls", arg, "-R", "--json")
	if err != nil {
		// hf returns an error when the prefix has no matches; treat as empty.
		if isHFNotFound(out, err) {
			return nil, nil
		}
		return nil, err
	}
	objects, err := parseListing(out)
	if err != nil {
		return nil, err
	}
	if c.IsPublic() {
		for i := range objects {
			objects[i].URL = c.publicURLForKey(objects[i].Key)
		}
	}
	return objects, nil
}

// Exists reports whether <bucket>/<username>/<filename> exists.
func (c *Client) Exists(ctx context.Context, filename string) (bool, error) {
	key := c.objectKey(filename)
	objects, err := c.ListPrefix(ctx, key)
	if err != nil {
		return false, err
	}
	return lo.ContainsBy(objects, func(o ObjectInfo) bool { return o.Key == key }), nil
}

// Delete removes <bucket>/<username>/<filename>.
func (c *Client) Delete(ctx context.Context, filename string) error {
	_, err := c.run(ctx, "buckets", "rm", "-y", c.Org+"/"+c.Bucket+"/"+c.objectKey(filename))
	return err
}

// Sync copies every object under the bucket prefix into localDir using
// `hf buckets sync`. Used for whole-folder downloads; the per-file `cp`
// path doesn't accept directories.
func (c *Client) Sync(ctx context.Context, prefix, localDir string) error {
	src := c.bucketHandle() + "/" + strings.TrimSuffix(prefix, "/")
	_, err := c.run(ctx, "buckets", "sync", src, localDir)
	return err
}

// ---------------------------------------------------------------------------
// hf subprocess + parsing helpers
// ---------------------------------------------------------------------------

// hfFile is the shape of one entry in `hf buckets ls --json` output.
type hfFile struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	MTime      string `json:"mtime"`
	UploadedAt string `json:"uploaded_at"`
}

// parseListing decodes `hf buckets ls --json` output into [ObjectInfo] slices.
// Folder entries are skipped. `hf` returns paths relative to the bucket root
// (verified empirically), so each `path` already includes the per-user prefix.
func parseListing(out []byte) ([]ObjectInfo, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 || bytes.Equal(out, []byte("[]")) {
		return nil, nil
	}
	var files []hfFile
	if err := json.Unmarshal(out, &files); err != nil {
		return nil, fmt.Errorf("parse hf buckets ls output: %w", err)
	}
	return lo.FilterMap(files, func(f hfFile, _ int) (ObjectInfo, bool) {
		if f.Type != "file" {
			return ObjectInfo{}, false
		}
		return ObjectInfo{
			Key:          f.Path,
			Size:         f.Size,
			LastModified: f.MTime,
		}, true
	}), nil
}

// isHFNotFound reports whether an `hf` failure indicates the prefix was empty
// or absent rather than a real error.
func isHFNotFound(out []byte, err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(low, "not found") || strings.Contains(low, "no such") || strings.Contains(low, "404")
}

// runHF is the default Client.run — executes `hf <args>` and returns combined output.
func runHF(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "hf", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Return stderr so callers can pattern-match (e.g. isHFNotFound).
		return stderr.Bytes(), fmt.Errorf("hf %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// streamHF executes `hf <args>` piping stdin/stdout through.
func streamHF(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "hf", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if stdout != nil {
		cmd.Stdout = stdout
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hf %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// hfWhoami runs `hf auth whoami --format=json` and parses the user + org list.
// Forcing the JSON format makes parsing deterministic regardless of whether
// stdout is a TTY (`auto` would otherwise switch between `agent` and `human`).
var hfWhoami = func() (user string, orgs []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "hf", "auth", "whoami", "--format=json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return "", nil, fmt.Errorf("hf auth whoami: %s", msg)
	}
	var resp struct {
		User string `json:"user"`
		Orgs string `json:"orgs"`
	}
	if jerr := json.Unmarshal(stdout.Bytes(), &resp); jerr != nil {
		return "", nil, fmt.Errorf("parse hf auth whoami output (%q): %w", stdout.String(), jerr)
	}
	if resp.User == "" {
		return "", nil, fmt.Errorf("hf auth whoami returned empty user (output: %q)", stdout.String())
	}
	if resp.Orgs != "" {
		orgs = strings.Split(resp.Orgs, ",")
	}
	return resp.User, orgs, nil
}
