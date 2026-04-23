package connector

// Batch import: walks one directory or accepts a flat list of files, then
// runs Import serially over the result. Continues on per-file error so a
// single bad PDF doesn't abort 50 imports. Pings desktop ONCE up front
// rather than per file.
//
// The batch flow forces NoWait=true regardless of opts: waiting up to a
// minute per file for recognition would compound a 50-PDF batch into 50
// minutes of foreground waiting. Desktop's recognize pipeline keeps running
// in the background after we hand off — users see the recognized titles in
// Zotero a few seconds later.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ItemResult is the per-file outcome inside a batch. Mirrors Result with an
// Err string instead of an error value so the type round-trips cleanly
// through JSON output for agents/scripts.
type ItemResult struct {
	Path       string `json:"path"`
	Recognized bool   `json:"recognized"`
	Title      string `json:"title,omitempty"`
	ItemType   string `json:"item_type,omitempty"`
	Message    string `json:"message,omitempty"`
	Err        string `json:"error,omitempty"`
}

// BatchOptions controls the batch run.
//
//   - Timeout is per-file. Inert when NoWait is true (which is forced for
//     batch — see the package doc above).
//   - OnStart fires before each file's upload; the CLI uses it to update
//     the progress bar's status line ("uploading Smith2022.pdf").
//   - OnDone fires after each file completes (success or failure); the CLI
//     uses it to advance the progress counter.
type BatchOptions struct {
	Timeout time.Duration
	OnStart func(idx, total int, path string)
	OnDone  func(idx, total int, r ItemResult)
}

// BatchResult is the full outcome of a batch import. Items is aligned 1:1
// with the input paths (in walk order for the directory case). Skipped is
// the count of non-PDF files seen during the directory walk (always 0 when
// the input was an explicit file list, since CollectPaths rejects non-PDFs
// in that path).
type BatchResult struct {
	Items      []ItemResult `json:"items"`
	Total      int          `json:"total"`
	Recognized int          `json:"recognized"`
	Imported   int          `json:"imported"` // upload OK, no recognition (timed out / no match / no-wait)
	Failed     int          `json:"failed"`
	Skipped    int          `json:"skipped_non_pdf"`
	Duration   string       `json:"duration"`
}

// CollectPaths resolves the user's args into the final list of PDFs to
// import. Two accepted shapes:
//
//   - Exactly one arg, and it's a directory → recursive walk; skips hidden
//     files/dirs (any path segment starting with "."), skips symlinks (no
//     follow), keeps only regular files with .pdf extension. Non-PDFs are
//     counted in skippedNonPDF for the summary line. An empty walk result
//     is an error ("no PDFs found in <dir>").
//   - One or more args, all regular files → returned as-is after .pdf
//     extension validation per file. Mixing file and directory args is
//     rejected ("cannot mix files and directories").
//
// Hidden / symlink filtering only applies inside the directory walk. An
// explicitly-passed hidden file or symlink is the user's intent and is
// imported.
func CollectPaths(args []string) (paths []string, skippedNonPDF int, err error) {
	if len(args) == 0 {
		return nil, 0, errors.New("no paths provided")
	}

	// Classify: which args are directories?
	var dirArgs, fileArgs []string
	for _, a := range args {
		fi, err := os.Stat(a)
		if err != nil {
			return nil, 0, fmt.Errorf("stat %q: %w", a, err)
		}
		if fi.IsDir() {
			dirArgs = append(dirArgs, a)
		} else {
			fileArgs = append(fileArgs, a)
		}
	}

	switch {
	case len(dirArgs) > 0 && len(fileArgs) > 0:
		return nil, 0, errors.New("cannot mix files and directories — pass a single directory or a list of files")
	case len(dirArgs) > 1:
		return nil, 0, errors.New("only one directory argument is supported per invocation")
	case len(dirArgs) == 1:
		return walkDirForPDFs(dirArgs[0])
	default:
		// All args are files. Validate each is a PDF.
		for _, a := range fileArgs {
			if !strings.EqualFold(filepath.Ext(a), ".pdf") {
				return nil, 0, fmt.Errorf("%q is not a PDF; the desktop connector only supports PDFs", a)
			}
		}
		return fileArgs, 0, nil
	}
}

