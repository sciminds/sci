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
//   - [FetchObjects] returns the raw bucket listing; [ListAt] folds it into
//     immediate children of a path (used by `sci cloud ls`); [ChildrenAt]
//     drives the `sci cloud browse` TUI.
//   - [Auth] / [AuthLogout] wrap `hf auth login` / `hf auth logout`.
//   - [Get] downloads a file to the current directory.
package share

import (
	"context"
	"errors"
	"fmt"
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
	metadataTimeout   = 30 * time.Second // list, exists, delete
	transferTimeout   = 10 * time.Minute // single-file upload/download
	folderSyncTimeout = 30 * time.Minute // hf buckets sync of a whole prefix
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

// FetchObjects returns the entire bucket listing as raw ObjectInfo. When
// plain is false a spinner is shown during the fetch. Used by both
// [ListAt] (plain CLI) and the browse TUI (interactive).
func FetchObjects(c *cloud.Client, plain bool) ([]cloud.ObjectInfo, error) {
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
		return objects, nil
	}
	if err := uikit.RunWithSpinner("Fetching files", listFn); err != nil {
		return nil, netutil.Wrap("listing files", err)
	}
	return objects, nil
}

// ListAt returns the immediate folder/file children at the bucket path
// (empty for root). Folders are synthesized client-side from "/" in keys.
func ListAt(c *cloud.Client, path string, plain bool) (*SharedListResult, error) {
	objects, err := FetchObjects(c, plain)
	if err != nil {
		return nil, err
	}
	entries := ChildrenAt(objects, strings.Trim(path, "/"))
	return &SharedListResult{Datasets: treeToShared(entries)}, nil
}

// treeToShared converts navigation TreeEntry rows into the JSON-shaped
// SharedEntry rendered by SharedListResult.
func treeToShared(entries []TreeEntry) []SharedEntry {
	return lo.Map(entries, func(e TreeEntry, _ int) SharedEntry {
		s := SharedEntry{
			Name:    e.Name,
			Owner:   e.Owner(),
			IsDir:   e.IsDir,
			Updated: e.LastModified,
			URL:     e.URL,
			Size:    e.Size,
		}
		if !e.IsDir {
			s.Type = detectFileType(e.Name)
		}
		return s
	})
}

// Get downloads a single file or syncs a whole folder from the chosen
// bucket. The name argument is resolved relative to the current user's
// folder by default; see [resolveDownloadKey] for the cross-user rules.
//
// File semantics: localPath empty or pointing at an existing directory
// puts the file inside it under its basename; otherwise localPath is the
// output filename. Folder semantics: localPath empty or pointing at an
// existing directory creates a same-named subdirectory in there;
// otherwise localPath is taken as the destination directory.
func Get(name, localPath string, public bool) (*CloudResult, error) {
	_, c, err := cloud.Setup(BucketFor(public))
	if err != nil {
		return nil, err
	}

	key := resolveDownloadKey(name, c.Username, public)

	metaCtx, metaCancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer metaCancel()
	objects, err := c.ListPrefix(metaCtx, key)
	if err != nil {
		return nil, netutil.Wrap("looking up "+key, err)
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("not found: %s", key)
	}

	if lo.ContainsBy(objects, func(o cloud.ObjectInfo) bool { return o.Key == key }) {
		return getFile(c, key, localPath)
	}
	return getFolder(c, key, localPath)
}

// resolveDownloadKey converts the user-supplied name into a full bucket key.
//
// Rules:
//   - An explicit "<username>/..." prefix is always honored as-is.
//   - On the public bucket, any other "<seg>/..." path is treated as an
//     absolute key (the cross-user form, e.g. "alice/results.csv").
//   - Otherwise the name is relative to the current user's folder and
//     the username prefix is prepended. This fixes nested-private paths
//     like "python-tutorials/data/credit.csv" that used to be misrouted
//     to the public bucket by the legacy "any slash → cross-user" rule.
func resolveDownloadKey(name, username string, public bool) string {
	name = strings.TrimPrefix(name, "/")
	if strings.HasPrefix(name, username+"/") {
		return name
	}
	if public && strings.Contains(name, "/") {
		return name
	}
	return username + "/" + name
}

// getFile streams a single file by full key into the path produced by
// resolveGetOutPath. Zips auto-extract next to the download.
func getFile(c *cloud.Client, key, localPath string) (*CloudResult, error) {
	outPath := resolveGetOutPath(key, localPath)
	if err := uikit.RunWithSpinner("Downloading "+filepath.Base(key), func() error {
		dlCtx, dlCancel := context.WithTimeout(context.Background(), transferTimeout)
		defer dlCancel()
		f, createErr := os.Create(outPath)
		if createErr != nil {
			return createErr
		}
		defer func() { _ = f.Close() }()
		if dlErr := c.DownloadByKey(dlCtx, key, f); dlErr != nil {
			return netutil.Wrap("download", dlErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if filepath.Ext(outPath) == ".zip" {
		extractDir := filepath.Join(filepath.Dir(outPath), nameFromFile(key))
		if err := uikit.RunWithSpinner("Extracting "+filepath.Base(key), func() error {
			return unzip(outPath, extractDir)
		}); err != nil {
			return nil, fmt.Errorf("extracting: %w", err)
		}
		_ = os.Remove(outPath)
		return &CloudResult{OK: true, Action: "get", Message: fmt.Sprintf("downloaded and extracted %s/", extractDir)}, nil
	}
	return &CloudResult{OK: true, Action: "get", Message: fmt.Sprintf("downloaded %s", outPath)}, nil
}

// getFolder syncs all objects under the bucket prefix into a local
// directory using `hf buckets sync`. The destination is computed the
// same way `cp -R` would: dropped inside localPath if it's an existing
// directory, otherwise localPath is taken as the new dir.
func getFolder(c *cloud.Client, key, localPath string) (*CloudResult, error) {
	outDir := resolveGetOutDir(key, localPath)
	if err := uikit.RunWithSpinner("Syncing "+filepath.Base(key)+"/", func() error {
		dlCtx, dlCancel := context.WithTimeout(context.Background(), folderSyncTimeout)
		defer dlCancel()
		if err := c.Sync(dlCtx, key, outDir); err != nil {
			return netutil.Wrap("sync", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &CloudResult{OK: true, Action: "get", Message: fmt.Sprintf("synced %s/ to %s/", key, outDir)}, nil
}

// resolveGetOutPath mirrors `cp`/`rsync` semantics: an empty or
// existing-directory localPath drops the file in there under its
// basename; anything else is taken as the destination filename.
func resolveGetOutPath(name, localPath string) string {
	base := filepath.Base(name)
	if localPath == "" {
		return base
	}
	if info, err := os.Stat(localPath); err == nil && info.IsDir() {
		return filepath.Join(localPath, base)
	}
	return localPath
}

// resolveGetOutDir is the folder analogue of resolveGetOutPath.
func resolveGetOutDir(key, localPath string) string {
	base := filepath.Base(key)
	if localPath == "" {
		return base
	}
	if info, err := os.Stat(localPath); err == nil && info.IsDir() {
		return filepath.Join(localPath, base)
	}
	return localPath
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
