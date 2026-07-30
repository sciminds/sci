package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
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
	extractLibOut        string
	extractLibPlan       bool
)

// newBatchExtractor constructs the docling extractor for a bulk run. A
// package var so tests can prove --plan never reaches it: under --plan
// this is not called, and docling need not be installed.
var newBatchExtractor = func() (extract.Extractor, error) { return extract.NewDoclingExtractor() }

// pageCounter is the scheduling-workstream seam. nil means the
// page-count estimator has not landed: --plan degrades to counts-only
// (no buckets, no ETA) rather than guessing. When the estimator lands,
// point this at it — e.g. `var pageCounter extract.PageCounter = ...`.
var pageCounter extract.PageCounter

func extractLibCommand() *cli.Command {
	return &cli.Command{
		Name:  "extract-lib",
		Usage: experimental + " Bulk-extract every PDF in the library into Zotero child notes (via docling)",
		Description: "Runs `docling` on every parent item that has a PDF attachment.\n" +
			"\n" +
			"A bare run STILL EXTRACTS — it caches markdown locally without posting\n" +
			"to Zotero. For a true read-only preview use --plan.\n" +
			"Pass --apply to also create child notes in Zotero.\n" +
			"\n" +
			"Re-running after a failure resumes where it left off:\n" +
			"  1. Items whose docling-tagged note already exists in Zotero are skipped (--apply only).\n" +
			"  2. Items whose docling output was cached locally skip re-extraction.\n" +
			"\n" +
			"$ sci zot extract-lib --plan           # preview only: no docling, no writes\n" +
			"$ sci zot extract-lib                  # extract all PDFs to local cache\n" +
			"$ sci zot extract-lib --apply          # extract + post notes to Zotero\n" +
			"$ sci zot extract-lib --apply --yes    # skip confirmation\n" +
			"$ sci zot extract-lib --reextract      # re-run docling, ignore cached output\n" +
			"$ sci zot extract-lib --force --apply  # create new notes even where docling note exists\n" +
			"$ sci zot extract-lib --limit 5        # extract at most 5 items (smoke test)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "plan", Aliases: []string{"dry-run"}, Usage: "report what a run would do and exit — no docling, no cache writes, no Zotero writes", Destination: &extractLibPlan, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "mps", Destination: &extractLibDevice, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads (0 = docling default)", Destination: &extractLibNumThreads, Local: true},
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Usage: "parallel docling processes (0 = single process for all PDFs)", Destination: &extractLibJobs, Local: true},
			&cli.BoolFlag{Name: "apply", Usage: "post extracted notes to Zotero (default is cache-only)", Destination: &extractLibApply, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &extractLibYes, Local: true},
			&cli.BoolFlag{Name: "force", Usage: "create new notes even if docling note already exists", Destination: &extractLibForce, Local: true},
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run extraction from scratch", Destination: &extractLibReextract, Local: true},
			&cli.IntFlag{Name: "limit", Usage: "extract at most N items (for smoke testing)", Destination: &extractLibLimit, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "render markdown as HTML before posting (default is raw markdown)", Destination: &extractLibHTML, Local: true},
			&cli.StringFlag{Name: "out", Usage: "extract_dir for per-key artifact layouts (KEY/KEY.md + KEY.json + images + tables); defaults to extract.dir from zot.json", Destination: &extractLibOut, Local: true},
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
	// --plan is the read-only mode; combining it with the one flag that
	// writes to Zotero is a contradiction, refused before anything runs
	// (including the ssh delegation — a bad command line never crosses
	// the wire).
	if extractLibPlan && extractLibApply {
		return cmdutil.Coded(cmdutil.CodeConflict,
			"--plan and --apply are mutually exclusive — --plan writes nothing").
			WithFix("sci zot extract-lib --plan")
	}
	// Quiet mode (--json) auto-confirms every prompt, so a bare
	// `extract-lib --json` used to launch a full docling run with no
	// confirmation at all — the exact footgun --plan exists to defuse.
	// Require an explicit choice.
	if (uikit.IsQuiet() || cmdutil.IsJSON(cmd)) && !extractLibPlan && !extractLibYes && !extractLibApply {
		return cmdutil.Coded(cmdutil.CodeUsage,
			"extract-lib under --json runs docling with no confirmation — pass --plan to preview or --yes to accept").
			WithFix("sci zot extract-lib --plan --json")
	}

	if handled, err := maybeDelegateExtract(cmd); handled {
		return err
	}

	cfg, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	all, err := db.ListAllPDFAttachments()
	if err != nil {
		return err
	}

	// Layout mode (persistent per-key artifact dirs): --out beats the
	// configured extract.dir; empty means classic Zotero-note-only mode.
	var layout *extract.KeyLayout
	if dir := cmp.Or(extractLibOut, cfg.Extract.Dir); dir != "" {
		layout = &extract.KeyLayout{Dir: dir}
	}

	if len(all) == 0 {
		if extractLibPlan {
			outputScoped(ctx, cmd, zot.NewExtractLibPlanResult(
				extract.Survey{}, extractLibDevice, extractLibJobs, layoutDirOf(layout)))
			return nil
		}
		_, _ = fmt.Fprintln(cmd.Root().Writer, "  no items with PDF attachments found")
		return nil
	}

	// Query local DB for parents that already have docling notes.
	hasExisting, err := db.ParentsWithDoclingNotes()
	if err != nil {
		return err
	}

	// Layout completion is computed ONCE over all candidates and reused
	// by both the reject below and the survey report, so the two can
	// never disagree (Done stats three files per key).
	candidates := len(all)
	doneSet := map[string]bool{}
	var layoutDone *int
	if layout != nil {
		for _, p := range all {
			if layout.Done(p.ParentKey) {
				doneSet[p.ParentKey] = true
			}
		}
		layoutDone = new(len(doneSet))
	}

	// Filter out items that are fully handled. Classic mode: a docling
	// note in Zotero settles it. Layout mode: the note AND the key dir
	// must both exist — either one missing keeps the item in the run
	// (the batch layer extracts and/or posts exactly what's absent).
	alreadyDone := 0
	if !extractLibForce {
		all = lo.Reject(all, func(p local.PDFParent, _ int) bool {
			done := hasExisting[p.ParentKey]
			if layout != nil {
				done = done && doneSet[p.ParentKey]
			}
			if done {
				alreadyDone++
			}
			return done
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
	planJobs := max(extract.BatchJobsDefault(extractLibDevice, runtime.NumCPU()), 4)

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

	// Plan phase — concurrent hashing + plan + survey, shows a spinner.
	// We plan ALL candidates first so the survey can filter out cached
	// items before applying --limit. This ensures --limit picks up the
	// next N truly-unextracted items instead of re-selecting cached ones.
	// BuildSurvey owns the cache filter, --limit slicing, and duplicate
	// detection; the live run consumes its Selected/CachedIdx, which is
	// what keeps --plan's report and the real run from ever diverging.
	var survey extract.Survey
	err = uikit.RunWithSpinner("Planning extraction...", func() error {
		planned := extract.PlanBatch(ctx, reqs, planJobs, extractLibForce, hasExisting)
		survey = extract.BuildSurvey(extract.SurveyInput{
			Items:       planned,
			Cache:       cache,
			Layout:      layout,
			Apply:       extractLibApply,
			Reextract:   extractLibReextract,
			Limit:       extractLibLimit,
			Jobs:        extractLibJobs,
			Device:      extractLibDevice,
			Pages:       pageCounter,
			Candidates:  candidates,
			AlreadyDone: alreadyDone,
			LayoutDone:  layoutDone,
		})
		return nil
	})
	if err != nil {
		return err
	}

	// --plan: report and stop — before the --reextract mutation, before
	// the docling constructor, before any confirmation.
	if extractLibPlan {
		outputScoped(ctx, cmd, zot.NewExtractLibPlanResult(
			survey, extractLibDevice, extractLibJobs, layoutDirOf(layout)))
		return nil
	}

	items := survey.Selected
	cachedIdx := survey.CachedIdx

	// Tally the plan for confirmation. nFresh = needs new extraction,
	// nCachedPost = already cached, only needs posting (--apply only),
	// nLayoutOnly = existing note but missing key dir (layout mode) —
	// extraction runs for the artifacts, no note is posted.
	var nCreate, nSkip, nErr, nFresh, nCachedPost, nLayoutOnly int
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
			if layout != nil && !layout.Done(it.Request.ParentKey) {
				nLayoutOnly++
			} else {
				nSkip++
			}
		}
	}

	// Check if there's anything to do.
	if nCreate == 0 && nLayoutOnly == 0 && nErr == 0 {
		result := zot.ExtractLibResult{
			Total:          len(items),
			Skipped:        nSkip,
			BackfilledTags: backfillTagged,
			BackfillFailed: backfillFailed,
		}
		if layout != nil {
			result.LayoutDir = layout.Dir
		}
		outputScoped(ctx, cmd, cmdutil.WithWarnings(result, dupWarnings(survey)...))
		return nil
	}

	// Surface duplicate PDF content before the user commits to the run
	// — tonight's 332-page book was OCR'd twice because two items
	// carried the same scan. Diagnostic, so it goes to stderr; the
	// --json envelope carries the same fact via Warnings.
	if len(survey.Duplicates) > 0 && !uikit.IsQuiet() {
		fmt.Fprintf(os.Stderr, "\n  %s %d PDF(s) attached to more than one item — %d duplicate extraction(s) queued\n",
			uikit.SymWarn, len(survey.Duplicates), survey.DuplicateWasted)
		for _, g := range survey.Duplicates {
			for _, m := range g.Members {
				fmt.Fprintf(os.Stderr, "      %s  %-30s %s\n", m.ParentKey, m.PDFName, m.Disposition)
			}
		}
		fmt.Fprintf(os.Stderr, "    %s triage: sci zot doctor duplicates\n", uikit.SymArrow)
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
	if nLayoutOnly > 0 {
		msg += fmt.Sprintf(", %d artifacts-only", nLayoutOnly)
	}
	if nErr > 0 {
		msg += fmt.Sprintf(", %d plan errors", nErr)
	}
	msg += fmt.Sprintf(")%s?", mode)
	if layout != nil {
		msg = fmt.Sprintf("%s\n  artifacts → %s", msg, layout.Dir)
	}
	if done, err := cmdutil.ConfirmOrSkip(extractLibYes, msg); done || err != nil {
		return err
	}

	// --reextract: clear cache entries so docling re-runs from scratch.
	// In layout mode also drop the .done markers — Finalize then rebuilds
	// each selected key dir wholesale. Deliberately AFTER the confirm: a
	// declined prompt must not have already cost the cache.
	if extractLibReextract {
		for _, it := range items {
			if it.Err == nil && it.Hash != "" {
				cache.Delete(it.Request.PDFKey, it.Hash)
			}
			if layout != nil && it.Err == nil {
				_ = os.Remove(filepath.Join(layout.KeyDir(it.Request.ParentKey), ".done"))
			}
		}
	}

	// From here on every "please stop" (signal, ssh drop, TUI ctrl+c)
	// must land as a ctx cancel so the docling process group dies with us.
	ctx, stop := extractContext(ctx)
	defer stop()

	// Execute phase — progress display.
	started := time.Now()
	var batchResult *extract.BatchResult

	ex, err := newBatchExtractor()
	if err != nil {
		return err
	}

	err = uikit.RunWithProgressCtx(ctx, "Planning...", func(ctx context.Context, t *uikit.ProgressTracker) error {
		t.SetTotal(nCreate + nLayoutOnly)

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
			Layout:      layout,
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
				// This guard is also what keeps layout mode's per-document
				// finalize callbacks (which fire mid-extraction, including
				// for ActionSkip items) from double-counting the bar —
				// don't remove it without rethinking that accounting.
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
	// An interrupted run (TUI ctrl+c returns ErrInterrupted; a signal or
	// stdin-EOF cancel may instead surface as a nil/err result with the
	// command ctx canceled) reports as one clean "interrupted" error, not
	// a result full of per-item context-canceled noise.
	if errors.Is(err, uikit.ErrInterrupted) || ctx.Err() != nil {
		return errExtractInterrupted()
	}
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
	if layout != nil {
		result.LayoutDir = layout.Dir
		result.LayoutWritten = lo.CountBy(batchResult.Outcomes, func(o extract.BatchOutcome) bool {
			return o.LayoutWritten
		})
	}
	if failed > 0 {
		result.Errors = make(map[string]string)
		for _, o := range batchResult.Outcomes {
			if o.Err != nil {
				result.Errors[o.Item.Request.ParentKey] = o.Err.Error()
			}
		}
	}
	outputScoped(ctx, cmd, cmdutil.WithWarnings(result, dupWarnings(survey)...))
	return nil
}

// layoutDirOf returns the layout's directory, or "" for classic mode.
func layoutDirOf(l *extract.KeyLayout) string {
	if l == nil {
		return ""
	}
	return l.Dir
}

// dupWarnings converts a survey's duplicate groups into the envelope
// warning the plan result also emits, so live runs and --plan speak the
// same warning vocabulary.
func dupWarnings(s extract.Survey) []cmdutil.Warning {
	if len(s.Duplicates) == 0 {
		return nil
	}
	return []cmdutil.Warning{{
		Code: cmdutil.CodeDuplicate,
		Message: fmt.Sprintf("%d PDF(s) are attached to more than one item — %d extraction(s) would run on bytes already queued under another key",
			len(s.Duplicates), s.DuplicateWasted),
		Fix: "sci zot doctor duplicates",
	}}
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
