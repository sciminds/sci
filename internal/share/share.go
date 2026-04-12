// Package share implements the public dataset sharing commands (sci share,
// sci unshare, sci shared) and the authentication flow (sci auth).
//
// It bridges the local filesystem with Cloudflare R2 object storage
// for uploading, downloading, and listing shared files.
//
// Key functions:
//
//   - [SmartShare] uploads a local file or downloads a dataset by name
//   - [Unshare] removes a shared file
//   - [Ls] lists shared files
//   - [Auth] / [AuthLogout] manage R2 credentials
//   - [GetTo] downloads a shared file to a specific directory
package share

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/ui"
)

// Network timeouts for cloud operations.
const (
	metadataTimeout = 30 * time.Second // list, head, exists, delete
	transferTimeout = 10 * time.Minute // upload, download
)

// Auth checks if already configured; if so, shows status. Otherwise initiates
// the GitHub OAuth device flow to authenticate and receive R2 credentials.
func Auth() (*AuthResult, error) {
	cfg, err := cloud.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.Public != nil && cfg.Public.AccessKey != "" {
		return &AuthResult{
			OK:       true,
			Action:   "status",
			Username: cfg.Username,
			Message:  fmt.Sprintf("authenticated as @%s", cfg.Username),
		}, nil
	}

	// Initiate device flow.
	ctx := context.Background()
	dc, err := cloud.RequestDeviceCode(ctx, cloud.DefaultWorkerURL)
	if err != nil {
		return nil, fmt.Errorf("starting auth: %w", err)
	}

	// Show the user code and verification URL.
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  Go to:  %s\n", dc.VerificationURI)
	fmt.Fprintf(os.Stderr, "  Code:   %s\n", ui.TUI.Bold().Render(dc.UserCode))
	fmt.Fprintln(os.Stderr)

	// Best-effort open browser.
	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", dc.VerificationURI).Start()
	}

	// Poll for approval.
	var resp *cloud.TokenResponse
	if err := ui.RunWithSpinner("Waiting for GitHub authorization", func() error {
		var pollErr error
		resp, pollErr = cloud.PollForToken(ctx, cloud.DefaultWorkerURL, dc.DeviceCode, time.Duration(dc.Interval)*time.Second)
		return pollErr
	}); err != nil {
		return nil, err
	}

	// Save credentials.
	newCfg := &cloud.Config{
		Username:    resp.Username,
		GitHubLogin: resp.GitHubLogin,
		AccountID:   resp.AccountID,
		Public:      resp.Public,
		Board:       resp.Board,
	}
	if err := cloud.SaveConfig(newCfg); err != nil {
		return nil, fmt.Errorf("saving credentials: %w", err)
	}

	return &AuthResult{
		OK:       true,
		Action:   "login",
		Username: resp.Username,
		Message:  fmt.Sprintf("authenticated as @%s", resp.Username),
	}, nil
}

// AuthLogout clears the saved credentials.
func AuthLogout() (*AuthResult, error) {
	if err := cloud.ClearConfig(); err != nil {
		return nil, err
	}
	return &AuthResult{OK: true, Action: "logout", Message: "credentials removed"}, nil
}

// MaxUploadSize is the maximum file size allowed for upload (10 GB).
const MaxUploadSize int64 = 10 * 1024 * 1024 * 1024

