package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

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
	// list reads a bucket listing from the Hub API. Overridable in tests.
	list func(ctx context.Context, org, bucket, prefix string) ([]hfFile, error)
}

// ErrNotAuthenticated is returned by [Verify] when `hf auth whoami` indicates
// the user has not yet logged in. Callers can use this to distinguish "needs
// login" from other failures (network, parsing, etc.) and trigger
// `hf auth login`.
var ErrNotAuthenticated = errors.New("not authenticated with Hugging Face")

// Verify confirms the user is authenticated with `hf` and is a member of the
// SciMinds org. It resolves the local credential once, then passes that same
// value to the Hub request so token-store failures remain distinguishable.
func Verify() (*Config, error) {
	tok, err := hfToken()
	if errors.Is(err, errNoToken) {
		return nil, ErrNotAuthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("reading Hugging Face credential: %w", err)
	}
	user, orgs, err := hfWhoamiWithToken(tok)
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
	return errors.Is(err, errNoToken) || errors.Is(err, errTokenRejected)
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
		list:     listBucketTree,
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
// Folder entries are dropped; the Hub returns paths relative to the bucket
// root, so each key already carries its owner's prefix.
func (c *Client) ListPrefix(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	files, err := c.list(ctx, c.Org, c.Bucket, prefix)
	if err != nil {
		return nil, err
	}
	objects := lo.FilterMap(files, func(f hfFile, _ int) (ObjectInfo, bool) {
		if f.Type != "file" {
			return ObjectInfo{}, false
		}
		return ObjectInfo{Key: f.Path, Size: f.Size, LastModified: f.MTime}, true
	})
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

// PrefixExists reports whether any objects live under the user's
// <name> prefix — used as the dir-upload analogue of [Exists]. Treats
// the hf "prefix not found" case as empty (no error).
func (c *Client) PrefixExists(ctx context.Context, name string) (bool, error) {
	objects, err := c.ListPrefix(ctx, c.objectKey(name))
	if err != nil {
		return false, err
	}
	return len(objects) > 0, nil
}

// Delete removes <bucket>/<username>/<filename>.
func (c *Client) Delete(ctx context.Context, filename string) error {
	_, err := c.run(ctx, "buckets", "rm", "-y", c.Org+"/"+c.Bucket+"/"+c.objectKey(filename))
	return err
}

// DeletePrefix recursively removes every object under <user>/<name>
// via `hf buckets rm -y -R`. Used to delete whole folders from the
// browse TUI; sole entry point for tree deletion.
func (c *Client) DeletePrefix(ctx context.Context, name string) error {
	target := c.Org + "/" + c.Bucket + "/" + c.objectKey(name)
	_, err := c.run(ctx, "buckets", "rm", "-y", "-R", target)
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

// SyncUp uploads localDir as a tree under the current user's prefix at
// name, via `hf buckets sync <localDir> hf://.../<user>/<name>`. Existing
// remote files at the same path are overwritten when size/mtime differ;
// remote files not present locally are left in place (no --delete).
// Used for whole-folder uploads; the per-file `cp` path doesn't accept
// directories.
func (c *Client) SyncUp(ctx context.Context, localDir, name string) error {
	src := strings.TrimSuffix(localDir, "/")
	dst := c.bucketHandle() + "/" + strings.TrimSuffix(c.objectKey(name), "/")
	_, err := c.run(ctx, "buckets", "sync", src, dst)
	return err
}

// ---------------------------------------------------------------------------
// hf subprocess + parsing helpers
// ---------------------------------------------------------------------------

// hfFile is one entry in a bucket listing, as returned by the Hub's
// /api/buckets/{org}/{bucket}/tree endpoint.
type hfFile struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	MTime      string `json:"mtime"`
	UploadedAt string `json:"uploadedAt"`
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
		return stderr.Bytes(), hfRunError(ctx.Err(), args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// hfRunError shapes a failed `hf` invocation into something a user can act on.
// When the context deadline killed the process, os/exec reports only
// "signal: killed" — naming the timeout instead keeps a slow `hf` from reading
// like a crash.
func hfRunError(ctxErr error, args []string, runErr error, stderr string) error {
	cmdline := "hf " + strings.Join(args, " ")
	if ctxErr != nil {
		return fmt.Errorf("%s: %w — the `hf` CLI did not finish in time", cmdline, ctxErr)
	}
	return fmt.Errorf("%s: %w: %s", cmdline, runErr, strings.TrimSpace(stderr))
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
		return hfRunError(ctx.Err(), args, err, stderr.String())
	}
	return nil
}
