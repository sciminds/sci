package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/urfave/cli/v3"
)

var (
	extractLibDevice     string
	extractLibNumThreads int
	extractLibBatchSize  int
	extractLibYes        bool
	extractLibForce      bool
	extractLibReextract  bool
	extractLibLimit      int
	extractLibApply      bool
	extractLibHTML       bool
)

func extractLibCommand() *cli.Command {
	return &cli.Command{
		Name:  "extract-lib",
		Usage: "Bulk-extract every PDF in the library into Zotero child notes (via docling)",
		Description: "Runs `docling` on every parent item that has a PDF attachment.\n" +
			"\n" +
			"By default, extracted markdown is cached locally but NOT posted to Zotero.\n" +
			"Pass --apply to also create child notes in Zotero.\n" +
			"\n" +
			"Re-running after a failure resumes where it left off:\n" +
			"  1. Items whose docling-tagged note already exists in Zotero are skipped (--apply only).\n" +
			"  2. Items whose docling output was cached locally skip re-extraction.\n" +
			"\n" +
			"$ zot extract-lib                  # extract all PDFs to local cache\n" +
			"$ zot extract-lib --apply          # extract + post notes to Zotero\n" +
			"$ zot extract-lib --apply --yes    # skip confirmation\n" +
			"$ zot extract-lib --reextract      # re-run docling, ignore cached output\n" +
			"$ zot extract-lib --force --apply  # create new notes even where docling note exists\n" +
			"$ zot extract-lib --limit 5        # extract at most 5 items (smoke test)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "mps", Destination: &extractLibDevice, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads (0 = docling default)", Destination: &extractLibNumThreads, Local: true},
			&cli.IntFlag{Name: "batch-size", Usage: "PDFs per docling process (0 = all in one process)", Destination: &extractLibBatchSize, Local: true},
			&cli.BoolFlag{Name: "apply", Usage: "post extracted notes to Zotero (default is cache-only)", Destination: &extractLibApply, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &extractLibYes, Local: true},
			&cli.BoolFlag{Name: "force", Usage: "create new notes even if docling note already exists", Destination: &extractLibForce, Local: true},
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run extraction from scratch", Destination: &extractLibReextract, Local: true},
			&cli.IntFlag{Name: "limit", Usage: "extract at most N items (for smoke testing)", Destination: &extractLibLimit, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "render markdown as HTML before posting (default is raw markdown)", Destination: &extractLibHTML, Local: true},
		},
		Action: extractLibAction,
	}
}

// noopNoteWriter accepts every write and discards it. Used in the
// default cache-only mode so ExecuteBatch populates the cache without
// touching Zotero.
type noopNoteWriter struct{}

func (noopNoteWriter) CreateChildNote(context.Context, string, string, []string) (string, error) {
	return "CACHE_ONLY", nil
}

