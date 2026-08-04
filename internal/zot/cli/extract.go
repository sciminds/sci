package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// extract-command flag destinations (package-scoped, matching the
// sci-go convention in write.go).
var (
	extractApply      bool
	extractForce      bool
	extractReextract  bool
	extractHTML       bool
	extractOut        string
	extractNoNote     bool
	extractYes        bool
	extractDevice     string
	extractOCR        bool
	extractNumThreads int
)

func contentExtractCommand() *cli.Command {
	return &cli.Command{
		Name:  "extract",
		Usage: experimental + " Run the docling PDF extraction pipeline",
		Description: "$ sci zot content extract 6R45EVSB                           # dry-run preview\n" +
			"$ sci zot content extract 6R45EVSB --apply                    # post markdown to Zotero\n" +
			"$ sci zot content extract 6R45EVSB --html --apply             # post rendered HTML\n" +
			"$ sci zot content extract 6R45EVSB --out ./vault/ckd --apply  # full extraction + post\n" +
			"$ sci zot content extract 6R45EVSB --out ./vault/ckd --no-note --apply  # artifacts only\n" +
			"\n" +
			"Zotero mode (default): raw markdown with YAML frontmatter posted as a child note (--html for rendered HTML).\n" +
			"Full mode (--out):     md + json + referenced PNGs + CSV tables written to DIR.\n" +
			"\n" +
			"Re-extract in place with `content refresh`; remove with `content drop`.\n" +
			"Uses the existing PDF attachment's contentType + path from the local zotero.sqlite.\n" +
			"The Plan step is pure (no docling run); pass --apply to actually extract and post.",
		ArgsUsage: "<parent-item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "apply", Usage: "run docling and create the note (default is dry-run)", Destination: &extractApply, Local: true},
			&cli.BoolFlag{Name: "force", Usage: "create a new note even if a docling note already exists", Destination: &extractForce, Local: true},
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run extraction from scratch", Destination: &extractReextract, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "render markdown as HTML before posting (default is raw markdown)", Destination: &extractHTML, Local: true},
			&cli.StringFlag{Name: "out", Usage: "write docling artifacts (md/json/PNGs/CSVs) to DIR; enables full-extraction mode", Destination: &extractOut, Local: true},
			&cli.BoolFlag{Name: "no-note", Usage: "skip the Zotero note post — requires --out (artifacts only)", Destination: &extractNoNote, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &extractYes, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "auto", Destination: &extractDevice, Local: true},
			&cli.BoolFlag{Name: "ocr", Usage: "OCR scanned/bitmap content (off by default; needs a working docling OCR engine — install its deps yourself if docling errors)", Destination: &extractOCR, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads (0 = docling default, usually 4)", Destination: &extractNumThreads, Local: true},
		},
		Action: extractAction,
	}
}

func extractAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cmdutil.UsageErrorf(cmd, "expected exactly one item key")
	}
	parentKey := cmd.Args().First()

	if extractNoNote && extractOut == "" {
		return cmdutil.UsageErrorf(cmd, "--no-note requires --out (artifacts need somewhere to go)")
	}
	if extractNoNote && extractHTML {
		return cmdutil.UsageErrorf(cmd, "--html has no effect with --no-note (no note is posted)")
	}
	if extractNoNote && extractReextract {
		return cmdutil.UsageErrorf(cmd, "--reextract has no effect with --no-note (no cache is used in --out mode)")
	}

	if handled, err := maybeDelegateExtract(cmd); handled {
		return err
	}

	cfg, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	att, err := db.ResolvePDFAttachment(parentKey)
	if err != nil {
		return err
	}

	pdfPath := filepath.Join(cfg.DataDir, "storage", att.Key, att.Filename)
	if _, err := os.Stat(pdfPath); err != nil {
		return fmt.Errorf("PDF attachment %s missing on disk at %s: %w", att.Key, pdfPath, err)
	}

	hash, err := extract.HashPDF(pdfPath)
	if err != nil {
		return fmt.Errorf("hash PDF: %w", err)
	}

	// Check local DB for existing docling notes.
	hasExisting, err := db.ParentsWithDoclingNotes()
	if err != nil {
		return err
	}

	// Layout mode: a configured extract.dir routes single extractions
	// into the persistent per-key layout (unless --out redirects to an
	// explicit flat dir, which keeps its historical meaning).
	var layout *extract.KeyLayout
	if extractOut == "" && cfg.Extract.Dir != "" {
		layout = &extract.KeyLayout{Dir: cfg.Extract.Dir}
	}

	// Resolve the output directory.
	outputDir := extractOut
	cleanup := func() {}
	if outputDir == "" {
		tmp, err := os.MkdirTemp("", "sci-extract-*")
		if err != nil {
			return fmt.Errorf("mkdir temp: %w", err)
		}
		outputDir = tmp
		cleanup = func() { _ = os.RemoveAll(tmp) }
	}
	defer cleanup()

	// Option set: full artifacts for --out and layout mode,
	// ZoteroDefaults otherwise.
	var opts extract.ExtractOptions
	if extractOut != "" || layout != nil {
		opts = extract.FullDefaults()
	} else {
		opts = extract.ZoteroDefaults()
	}
	if extractDevice != "" {
		opts.Device = extractDevice
	}
	opts.NumThreads = extractNumThreads
	opts.OCR = extractOCR

	// ── --no-note: run docling directly, no plan, no API ──
	if extractNoNote {
		return runExtractOnly(ctx, cmd, parentKey, att, pdfPath, outputDir, opts)
	}

	plan := extract.PlanExtract(extract.PlanRequest{
		ParentKey: parentKey,
		PDFKey:    att.Key,
		PDFName:   att.Title,
		PDFHash:   hash,
		DOI:       att.DOI,
		Force:     extractForce,
	}, hasExisting[parentKey])

	// Dry-run: print the plan and stop.
	if !extractApply {
		planOut := outputDir
		if layout != nil {
			planOut = layout.KeyDir(parentKey)
		}
		outputScoped(ctx, cmd, zot.ExtractPlanResult{
			ParentKey: plan.Request.ParentKey,
			PDFKey:    plan.Request.PDFKey,
			PDFName:   plan.Request.PDFName,
			PDFHash:   plan.Request.PDFHash,
			Action:    zot.ActionLabel(plan.Action),
			Reason:    plan.Reason,
			OutputDir: planOut,
			FullMode:  extractOut != "" || layout != nil,
		})
		return nil
	}

	if layout != nil {
		return runExtractLayout(ctx, cmd, plan, att, pdfPath, outputDir, opts, layout)
	}

	// Apply path — confirm.
	if plan.Action != extract.ActionSkip {
		verb := zot.ActionLabel(plan.Action)
		if done, err := cmdutil.ConfirmOrSkip(extractYes,
			fmt.Sprintf("%s note for %s (%s)?", verb, att.Title, plan.Reason)); done || err != nil {
			return err
		}
	}

	// Cache: Zotero mode uses the shared cache so a failed post
	// doesn't force re-extraction on retry. Full mode (--out) writes
	// persistent artifacts to a user dir and doesn't benefit from
	// caching.
	var cache *extract.MarkdownCache
	if extractOut == "" {
		cacheDir, err := extract.DefaultCacheDir()
		if err != nil {
			return err
		}
		cache = &extract.MarkdownCache{Dir: cacheDir}
		if extractReextract {
			cache.Delete(att.Key, hash)
		}
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	ex, err := extract.NewDoclingExtractor()
	if err != nil {
		return err
	}
	// Ctrl-C / SIGTERM / ssh drop must cancel ctx so the docling process
	// group dies with us instead of orphaning.
	ctx, stop := extractContext(ctx)
	defer stop()
	result, err := extract.Execute(ctx, extract.ExecuteInput{
		Plan:        plan,
		Extractor:   ex,
		Writer:      apiClient,
		PDFPath:     pdfPath,
		OutputDir:   outputDir,
		ExtractOpts: opts,
		Cache:       cache,
		RenderHTML:  extractHTML,
	})
	if ctx.Err() != nil {
		return errExtractInterrupted()
	}
	if err != nil {
		return err
	}

	apply := zot.ExtractApplyResult{
		ParentKey: plan.Request.ParentKey,
		PDFKey:    plan.Request.PDFKey,
		PDFName:   plan.Request.PDFName,
		Action:    zot.ActionLabel(plan.Action),
		Reason:    plan.Reason,
		NoteKey:   result.NoteKey,
	}
	if result.Extraction != nil {
		apply.ToolVersion = result.Extraction.ToolVersion
		apply.Duration = result.Extraction.Duration
	}
	// In full mode, surface the artifact paths in the result.
	if extractOut != "" && result.Extraction != nil {
		apply.OutputDir = outputDir
		apply.Markdown = result.Extraction.MarkdownPath
		apply.JSONDoc = result.Extraction.JSONPath
		apply.Images = result.Extraction.ImagePaths
		apply.Tables = result.Extraction.TablePaths
	}
	outputScoped(ctx, cmd, apply)
	return nil
}