// ShareOpts controls Share behavior.
type ShareOpts struct { //nolint:revive // name is established in the API
	// Name is the object name in R2 (e.g. "my-results.csv").
	// If empty, defaults to the base filename.
	Name string
	// Description is an optional human-readable description of the file.
	Description string
	// Force overwrites an existing file without error.
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

// CheckExists returns true if a file with the given name already exists in R2.
func CheckExists(name string) (bool, error) {
	_, c, err := cloud.Setup()
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

// Share uploads a file or directory to R2 under the given name.
// Directories are automatically zipped.
func Share(filePath string, opts ShareOpts) (*CloudResult, error) {
	_, c, err := cloud.Setup()
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

	// If it's a directory, zip it to a temp file.
	if info.IsDir() {
		stem := nameFromFile(filePath)
		tmpZip, err := os.CreateTemp("", stem+"-*.zip")
		if err != nil {
			return nil, fmt.Errorf("creating temp file: %w", err)
		}
		tmpZipPath := tmpZip.Name()
		_ = tmpZip.Close() // zipDir will create/overwrite the file
		if err := ui.RunWithSpinner("Packing "+stem, func() error {
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

	// Check if it already exists.
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

	// Check file size before uploading.
	uploadInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if uploadInfo.Size() > MaxUploadSize {
		return nil, fmt.Errorf("file size %s exceeds the 10 GB upload limit",
			humanize.Bytes(uint64(uploadInfo.Size())))
	}

	// Build user metadata.
	var metadata map[string]string
	if opts.Description != "" {
		metadata = map[string]string{"description": opts.Description}
	}

	var result *CloudResult
	if err := ui.RunWithSpinner("Uploading "+name, func() error {
		f, openErr := os.Open(absPath)
		if openErr != nil {
			return openErr
		}
		defer func() { _ = f.Close() }()

		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), transferTimeout)
		defer uploadCancel()
		contentType := detectContentType(absPath)
		if uploadErr := c.Upload(uploadCtx, name, f, contentType, metadata); uploadErr != nil {
			return netutil.Wrap("upload", uploadErr)
		}

		url := c.PublicObjectURL(name)
		action := "shared"
		if opts.Force {
			action = "updated"
		}
		result = &CloudResult{
			OK:      true,
			Action:  "put",
			Message: fmt.Sprintf("%s %q", action, name),
			URL:     url,
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Unshare removes a shared file from R2.
func Unshare(name string) (*CloudResult, error) {
	_, c, err := cloud.Setup()
	if err != nil {
		return nil, err
	}

	filename := ensureExtension(name)

	if err := ui.RunWithSpinner("Removing "+filename, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
		defer cancel()
		exists, existsErr := c.Exists(ctx, filename)
		if existsErr != nil {
			return netutil.Wrap("checking file", existsErr)
		}
		if !exists {
			return fmt.Errorf("file %q not found", filename)
		}
		if delErr := c.Delete(ctx, filename); delErr != nil {
			return netutil.Wrap("removing file", delErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &CloudResult{OK: true, Action: "remove", Message: fmt.Sprintf("removed %q", filename)}, nil
}

// SharedAll lists all users' shared files in the bucket.
// When plain is true the spinner is skipped.
func SharedAll(c *cloud.Client, plain bool) (*SharedListResult, error) {
	return sharedWithOpts(c, plain, true)
}

func sharedWithOpts(c *cloud.Client, plain, allUsers bool) (*SharedListResult, error) {
	listFn := c.List
	spinnerMsg := "Fetching your files"
	if allUsers {
		listFn = func(ctx context.Context) ([]cloud.ObjectInfo, error) {
			return c.ListPrefix(ctx, "")
		}
		spinnerMsg = "Fetching files"
	}

	listCtx, listCancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer listCancel()

	var objects []cloud.ObjectInfo
	if plain {
		var err error
		objects, err = listFn(listCtx)
		if err != nil {
			return nil, netutil.Wrap("listing files", err)
		}
	} else if err := ui.RunWithSpinner(spinnerMsg, func() error {
		var listErr error
		objects, listErr = listFn(listCtx)
		return listErr
	}); err != nil {
		return nil, netutil.Wrap("listing files", err)
	}

	entries := buildSharedEntries(objects, c.Username, allUsers)

	// Fetch descriptions concurrently via HeadObject (max 10 in flight).
	// Only fetch for the current user's files (HeadObject is scoped to username).
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for i := range entries {
		if allUsers && entries[i].Owner != c.Username {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			headCtx, headCancel := context.WithTimeout(context.Background(), metadataTimeout)
			defer headCancel()
			meta, err := c.HeadObject(headCtx, entries[idx].Name)
			if err != nil {
				return
			}
			if desc, ok := meta["description"]; ok {
				entries[idx].Description = desc
			}
		}(i)
	}
	wg.Wait()

	return &SharedListResult{Datasets: entries}, nil
}

// buildSharedEntries converts raw ObjectInfo into SharedEntry slices.
// When allUsers is false, it strips the username prefix from keys.
// When allUsers is true, it parses the owner from the key and populates Owner.
func buildSharedEntries(objects []cloud.ObjectInfo, username string, allUsers bool) []SharedEntry {
	entries := make([]SharedEntry, len(objects))
	for i, obj := range objects {
		if allUsers {
			parts := strings.SplitN(obj.Key, "/", 2)
			owner := ""
			name := obj.Key
			if len(parts) == 2 {
				owner = parts[0]
				name = parts[1]
			}
			entries[i] = SharedEntry{
				Name:    name,
				Owner:   owner,
				Type:    detectFileType(name),
				Updated: obj.LastModified,
				URL:     obj.URL,
				Size:    obj.Size,
			}
		} else {
			name := strings.TrimPrefix(obj.Key, username+"/")
			entries[i] = SharedEntry{
				Name:    name,
				Type:    detectFileType(name),
				Updated: obj.LastModified,
				URL:     obj.URL,
				Size:    obj.Size,
			}
		}
	}
	return entries
}

// Get downloads a shared file to the current directory.
// If name contains a "/" it is treated as "owner/filename" for cross-user
// downloads; otherwise the current user's namespace is used.
func Get(name string) (*CloudResult, error) {
	_, c, err := cloud.Setup()
	if err != nil {
		return nil, err
	}

	filename := ensureExtension(name)

	outPath := filepath.Base(filename)
	dl := downloadFunc(c, filename)
	if err := ui.RunWithSpinner("Downloading "+filepath.Base(filename), func() error {
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
		extractDir := nameFromFile(filename)
		if err := ui.RunWithSpinner("Extracting "+filepath.Base(filename), func() error {
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

// ensureExtension returns the name as-is if it has an extension,
// otherwise it's returned unchanged (the caller may need to search).
func ensureExtension(name string) string {
	return name
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

// detectContentType maps file extensions to MIME types.
func detectContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return "text/csv"
	case ".tsv":
		return "text/tab-separated-values"
	case ".json":
		return "application/json"
	case ".db":
		return "application/x-sqlite3"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".zip":
		return "application/zip"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".tar":
		return "application/x-tar"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