func extractLibAction(ctx context.Context, cmd *cli.Command) error {
	cfg, db, err := openLocalDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	all, err := db.ListAllPDFAttachments()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		_, _ = fmt.Fprintln(cmd.Root().Writer, "  no items with PDF attachments found")
		return nil
	}

	// Query local DB for parents that already have docling notes.
	hasExisting, err := db.ParentsWithDoclingNotes()
	if err != nil {
		return err
	}

	// Filter out items that already have a docling note in Zotero.
	if !extractLibForce {
		filtered := all[:0]
		for _, p := range all {
			if !hasExisting[p.ParentKey] {
				filtered = append(filtered, p)
			}
		}
		all = filtered
	}

	reqs := make([]extract.BatchRequest, len(all))
	for i, p := range all {
		reqs[i] = extract.BatchRequest{
			ParentKey: p.ParentKey,
			PDFKey:    p.Attachment.Key,
			PDFName:   p.Attachment.Title,
			PDFPath:   filepath.Join(cfg.DataDir, "storage", p.Attachment.Key, p.Attachment.Filename),
		}
	}

	// PlanBatch uses concurrent hashing; use a reasonable parallelism.
	planJobs := extract.BatchJobsDefault(extractLibDevice, runtime.NumCPU())
	if planJobs < 4 {
		planJobs = 4
	}

	opts := extract.ZoteroDefaults()
	if extractLibDevice != "" {
		opts.Device = extractLibDevice
	}
	opts.NumThreads = extractLibNumThreads

	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		return err
	}
	cache := &extract.MarkdownCache{Dir: cacheDir}

	// Default is cache-only (noops); --apply wires the real Zotero API.
	var writer extract.NoteWriter
	if extractLibApply {
		apiClient, err := requireAPIClient()
		if err != nil {
			return err
		}
		writer = apiClient
	} else {
		writer = noopNoteWriter{}
	}

	// Plan phase — concurrent hashing + plan, shows a spinner.
	// We plan ALL candidates first so we can filter out cached items
	// before applying --limit. This ensures --limit picks up the next
	// N truly-unextracted items instead of re-selecting cached ones.
	var items []extract.BatchItem
	err = ui.RunWithSpinner("Planning extraction...", func() error {
		items = extract.PlanBatch(ctx, reqs, planJobs, extractLibForce, hasExisting)
		return nil
	})
	if err != nil {
		return err
	}

	// Filter out items that are already cached (prior run extracted
	// but didn't --apply). Without this, --limit keeps hitting cache
	// instead of advancing to new items.
	if !extractLibReextract {
		filtered := items[:0]
		for _, it := range items {
			if it.Err != nil || it.Plan.Action == extract.ActionSkip {
				filtered = append(filtered, it)
				continue
			}
			if _, ok := cache.Get(it.Request.PDFKey, it.Hash); ok {
				// Already cached — skip unless --apply needs to post.
				if extractLibApply {
					filtered = append(filtered, it)
				}
				// In cache-only mode, nothing to do — drop it.
				continue
			}
			filtered = append(filtered, it)
		}
		items = filtered
	}

	// Apply --limit after filtering so re-runs advance past cached items.
	if extractLibLimit > 0 && extractLibLimit < len(items) {
		items = items[:extractLibLimit]
	}

	// Tally the plan for confirmation.
	var nCreate, nSkip, nErr int
	for _, it := range items {
		if it.Err != nil {
			nErr++
			continue
		}
		switch it.Plan.Action {
		case extract.ActionCreate:
			nCreate++
		case extract.ActionSkip:
			nSkip++
		}
	}

	// --reextract: clear cache entries so docling re-runs from scratch.
	if extractLibReextract {
		for _, it := range items {
			if it.Err == nil && it.Hash != "" {
				cache.Delete(it.Request.PDFKey, it.Hash)
			}
		}
	}

	// Check if there's anything to do.
	if nCreate == 0 && nErr == 0 {
		cmdutil.Output(cmd, zot.ExtractLibResult{
			Total:   len(items),
			Skipped: nSkip,
		})
		return nil
	}

	// Confirm.
	mode := " (cache-only)"
	if extractLibApply {
		mode = " (apply: posting notes to Zotero)"
	}
	msg := fmt.Sprintf("Extract %d items (%d create, %d skip",
		len(items), nCreate, nSkip)
	if nErr > 0 {
		msg += fmt.Sprintf(", %d plan errors", nErr)
	}
	msg += fmt.Sprintf(")%s?", mode)
	if done, err := cmdutil.ConfirmOrSkip(extractLibYes, msg); done || err != nil {
		return err
	}

	// Execute phase — progress display.
	started := time.Now()
	var batchResult *extract.BatchResult

	ex, err := extract.NewDoclingExtractor()
	if err != nil {
		return err
	}

	progressTitle := "Extracting library (cache-only)"
	if extractLibApply {
		progressTitle = "Extracting library"
	}

	err = ui.RunWithProgress(progressTitle, func(t *ui.ProgressTracker) error {
		t.SetTotal(nCreate)

		var res *extract.BatchResult
		var batchErr error
		res, batchErr = extract.ExecuteBatch(ctx, extract.BatchInput{
			Items:       items,
			Extractor:   ex,
			Writer:      writer,
			Cache:       cache,
			ExtractOpts: opts,
			BatchSize:   extractLibBatchSize,
			RenderHTML:  extractLibHTML,
			OnProgress: func(ev *extract.DoclingEvent) {
				switch ev.Kind {
				case extract.EventProcessing:
					t.Status(ev.Document)
				case extract.EventFinished:
					t.Advance("extracted", fmt.Sprintf("%s %s (%.1fs)", ui.SymOK, ev.Document, ev.Duration.Seconds()))
				case extract.EventFailed:
					t.Advance("failed", fmt.Sprintf("%s %s", ui.SymFail, ev.Document))
				}
			},
			OnItemDone: func(i int, outcome extract.BatchOutcome) {
				if outcome.Action == extract.ActionSkip {
					return
				}
				if outcome.Err != nil && outcome.FromCache {
					// Cache hit but note post failed — still counts
					// as an advance for the progress bar.
					t.Advance("failed", fmt.Sprintf("%s %s: %s", ui.SymFail, outcome.Item.Request.PDFName, outcome.Err))
				}
			},
		})
		batchResult = res
		return batchErr
	})
	if err != nil {
		return err
	}

	created, skipped, cached, failed := batchResult.Counts()
	result := zot.ExtractLibResult{
		Total:    len(items),
		Created:  created,
		Skipped:  skipped,
		Cached:   cached,
		Failed:   failed,
		Duration: time.Since(started),
	}
	if failed > 0 {
		result.Errors = make(map[string]string)
		for _, o := range batchResult.Outcomes {
			if o.Err != nil {
				result.Errors[o.Item.Request.ParentKey] = o.Err.Error()
			}
		}
	}
	cmdutil.Output(cmd, result)
	return nil
}