// walkDirForPDFs recursively walks root and returns the .pdf files within.
// Hidden segments (any component starting with ".") and symlinks are
// skipped. Non-PDF files encountered are counted but not returned; the
// count is shown in the batch summary so the user can sanity-check that
// the walk skipped what they expected.
func walkDirForPDFs(root string) ([]string, int, error) {
	var pdfs []string
	skipped := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Hidden segment — skip the entry. For directories, return SkipDir
		// to avoid descending into .git / .zotero / etc.
		if strings.HasPrefix(d.Name(), ".") && path != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Don't follow symlinks. WalkDir already doesn't follow symlinked
		// directories; this catches symlinked files too.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Only regular files we recognize as PDFs.
		if d.Type().IsRegular() && strings.EqualFold(filepath.Ext(d.Name()), ".pdf") {
			pdfs = append(pdfs, path)
			return nil
		}
		// Anything else — count as a non-PDF skip for the summary.
		if d.Type().IsRegular() {
			skipped++
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("walk %q: %w", root, err)
	}
	if len(pdfs) == 0 {
		return nil, skipped, fmt.Errorf("no PDFs found in %q", root)
	}
	return pdfs, skipped, nil
}

// ImportBatch processes paths serially, continuing past per-file errors.
// One Ping happens up front; per-file failures are recorded as ItemResult.Err
// and the batch keeps going. NoWait is forced true regardless of opts —
// see the package doc for why.
func ImportBatch(ctx context.Context, t Transport, paths []string, opts BatchOptions) (*BatchResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	// Force no-wait for batch: waiting per file would multiply latency by N.
	// Desktop's recognize pipeline keeps running after upload so the user
	// sees results in Zotero shortly after the batch completes.
	perFile := Options{Timeout: opts.Timeout, NoWait: true}

	if err := t.Ping(ctx); err != nil {
		return nil, err
	}

	start := time.Now()
	out := &BatchResult{Total: len(paths), Items: make([]ItemResult, 0, len(paths))}

	for i, path := range paths {
		// Honour ctx cancellation between files. A cancellation mid-loop
		// stops processing but returns whatever we already have.
		if err := ctx.Err(); err != nil {
			out.Duration = time.Since(start).Round(time.Millisecond).String()
			return out, err
		}

		if opts.OnStart != nil {
			opts.OnStart(i, len(paths), path)
		}
		item := importBatchItem(ctx, t, path, perFile)
		out.Items = append(out.Items, item)
		switch {
		case item.Err != "":
			out.Failed++
		case item.Recognized:
			out.Recognized++
		default:
			out.Imported++
		}
		if opts.OnDone != nil {
			opts.OnDone(i, len(paths), item)
		}
	}

	out.Duration = time.Since(start).Round(time.Millisecond).String()
	return out, nil
}

// importBatchItem runs the per-file work and packages success or failure
// into an ItemResult so the caller's loop stays linear. Errors don't
// propagate — they get embedded in the result so the batch continues.
func importBatchItem(ctx context.Context, t Transport, path string, opts Options) ItemResult {
	f, err := openPDF(path)
	if err != nil {
		return ItemResult{Path: path, Err: err.Error()}
	}
	defer func() { _ = f.Close() }()

	res, err := importOne(ctx, t, f, path, opts)
	if err != nil {
		return ItemResult{Path: path, Err: err.Error()}
	}
	return ItemResult{
		Path:       res.Path,
		Recognized: res.Recognized,
		Title:      res.Title,
		ItemType:   res.ItemType,
		Message:    res.Message,
	}
}
