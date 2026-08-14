package cli

// `zot import <path…>` is the desktop-connector drag-drop equivalent: it
// POSTs local PDFs to Zotero desktop's 127.0.0.1:23119/connector/ endpoint,
// and desktop handles ingestion, sync-to-cloud, AND metadata recognition
// the same way it would for a drag-and-drop. Requires Zotero desktop to be
// running.
//
// Two modes, picked by what was passed:
//   - One file (or one explicit file arg) → single-file path. Waits for
//     recognition by default so the user sees the recognized title in the
//     result line. --no-wait opts out.
//   - Multiple files OR a directory (recursive walk) → batch mode. NoWait
//     is forced true; waiting per file would multiply latency by N. Desktop
//     keeps recognizing in the background and the user sees titles populate
//     in Zotero shortly after the batch finishes.
//
// This is intentionally a separate operation from the zot binary's
// `zot item add --file` (Web API, headless, no recognition). Users pick based
// on whether desktop is available: import when you want real metadata,
// `zot item add --file` when you don't care or don't have desktop up. sci
// keeps only the desktop-connector half — the Web-API upload is a credentialed
// write and lives in the other binary.

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/connector"
	"github.com/urfave/cli/v3"
)

func importCommand() *cli.Command {
	return &cli.Command{
		Name:      "import",
		Usage:     "Import local PDFs via Zotero desktop (drag-drop equivalent, with metadata recognition)",
		ArgsUsage: "<path>...",
		Description: "$ sci zot import ~/papers/Smith2022.pdf\n" +
			"$ sci zot import ~/papers/Smith2022.pdf --timeout 90s\n" +
			"$ sci zot import ~/papers/Smith2022.pdf --no-wait\n" +
			"$ sci zot import ~/papers/                       # recursive folder import\n" +
			"$ sci zot import a.pdf b.pdf c.pdf               # explicit file list\n" +
			"\n" +
			"Posts each PDF to Zotero desktop's local connector server\n" +
			"(127.0.0.1:23119). For single files the command waits for desktop's\n" +
			"metadata recognition (CrossRef/arXiv lookup → parent bib item) and\n" +
			"prints the recognized title.\n" +
			"\n" +
			"Folder / multi-file mode: pass one directory (walked recursively,\n" +
			"hidden files and symlinks skipped, non-PDFs counted in the summary)\n" +
			"or a list of PDF file paths. Recognition is NOT awaited per file\n" +
			"(desktop keeps recognizing in the background); a progress bar shows\n" +
			"upload status with running counters for recognized / imported / failed.\n" +
			"\n" +
			"Requires Zotero desktop to be running. Uses desktop's currently\n" +
			"selected library — not the --library flag — which is why this command\n" +
			"is exempt from --library. A headless Web-API upload (no recognition,\n" +
			"standalone attachment) is `zot item add --file` in the zot binary.",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:  "timeout",
				Value: connector.DefaultTimeout,
				Usage: "how long to wait for recognition before returning a partial result (single-file mode only)",
				Local: true,
			},
			&cli.BoolFlag{
				Name:  "no-wait",
				Usage: "return immediately after upload; skip polling for recognition (single-file mode only — batch always skips)",
				Local: true,
			},
		},
		Action: runImport,
	}
}

func runImport(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return cmdutil.UsageErrorf(cmd, "expected at least one <path> argument")
	}

	paths, skippedNonPDF, err := connector.CollectPaths(args)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	c := connector.NewClient()
	timeout := cmd.Duration("timeout")

	// Single-file fast path: keep current UX (no progress bar, blocking wait
	// for recognition). The user explicitly named one PDF, so we owe them
	// the title right there in the result line.
	if len(paths) == 1 && len(args) == 1 && skippedNonPDF == 0 {
		return runImportSingle(ctx, cmd, c, paths[0], timeout, cmd.Bool("no-wait"))
	}

	return runImportBatch(ctx, cmd, c, paths, skippedNonPDF, timeout)
}

// runImportSingle imports one PDF and, unless noWait is set, blocks up to
// timeout for Zotero desktop's metadata recognition so the result line can
// carry the recognized title.
func runImportSingle(ctx context.Context, cmd *cli.Command, c *connector.Client, path string, timeout time.Duration, noWait bool) error {
	res, err := connector.Import(ctx, c, path, connector.Options{
		Timeout: timeout,
		NoWait:  noWait,
	})
	if err != nil {
		return fmt.Errorf("zot import: %w", err)
	}
	// connector.Result and zot.ImportResult have identical fields/order, so
	// Go's struct conversion handles the wire-shape→render-shape hop without
	// a manual field-by-field copy. Tags differ (the render type carries
	// JSON tags) — that's fine since Go 1.8 conversions ignore tags.
	cmdutil.Output(cmd, zot.ImportResult(*res))
	return nil
}

// runImportBatch imports many PDFs behind a progress bar. Recognition is
// never awaited per file (that would multiply latency by N), so timeout
// bounds each upload rather than a recognition poll.
func runImportBatch(ctx context.Context, cmd *cli.Command, c *connector.Client, paths []string, skippedNonPDF int, timeout time.Duration) error {
	var batch *connector.BatchResult
	err := uikit.RunWithProgress("Importing PDFs", func(t *uikit.ProgressTracker) error {
		t.SetTotal(len(paths))
		res, berr := connector.ImportBatch(ctx, c, paths, connector.BatchOptions{
			Timeout: timeout,
			OnStart: func(_, _ int, p string) {
				t.Status(filepath.Base(p))
			},
			OnDone: func(_, _ int, r connector.ItemResult) {
				counter := "imported"
				switch {
				case r.Err != "":
					counter = "failed"
				case r.Recognized:
					counter = "recognized"
				}
				t.Advance(counter, filepath.Base(r.Path))
			},
		})
		batch = res
		return berr
	})
	if err != nil && batch == nil {
		// Hard failure (e.g. Ping refused) before any per-file work.
		return fmt.Errorf("zot import: %w", err)
	}

	out := zot.ImportBatchResult{
		Items:      make([]zot.ImportBatchItem, len(batch.Items)),
		Total:      batch.Total,
		Recognized: batch.Recognized,
		Imported:   batch.Imported,
		Failed:     batch.Failed,
		Skipped:    skippedNonPDF,
		Duration:   batch.Duration,
	}
	for i, it := range batch.Items {
		out.Items[i] = zot.ImportBatchItem{
			Path:       it.Path,
			Recognized: it.Recognized,
			Title:      it.Title,
			ItemType:   it.ItemType,
			Message:    it.Message,
			Error:      it.Err,
		}
	}
	cmdutil.Output(cmd, out)

	// Surface a non-zero exit when the whole batch failed (e.g. ctx cancel
	// after a few files). Per-file failures with some successes do NOT
	// fail the command — the user has the per-file detail in the result.
	if err != nil && batch.Recognized == 0 && batch.Imported == 0 {
		return fmt.Errorf("zot import: %w", err)
	}
	return nil
}