// runExtractLayout handles the --apply path when a persistent extract
// dir is configured: extraction runs in full form over a staged KEY.pdf
// symlink and lands in the per-key layout, independent of the Zotero
// note. The two stores compose:
//
//   - note missing, layout Done  → note posted from the layout markdown,
//     no docling run
//   - note missing, layout stale → docling once, layout finalized, note posted
//   - note exists,  layout stale → docling once, layout finalized, no note
//   - note exists,  layout Done  → nothing to do
//
// The markdown cache is only consulted when the layout is Done (a cached
// placeholder-mode markdown can't produce the DoclingDocument JSON).
func runExtractLayout(
	ctx context.Context,
	cmd *cli.Command,
	plan *extract.Plan,
	att *local.PDFAttachment,
	pdfPath, outputDir string,
	opts extract.ExtractOptions,
	layout *extract.KeyLayout,
) error {
	parentKey := plan.Request.ParentKey
	if extractReextract {
		_ = os.Remove(filepath.Join(layout.KeyDir(parentKey), ".done"))
	}
	done := layout.Done(parentKey)

	// Nothing missing anywhere.
	if plan.Action == extract.ActionSkip && done {
		outputScoped(ctx, cmd, zot.ExtractApplyResult{
			ParentKey: parentKey,
			PDFKey:    plan.Request.PDFKey,
			PDFName:   plan.Request.PDFName,
			Action:    zot.ActionLabel(plan.Action),
			Reason:    plan.Reason + "; layout dir complete",
			OutputDir: layout.KeyDir(parentKey),
		})
		return nil
	}

	verb := zot.ActionLabel(plan.Action)
	if plan.Action == extract.ActionSkip {
		verb = "extract artifacts (no note)"
	}
	if ok, err := cmdutil.ConfirmOrSkip(extractYes,
		fmt.Sprintf("%s for %s → %s?", verb, att.Title, layout.KeyDir(parentKey))); ok || err != nil {
		return err
	}

	// Ctrl-C / SIGTERM / ssh drop must cancel ctx so the docling process
	// group dies with us instead of orphaning.
	ctx, stop := extractContext(ctx)
	defer stop()

	// Note exists, layout missing: docling for the artifacts only.
	if plan.Action == extract.ActionSkip {
		ex, err := extract.NewDoclingExtractor()
		if err != nil {
			return err
		}
		staged, err := extract.StageKeyPDF(outputDir, parentKey, pdfPath)
		if err != nil {
			return err
		}
		opts.PDFPath = staged
		opts.OutputDir = outputDir
		res, err := ex.Extract(ctx, opts)
		if ctx.Err() != nil {
			return errExtractInterrupted()
		}
		if err != nil {
			return err
		}
		if _, err := layout.Finalize(parentKey, outputDir, pdfPath, res.Duration.Seconds()); err != nil {
			return err
		}
		outputScoped(ctx, cmd, zot.ExtractArtifactResult{
			ParentKey:   parentKey,
			PDFKey:      att.Key,
			PDFName:     att.Title,
			OutputDir:   layout.KeyDir(parentKey),
			Markdown:    layout.MarkdownPath(parentKey),
			JSONDoc:     layout.JSONPath(parentKey),
			ToolVersion: res.ToolVersion,
			Duration:    res.Duration,
		})
		return nil
	}

	// Note needed. When the layout is already Done, serve its markdown
	// through the cache so Execute posts without a docling run.
	var cache *extract.MarkdownCache
	execPDFPath := pdfPath
	if done {
		cacheDir, err := extract.DefaultCacheDir()
		if err != nil {
			return err
		}
		cache = &extract.MarkdownCache{Dir: cacheDir}
		if _, ok := cache.Get(att.Key, plan.Request.PDFHash); !ok {
			md, err := os.ReadFile(layout.MarkdownPath(parentKey))
			if err != nil {
				return fmt.Errorf("read layout markdown: %w", err)
			}
			if _, err := cache.Put(att.Key, plan.Request.PDFHash, md); err != nil {
				return err
			}
		}
	} else {
		staged, err := extract.StageKeyPDF(outputDir, parentKey, pdfPath)
		if err != nil {
			return err
		}
		execPDFPath = staged
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	ex, err := extract.NewDoclingExtractor()
	if err != nil {
		return err
	}
	result, err := extract.Execute(ctx, extract.ExecuteInput{
		Plan:        plan,
		Extractor:   ex,
		Writer:      apiClient,
		PDFPath:     execPDFPath,
		OutputDir:   outputDir,
		ExtractOpts: opts,
		Cache:       cache,
		RenderHTML:  extractHTML,
	})
	if ctx.Err() != nil {
		return errExtractInterrupted()
	}
	if err != nil {
		return err
	}
	if !done {
		secs := 0.0
		if result.Extraction != nil {
			secs = result.Extraction.Duration.Seconds()
		}
		if _, err := layout.Finalize(parentKey, outputDir, pdfPath, secs); err != nil {
			return err
		}
	}

	apply := zot.ExtractApplyResult{
		ParentKey: parentKey,
		PDFKey:    plan.Request.PDFKey,
		PDFName:   plan.Request.PDFName,
		Action:    zot.ActionLabel(plan.Action),
		Reason:    plan.Reason,
		NoteKey:   result.NoteKey,
		OutputDir: layout.KeyDir(parentKey),
		Markdown:  layout.MarkdownPath(parentKey),
		JSONDoc:   layout.JSONPath(parentKey),
	}
	if result.Extraction != nil {
		apply.ToolVersion = result.Extraction.ToolVersion
		apply.Duration = result.Extraction.Duration
	}
	outputScoped(ctx, cmd, apply)
	return nil
}

// runExtractOnly handles the `--no-note` path: run docling against the
// PDF, write everything to outputDir, and print the artifact paths.
func runExtractOnly(
	ctx context.Context,
	cmd *cli.Command,
	parentKey string,
	att *local.PDFAttachment,
	pdfPath, outputDir string,
	opts extract.ExtractOptions,
) error {
	if !extractApply {
		outputScoped(ctx, cmd, zot.ExtractPlanResult{
			ParentKey: parentKey,
			PDFKey:    att.Key,
			PDFName:   att.Title,
			Action:    "extract-only",
			Reason:    "note posting disabled (--no-note)",
			OutputDir: outputDir,
			FullMode:  true,
		})
		return nil
	}

	if done, err := cmdutil.ConfirmOrSkip(extractYes,
		fmt.Sprintf("Run docling on %s → %s?", att.Title, outputDir)); done || err != nil {
		return err
	}

	ex, err := extract.NewDoclingExtractor()
	if err != nil {
		return err
	}
	opts.PDFPath = pdfPath
	opts.OutputDir = outputDir
	res, err := ex.Extract(ctx, opts)
	if err != nil {
		return err
	}
	outputScoped(ctx, cmd, zot.ExtractArtifactResult{
		ParentKey:   parentKey,
		PDFKey:      att.Key,
		PDFName:     att.Title,
		OutputDir:   outputDir,
		Markdown:    res.MarkdownPath,
		JSONDoc:     res.JSONPath,
		Images:      res.ImagePaths,
		Tables:      res.TablePaths,
		ToolVersion: res.ToolVersion,
		Duration:    res.Duration,
	})
	return nil
}
