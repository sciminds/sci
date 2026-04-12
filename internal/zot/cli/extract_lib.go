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
	extractLibJobs       int
	extractLibDevice     string
	extractLibNumThreads int
	extractLibYes        bool
	extractLibForce      bool
	extractLibReextract  bool
	extractLibLimit      int
	extractLibApply      bool
)

const defaultConsecutiveFailureLimit = 10

func extractLibCommand() *cli.Command {
	return &cli.Command{
		Name:  "extract-lib",
		Usage: "Bulk-extract every PDF in the library into Zotero child notes (via docling)",
		Description: "Runs `docling` on every parent item that has a PDF attachment.\n" +
			"\n" +
			"By default, extracted markdown is cached locally but NOT posted to Zotero.\n" +
			"Pass --apply to also create/update child notes in Zotero.\n" +
			"\n" +
			"Re-running after a failure resumes where it left off:\n" +
			"  1. Items whose note already landed in Zotero are skipped (sentinel check, --apply only).\n" +
			"  2. Items whose docling output was cached locally skip re-extraction.\n" +
			"\n" +
			"$ zot extract-lib                  # extract all PDFs to local cache\n" +
			"$ zot extract-lib --apply          # extract + post notes to Zotero\n" +
			"$ zot extract-lib --apply --yes    # skip confirmation\n" +
			"$ zot extract-lib --jobs 4         # 4 parallel workers (CPU mode)\n" +
			"$ zot extract-lib --reextract      # re-run docling, ignore cached output\n" +
			"$ zot extract-lib --force --apply  # re-post notes even if sentinel matches\n" +
			"$ zot extract-lib --limit 5        # extract at most 5 items (smoke test)",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Usage: "parallel docling workers (0 = auto: 1 for GPU, NumCPU/4 for CPU)", Destination: &extractLibJobs, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "auto", Destination: &extractLibDevice, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads per worker (0 = docling default)", Destination: &extractLibNumThreads, Local: true},
			&cli.BoolFlag{Name: "apply", Usage: "post extracted notes to Zotero (default is cache-only)", Destination: &extractLibApply, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &extractLibYes, Local: true},
			&cli.BoolFlag{Name: "force", Usage: "re-post notes even if sentinel says up-to-date", Destination: &extractLibForce, Local: true},
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run extraction from scratch", Destination: &extractLibReextract, Local: true},
			&cli.IntFlag{Name: "limit", Usage: "extract at most N items (for smoke testing)", Destination: &extractLibLimit, Local: true},
		},
		Action: extractLibAction,
	}
}

// noopChildLister returns no children for any parent, so PlanBatch
// always produces ActionCreate. Used in the default cache-only mode
// where we skip the Zotero API entirely.
type noopChildLister struct{}

func (noopChildLister) ListNoteChildren(context.Context, string) ([]extract.ChildNote, error) {
	return nil, nil
}

// noopNoteWriter accepts every write and discards it. Used in the
// default cache-only mode so ExecuteBatch populates the cache without
// touching Zotero.
type noopNoteWriter struct{}

func (noopNoteWriter) CreateChildNote(context.Context, string, string, []string) (string, error) {
	return "CACHE_ONLY", nil
}

func (noopNoteWriter) UpdateChildNote(context.Context, string, string) error {
	return nil
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

	if extractLibLimit > 0 && extractLibLimit < len(all) {
		all = all[:extractLibLimit]
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

	jobs := extractLibJobs
	if jobs == 0 {
		jobs = extract.BatchJobsDefault(extractLibDevice, runtime.NumCPU())
	}

	opts := extract.ZoteroDefaults()
	if extractLibDevice != "" && extractLibDevice != "auto" {
		opts.Device = extractLibDevice
	}
	opts.NumThreads = extractLibNumThreads

	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		return err
	}
	cache := &extract.MarkdownCache{Dir: cacheDir}

	// Default is cache-only (noops); --apply wires the real Zotero API.
	var lister extract.ChildLister
	var writer extract.NoteWriter
	if extractLibApply {
		apiClient, err := requireAPIClient()
		if err != nil {
			return err
		}
		lister = &apiChildListerAdapter{c: apiClient}
		writer = apiClient
	} else {
		lister = noopChildLister{}
		writer = noopNoteWriter{}
	}

	// Plan phase — concurrent, shows a spinner.
	var items []extract.BatchItem
	err = ui.RunWithSpinner("Planning extraction...", func() error {
		items = extract.PlanBatch(ctx, lister, reqs, jobs, extractLibForce)
		return nil
	})
	if err != nil {
		return err
	}

	// Tally the plan for confirmation.
	var nCreate, nReplace, nSkip, nErr int
	for _, it := range items {
		if it.Err != nil {
			nErr++
			continue
		}
		switch it.Plan.Action {
		case extract.ActionCreate:
			nCreate++
		case extract.ActionReplace:
			nReplace++
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
	needWork := nCreate + nReplace
	if needWork == 0 && nErr == 0 {
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
	msg := fmt.Sprintf("Extract %d items (%d create, %d replace, %d skip",
		len(items), nCreate, nReplace, nSkip)
	if nErr > 0 {
		msg += fmt.Sprintf(", %d plan errors", nErr)
	}
	msg += fmt.Sprintf("), %d workers%s?", jobs, mode)
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
		t.SetTotal(len(items))

		var res *extract.BatchResult
		var batchErr error
		res, batchErr = extract.ExecuteBatch(ctx, extract.BatchInput{
			Items:                   items,
			Extractor:               ex,
			Writer:                  writer,
			Cache:                   cache,
			ExtractOpts:             opts,
			Jobs:                    jobs,
			ConsecutiveFailureLimit: defaultConsecutiveFailureLimit,
			OnItemStart: func(i int, item extract.BatchItem) {
				t.Status(item.Request.PDFName)
			},
			OnItemDone: func(i int, outcome extract.BatchOutcome) {
				counter := "skipped"
				event := fmt.Sprintf("%s %s", ui.SymArrow, outcome.Item.Request.PDFName)
				if outcome.Err != nil {
					counter = "failed"
					event = fmt.Sprintf("%s %s: %s", ui.SymFail, outcome.Item.Request.PDFName, outcome.Err)
				} else {
					switch outcome.Action {
					case extract.ActionCreate:
						if outcome.FromCache {
							counter = "cached"
						} else {
							counter = "created"
						}
						event = fmt.Sprintf("%s %s %s", ui.SymOK, outcome.Action, outcome.Item.Request.PDFName)
					case extract.ActionReplace:
						if outcome.FromCache {
							counter = "cached"
						} else {
							counter = "replaced"
						}
						event = fmt.Sprintf("%s %s %s", ui.SymOK, outcome.Action, outcome.Item.Request.PDFName)
					case extract.ActionSkip:
						event = fmt.Sprintf("%s skipped %s", ui.SymArrow, outcome.Item.Request.PDFName)
					}
				}
				t.Advance(counter, event)
			},
		})
		batchResult = res
		return batchErr
	})
	if err != nil {
		return err
	}

	created, replaced, skipped, cached, failed := batchResult.Counts()
	result := zot.ExtractLibResult{
		Total:    len(items),
		Created:  created,
		Replaced: replaced,
		Skipped:  skipped,
		Cached:   cached,
		Failed:   failed,
		Aborted:  batchResult.Aborted,
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
