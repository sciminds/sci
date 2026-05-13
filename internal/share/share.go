// Package share implements the cloud upload/download/list/remove commands
// against the SciMinds Hugging Face org buckets.
//
// Two buckets exist under the sciminds org:
//
//   - "public"  — world-readable; uploads produce a stable HTTPS resolve URL
//   - "private" — org-only; the default for new uploads
//
// Authentication is delegated to the `hf` CLI: users run `hf auth login`
// themselves, and [Auth] is a thin wrapper that triggers it for first-timers.
//
// Key functions:
//
//   - [Share] uploads a local file or directory (zipped); [ShareOpts.Public]
//     selects the bucket.
//   - [Unshare] removes a shared file.
//   - [SharedAll] lists every user's files in a bucket.
//   - [Auth] / [AuthLogout] wrap `hf auth login` / `hf auth logout`.
//   - [Get] downloads a file to the current directory.
package share

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/uikit"
)

// Network timeouts for cloud operations.
const (
	metadataTimeout = 30 * time.Second // list, exists, delete
	transferTimeout = 10 * time.Minute // upload, download
)

// BucketFor returns the bucket name matching the public flag.
func BucketFor(public bool) string {
	if public {
		return cloud.BucketPublic
	}
	return cloud.BucketPrivate
}

// Auth verifies HF auth + org membership; only when `hf` reports the user is
// not logged in does it launch `hf auth login` interactively. Other errors
// (network, parsing, missing `hf` binary) are surfaced directly.
func Auth() (*AuthResult, error) {
	cfg, err := cloud.Verify()
	if err == nil {
		return &AuthResult{
			OK: true, Action: "status", Username: cfg.Username,
			Message: fmt.Sprintf("authenticated as @%s (%s)", cfg.Username, cfg.Org),
		}, nil
	}
	if !errors.Is(err, cloud.ErrNotAuthenticated) {
		return nil, err
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, uikit.TUI.Dim().Render("Launching `hf auth login`…"))
	cmd := exec.Command("hf", "auth", "login")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hf auth login failed: %w", err)
	}

	cfg, err = cloud.Verify()
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		OK: true, Action: "login", Username: cfg.Username,
		Message: fmt.Sprintf("authenticated as @%s (%s)", cfg.Username, cfg.Org),
	}, nil
}

// AuthLogout runs `hf auth logout`.
func AuthLogout() (*AuthResult, error) {
	cmd := exec.Command("hf", "auth", "logout")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hf auth logout failed: %w", err)
	}
	return &AuthResult{OK: true, Action: "logout", Message: "logged out of Hugging Face"}, nil
}

// MaxUploadSize is the maximum file size allowed for upload (10 GB).
const MaxUploadSize int64 = 10 * 1024 * 1024 * 1024

// ShareOpts controls Share behaviour.
type ShareOpts struct { //nolint:revive // name is established in the API
	// Name is the object name (e.g. "my-results.csv"). Defaults to base filename.
	Name string
	// Public selects the public bucket; default is the private bucket.
	Public bool
	// Force overwrites an existing file without erroring.
	Force bool
}

// DefaultShareName returns the default share name for a file path,
// preserving the extension. For directories, appends ".zip".
func DefaultShareName(filePath string) string {
	info, err := os.Stat(filePath)
	if err != nil {
		return filepath.Base(filePath)
	}
	if info.IsDir() {
		return nameFromFile(filePath) + ".zip"
	}
	return filepath.Base(filePath)
}

// CheckExists returns true if a file with the given name already exists in
// the bucket selected by public.
func CheckExists(name string, public bool) (bool, error) {
	_, c, err := cloud.Setup(BucketFor(public))
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()
	ok, err := c.Exists(ctx, name)
	if err != nil {
		return false, netutil.Wrap("checking file", err)
	}
	return ok, nil
}

