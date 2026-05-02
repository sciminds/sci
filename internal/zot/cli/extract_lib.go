package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

var (
	extractLibDevice     string
	extractLibNumThreads int
	extractLibJobs       int
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
		Usage: experimental + " Bulk-extract every PDF in the library into Zotero child notes (via docling)",
		Description: "Runs `docling` on every parent item that has a PDF attachment.\n" +
			"\n" +
			"By default, extracted markdown is cached locally but NOT posted to Zotero.\n" +
			"Pass --apply to also create child notes in Zotero.\n" +
			"\n" +
			"Re-running after a failure resumes where it left off:\n" +
			"  1. Items whose docling-tagged note already exists in Zotero are skipped (--apply only).\n" +
			"  2. Items whose docling output was cached locally skip re-extraction.\n" +
			"\n" +
			"$ sci zot extract-lib                  # extract all PDFs to local cache\n" +
			"$ sci zot extract-lib --apply          # extract + post notes to Zotero\n" +
			"$ sci zot extract-lib --apply --yes    # skip confirmation\n" +
			"$ sci zot extract-lib --reextract      # re-run docling, ignore cached output\n" +
			"$ sci zot extract-lib --force --apply  # create new notes even where docling note exists\n" +
			"$ sci zot extract-lib --limit 5        # extract at most 5 items (smoke test)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "mps", Destination: &extractLibDevice, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads (0 = docling default)", Destination: &extractLibNumThreads, Local: true},
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Usage: "parallel docling processes (0 = single process for all PDFs)", Destination: &extractLibJobs, Local: true},
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

func (noopNoteWriter) AddTagToItem(context.Context, string, string) error {
	return nil
}

func extractLibAction(ctx context.Context, cmd *cli.Command) error {
	cfg, db, err := openLocalDB(ctx)
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
		all = lo.Reject(all, func(p local.PDFParent, _ int) bool {
			return hasExisting[p.ParentKey]
		})
	}

	reqs := lo.Map(all, func(p local.PDFParent, _ int) extract.BatchRequest {
		return extract.BatchRequest{
			ParentKey: p.ParentKey,
			PDFKey:    p.Attachment.Key,
			PDFName:   p.Attachment.Title,
			PDFPath:   filepath.Join(cfg.DataDir, "storage", p.Attachment.Key, p.Attachment.Filename),
		}
	})

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
	var backfillTagged, backfillFailed int
	if extractLibApply {
		apiClient, err := requireAPIClient(ctx)
		if err != nil {
			return err
		}
		writer = apiClient

		// Retroactively tag any parent that already has a docling note
		// in Zotero but is missing the has-markdown marker on the
		// parent itself. Runs BEFORE the extract phase so the local-DB
		// query isn't racing freshly-posted notes (which the inline
		// tag in postNote covers anyway). Idempotent: a parent that
		// already carries the tag is a no-op inside AddTagToItem.
		tagged, failed, err := backfillHasMarkdownTag(ctx, db, apiClient)
		if err != nil {
			return err
		}
		backfillTagged, backfillFailed = tagged, failed
	} else {
		writer = noopNoteWriter{}
	}

	// Plan phase — concurrent hashing + plan, shows a spinner.
	// We plan ALL candidates first so we can filter out cached items
	// before applying --limit. This ensures --limit picks up the next
	// N truly-unextracted items instead of re-selecting cached ones.
	var items []extract.BatchItem
	err = uikit.RunWithSpinner("Planning extraction...", func() error {
		items = extract.PlanBatch(ctx, reqs, planJobs, extractLibForce, hasExisting)
		return nil
	})
	if err != nil {
		return err
	}

	// Filter out items that are already cached (prior run extracted
	// but didn't --apply). Without this, --limit keeps hitting cache
	// instead of advancing to new items.
	//
	// In --apply mode cached items are kept (they still need posting)
	// and tracked in cachedIdx so the confirm prompt can distinguish
	// "needs new extraction" from "already cached, only needs posting".
	cachedIdx := make(map[int]bool)
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
					cachedIdx[len(filtered)] = true
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
		for i := range cachedIdx {
			if i >= extractLibLimit {
				delete(cachedIdx, i)
			}
		}
	}

	// Tally the plan for confirmation. nFresh = needs new extraction,
	// nCachedPost = already cached, only needs posting (--apply only).
	var nCreate, nSkip, nErr, nFresh, nCachedPost int
	for i, it := range items {
		if it.Err != nil {
			nErr++
			continue
		}
		switch it.Plan.Action {
		case extract.ActionCreate:
			nCreate++
			if cachedIdx[i] {
				nCachedPost++
			} else {
				nFresh++
			}
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
		outputScoped(ctx, cmd, zot.ExtractLibResult{
			Total:          len(items),
			Skipped:        nSkip,
			BackfilledTags: backfillTagged,
			BackfillFailed: backfillFailed,
		})
		return nil
	}

	// Confirm.
	mode := " (cache-only)"
	if extractLibApply {
		mode = " (apply: posting notes to Zotero)"
	}
	var msg string
	if extractLibApply && nCachedPost > 0 {
		msg = fmt.Sprintf("Process %d items (%d new extractions, %d post from cache, %d skip",
			len(items), nFresh, nCachedPost, nSkip)
	} else {
		msg = fmt.Sprintf("Extract %d items (%d create, %d skip",
			len(items), nCreate, nSkip)
	}
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

	err = uikit.RunWithProgress("Planning...", func(t *uikit.ProgressTracker) error {
		t.SetTotal(nCreate)

		var curPhase extract.BatchPhase

		var res *extract.BatchResult
		var batchErr error
		res, batchErr = extract.ExecuteBatch(ctx, extract.BatchInput{
			Items:       items,
			Extractor:   ex,
			Writer:      writer,
			Cache:       cache,
			ExtractOpts: opts,
			Jobs:        extractLibJobs,
			RenderHTML:  extractLibHTML,
			OnPhase: func(phase extract.BatchPhase, count int) {
				curPhase = phase
				switch phase {
				case extract.PhasePostCached:
					t.Reset("Posting cached notes to Zotero", count)
				case extract.PhaseExtract:
					suffix := " (cache-only)"
					if extractLibApply {
						suffix = ""
					}
					t.Reset(fmt.Sprintf("Extracting PDFs%s", suffix), count)
				case extract.PhasePostFresh:
					t.Reset("Posting notes to Zotero", count)
				}
			},
			OnProgress: func(ev *extract.DoclingEvent) {
				switch ev.Kind {
				case extract.EventProcessing:
					t.Status(ev.Document)
				case extract.EventFinished:
					t.Advance("extracted", fmt.Sprintf("%s %s (%.1fs)", uikit.SymOK, ev.Document, ev.Duration.Seconds()))
				case extract.EventFailed:
					t.Advance("failed", fmt.Sprintf("%s %s", uikit.SymFail, ev.Document))
				}
			},
			OnItemDone: func(i int, outcome extract.BatchOutcome) {
				if outcome.Action == extract.ActionSkip {
					return
				}
				// During the posting phases, advance the bar for each note.
				// During extraction, OnProgress handles the bar — OnItemDone
				// only fires for cache-populate bookkeeping, not user-visible.
				if curPhase == extract.PhaseExtract {
					return
				}
				name := outcome.Item.Request.PDFName
				if outcome.Err != nil {
					t.Advance("failed", fmt.Sprintf("%s %s: %s", uikit.SymFail, name, outcome.Err))
					return
				}
				t.Advance("posted", fmt.Sprintf("%s %s", uikit.SymOK, name))
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
		Total:          len(items),
		Created:        created,
		Skipped:        skipped,
		Cached:         cached,
		Failed:         failed,
		Duration:       time.Since(started),
		BackfilledTags: backfillTagged,
		BackfillFailed: backfillFailed,
	}
	if failed > 0 {
		result.Errors = make(map[string]string)
		for _, o := range batchResult.Outcomes {
			if o.Err != nil {
				result.Errors[o.Item.Request.ParentKey] = o.Err.Error()
			}
		}
	}
	outputScoped(ctx, cmd, result)
	return nil
}

// backfillHasMarkdownTag adds extract.MarkdownTag to every parent that
// has a docling note in Zotero but is missing the tag on the parent
// item. Drives the saved-search workflow without requiring a separate
// CLI command — every --apply heals the invariant. Returns the (tagged,
// failed) counts for the result struct.
//
// Idempotent: AddTagToItem dedups against the current tag set, so
// running the sweep on an already-consistent library is a no-op (one
// query + zero PATCHes).
func backfillHasMarkdownTag(ctx context.Context, db local.Reader, w extract.TagAdder) (int, int, error) {
	parents, err := db.ParentsWithDoclingNotesMissingTag(extract.MarkdownTag)
	if err != nil {
		return 0, 0, fmt.Errorf("backfill: query parents missing tag: %w", err)
	}
	if len(parents) == 0 {
		return 0, 0, nil
	}

	var res extract.BackfillResult
	err = uikit.RunWithProgress("Backfilling has-markdown tag", func(t *uikit.ProgressTracker) error {
		t.SetTotal(len(parents))
		res = extract.BackfillParentTag(ctx, w, parents, extract.MarkdownTag, func(key string, perr error) {
			if perr != nil {
				t.Advance("failed", fmt.Sprintf("%s %s: %s", uikit.SymFail, key, perr))
			} else {
				t.Advance("tagged", fmt.Sprintf("%s %s", uikit.SymOK, key))
			}
		})
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return len(res.Tagged), len(res.Failed), nil
}