// Share uploads a file or directory under the chosen name to the bucket
// selected by opts.Public. Directories are automatically zipped.
func Share(filePath string, opts ShareOpts) (*CloudResult, error) {
	_, c, err := cloud.Setup(BucketFor(opts.Public))
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}

	name := opts.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	// Zip directories to a temp file.
	if info.IsDir() {
		stem := nameFromFile(filePath)
		tmpZip, err := os.CreateTemp("", stem+"-*.zip")
		if err != nil {
			return nil, fmt.Errorf("creating temp file: %w", err)
		}
		tmpZipPath := tmpZip.Name()
		_ = tmpZip.Close()
		if err := uikit.RunWithSpinner("Packing "+stem, func() error {
			return zipDir(absPath, tmpZipPath)
		}); err != nil {
			_ = os.Remove(tmpZipPath)
			return nil, fmt.Errorf("zipping directory: %w", err)
		}
		defer func() { _ = os.Remove(tmpZipPath) }()
		absPath = tmpZipPath
		if !strings.HasSuffix(name, ".zip") {
			name += ".zip"
		}
	}

	// Refuse to clobber unless --force.
	if !opts.Force {
		existsCtx, existsCancel := context.WithTimeout(context.Background(), metadataTimeout)
		defer existsCancel()
		exists, err := c.Exists(existsCtx, name)
		if err != nil {
			return nil, netutil.Wrap("checking file", err)
		}
		if exists {
			return nil, fmt.Errorf("file %q already exists (use --force to replace)", name)
		}
	}

	// Size guard before opening the stream.
	uploadInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if uploadInfo.Size() > MaxUploadSize {
		return nil, fmt.Errorf("file size %s exceeds the 10 GB upload limit",
			humanize.Bytes(uint64(uploadInfo.Size())))
	}

	var result *CloudResult
	if err := uikit.RunWithSpinner("Uploading "+name, func() error {
		f, openErr := os.Open(absPath)
		if openErr != nil {
			return openErr
		}
		defer func() { _ = f.Close() }()

		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), transferTimeout)
		defer uploadCancel()
		if uploadErr := c.Upload(uploadCtx, name, f); uploadErr != nil {
			return netutil.Wrap("upload", uploadErr)
		}

		action := "shared"
		if opts.Force {
			action = "updated"
		}
		result = &CloudResult{
			OK:      true,
			Action:  "put",
			Message: fmt.Sprintf("%s %q to %s bucket", action, name, c.Bucket),
			URL:     c.PublicObjectURL(name), // empty for private bucket
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Unshare removes a shared file from the bucket selected by public.
func Unshare(name string, public bool) (*CloudResult, error) {
	_, c, err := cloud.Setup(BucketFor(public))
	if err != nil {
		return nil, err
	}

	if err := uikit.RunWithSpinner("Removing "+name, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
		defer cancel()
		exists, existsErr := c.Exists(ctx, name)
		if existsErr != nil {
			return netutil.Wrap("checking file", existsErr)
		}
		if !exists {
			return fmt.Errorf("file %q not found", name)
		}
		if delErr := c.Delete(ctx, name); delErr != nil {
			return netutil.Wrap("removing file", delErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &CloudResult{
		OK: true, Action: "remove",
		Message: fmt.Sprintf("removed %q from %s bucket", name, c.Bucket),
	}, nil
}

// SharedAll lists every user's files in the bucket the client is scoped to.
// When plain is true the spinner is skipped.
func SharedAll(c *cloud.Client, plain bool) (*SharedListResult, error) {
	listCtx, listCancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer listCancel()

	var objects []cloud.ObjectInfo
	listFn := func() error {
		var listErr error
		objects, listErr = c.ListPrefix(listCtx, "")
		return listErr
	}
	if plain {
		if err := listFn(); err != nil {
			return nil, netutil.Wrap("listing files", err)
		}
	} else if err := uikit.RunWithSpinner("Fetching files", listFn); err != nil {
		return nil, netutil.Wrap("listing files", err)
	}

	return &SharedListResult{Datasets: buildSharedEntries(objects, c.Username, true)}, nil
}

// buildSharedEntries converts raw ObjectInfo into SharedEntry slices.
// When allUsers is false, the username prefix is stripped from keys.
// When allUsers is true, the owner is parsed from the key.
func buildSharedEntries(objects []cloud.ObjectInfo, username string, allUsers bool) []SharedEntry {
	return lo.Map(objects, func(obj cloud.ObjectInfo, _ int) SharedEntry {
		if allUsers {
			parts := strings.SplitN(obj.Key, "/", 2)
			owner, name := "", obj.Key
			if len(parts) == 2 {
				owner, name = parts[0], parts[1]
			}
			return SharedEntry{
				Name: name, Owner: owner, Type: detectFileType(name),
				Updated: obj.LastModified, URL: obj.URL, Size: obj.Size,
			}
		}
		name := strings.TrimPrefix(obj.Key, username+"/")
		return SharedEntry{
			Name: name, Type: detectFileType(name),
			Updated: obj.LastModified, URL: obj.URL, Size: obj.Size,
		}
	})
}

// Get downloads a shared file to the current directory.
// If name contains a "/", it's treated as "owner/filename" (cross-user
// download). Cross-user downloads only work from the public bucket — the
// private bucket has no owner field on the read path.
func Get(name string, public bool) (*CloudResult, error) {
	_, c, err := cloud.Setup(BucketFor(public))
	if err != nil {
		return nil, err
	}

	outPath := filepath.Base(name)
	dl := downloadFunc(c, name)
	if err := uikit.RunWithSpinner("Downloading "+filepath.Base(name), func() error {
		dlCtx, dlCancel := context.WithTimeout(context.Background(), transferTimeout)
		defer dlCancel()
		f, createErr := os.Create(outPath)
		if createErr != nil {
			return createErr
		}
		defer func() { _ = f.Close() }()
		if dlErr := dl(dlCtx, f); dlErr != nil {
			return netutil.Wrap("download", dlErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Auto-extract zip files.
	if filepath.Ext(outPath) == ".zip" {
		extractDir := nameFromFile(name)
		if err := uikit.RunWithSpinner("Extracting "+filepath.Base(name), func() error {
			return unzip(outPath, extractDir)
		}); err != nil {
			return nil, fmt.Errorf("extracting: %w", err)
		}
		_ = os.Remove(outPath)
		return &CloudResult{OK: true, Action: "get", Message: fmt.Sprintf("downloaded and extracted %s/", extractDir)}, nil
	}
	return &CloudResult{OK: true, Action: "get", Message: fmt.Sprintf("downloaded %s", outPath)}, nil
}

// downloadFunc returns a download closure. If filename contains a "/"
// it is treated as a full key (owner/file) and downloaded via DownloadByKey;
// otherwise it uses the current user's namespace via Download.
func downloadFunc(c *cloud.Client, filename string) func(context.Context, io.Writer) error {
	if strings.Contains(filename, "/") {
		return func(ctx context.Context, dst io.Writer) error {
			return c.DownloadByKey(ctx, filename, dst)
		}
	}
	return func(ctx context.Context, dst io.Writer) error {
		return c.Download(ctx, filename, dst)
	}
}

// nameFromFile derives a dataset name from a file path (stem without extension).
func nameFromFile(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// detectFileType maps file extensions to type labels.
func detectFileType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv", ".tsv":
		return "csv"
	case ".db", ".duckdb", ".ddb":
		return "db"
	case ".png", ".jpg", ".jpeg", ".gif", ".svg":
		return "media"
	case ".zip", ".tar", ".gz", ".tgz":
		return "zip"
	default:
		return "other"
	}
}
