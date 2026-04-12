package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// extract-command flag destinations (package-scoped, matching the
// sci-go convention in write.go).
var (
	extractApply      bool
	extractForce      bool
	extractOut        string
	extractNoNote     bool
	extractDelete     bool
	extractYes        bool
	extractDevice     string
	extractNumThreads int
)

// apiChildListerAdapter bridges *api.Client → extract.ChildLister.
// The api package returns []api.NoteChild, extract wants
// []extract.ChildNote — identical shape, one-field projection.
type apiChildListerAdapter struct {
	c *api.Client
}

func (a *apiChildListerAdapter) ListNoteChildren(ctx context.Context, parentKey string) ([]extract.ChildNote, error) {
	items, err := a.c.ListNoteChildren(ctx, parentKey)
	if err != nil {
		return nil, err
	}
	out := make([]extract.ChildNote, len(items))
	for i, it := range items {
		out[i] = extract.ChildNote{Key: it.Key, Body: it.Body}
	}
	return out, nil
}

func extractCommand() *cli.Command {
	return &cli.Command{
		Name:  "extract",
		Usage: "Convert a PDF attachment into a Zotero child note (via docling)",
		Description: "$ zot item extract 6R45EVSB                           # dry-run preview\n" +
			"$ zot item extract 6R45EVSB --apply                    # post clean note to Zotero\n" +
			"$ zot item extract 6R45EVSB --out ./vault/ckd --apply  # full extraction + note\n" +
			"$ zot item extract 6R45EVSB --out ./vault/ckd --no-note --apply  # artifacts only\n" +
			"$ zot item extract 6R45EVSB --delete                   # undo: trash sci-extract note\n" +
			"\n" +
			"Zotero mode (default): clean markdown posted as a child note.\n" +
			"Full mode (--out):     md + json + referenced PNGs + CSV tables written to DIR.\n" +
			"Delete mode (--delete): trash any child note carrying this PDF's sci-extract sentinel.\n" +
			"\n" +
			"Uses the existing PDF attachment's contentType + path from the local zotero.sqlite.\n" +
			"The Plan step is pure (no docling run); pass --apply to actually extract and post.",
		ArgsUsage: "<parent-item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "apply", Usage: "run docling and create/replace the note (default is dry-run)", Destination: &extractApply, Local: true},
			&cli.BoolFlag{Name: "force", Usage: "replace an existing note even if the PDF hash hasn't changed", Destination: &extractForce, Local: true},
			&cli.StringFlag{Name: "out", Usage: "write docling artifacts (md/json/PNGs/CSVs) to DIR; enables full-extraction mode", Destination: &extractOut, Local: true},
			&cli.BoolFlag{Name: "no-note", Usage: "skip the Zotero note post — requires --out (artifacts only)", Destination: &extractNoNote, Local: true},
			&cli.BoolFlag{Name: "delete", Usage: "trash any child note whose sci-extract sentinel matches this PDF (undo a prior extraction)", Destination: &extractDelete, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &extractYes, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "auto", Destination: &extractDevice, Local: true},
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
	if extractDelete && (extractApply || extractForce || extractOut != "" || extractNoNote) {
		return cmdutil.UsageErrorf(cmd, "--delete is mutually exclusive with --apply, --force, --out, and --no-note")
	}

	cfg, db, err := openLocalDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	att, err := db.ResolvePDFAttachment(parentKey)
	if err != nil {
		return err
	}

	// ── --delete: surgical undo, no PDF on disk needed ──
	if extractDelete {
		return runExtractDelete(ctx, cmd, parentKey, att)
	}

	pdfPath := filepath.Join(cfg.DataDir, "storage", att.Key, att.Filename)
	if _, err := os.Stat(pdfPath); err != nil {
		return fmt.Errorf("PDF attachment %s missing on disk at %s: %w", att.Key, pdfPath, err)
	}

	hash, err := extract.HashPDF(pdfPath)
	if err != nil {
		return fmt.Errorf("hash PDF: %w", err)
	}

	// Resolve the output directory. --out is persisted; otherwise a
	// hidden temp dir that gets cleaned up on return.
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

	// Option set: FullDefaults for --out, ZoteroDefaults otherwise.
	var opts extract.ExtractOptions
	if extractOut != "" {
		opts = extract.FullDefaults()
	} else {
		opts = extract.ZoteroDefaults()
	}
	if extractDevice != "" && extractDevice != "auto" {
		// Only pass through non-default devices; docling treats
		// missing --device as "auto" already.
		opts.Device = extractDevice
	}
	opts.NumThreads = extractNumThreads

	// ── --no-note: run docling directly, no plan, no API ──
	if extractNoNote {
		return runExtractOnly(ctx, cmd, parentKey, att, pdfPath, outputDir, opts)
	}

	// ── Plan path: hit the API to enumerate existing note children ──
	apiClient, err := requireAPIClient()
	if err != nil {
		return err
	}
	lister := &apiChildListerAdapter{c: apiClient}

	plan, err := extract.PlanExtract(ctx, lister, extract.PlanRequest{
		ParentKey: parentKey,
		PDFKey:    att.Key,
		PDFName:   att.Title,
		PDFHash:   hash,
		Force:     extractForce,
	})
	if err != nil {
		return err
	}

	// Dry-run: print the plan and stop.
	if !extractApply {
		cmdutil.Output(cmd, zot.ExtractPlanResult{
			ParentKey:    plan.Request.ParentKey,
			PDFKey:       plan.Request.PDFKey,
			PDFName:      plan.Request.PDFName,
			PDFHash:      plan.Request.PDFHash,
			Action:       zot.ActionLabel(plan.Action),
			Reason:       plan.Reason,
			ExistingNote: plan.ExistingNote,
			OutputDir:    outputDir,
			FullMode:     extractOut != "",
		})
		// If caller kept the temp dir for dry-run inspection, suppress cleanup.
		// (Not currently exposed; left intentional.)
		return nil
	}

	// Apply path — confirm destructive actions.
	if plan.Action != extract.ActionSkip {
		verb := zot.ActionLabel(plan.Action)
		if done, err := cmdutil.ConfirmOrSkip(extractYes,
			fmt.Sprintf("%s note for %s (%s)?", verb, att.Title, plan.Reason)); done || err != nil {
			return err
		}
	}

	ex, err := extract.NewDoclingExtractor()
	if err != nil {
		return err
	}
	result, err := extract.Execute(ctx, extract.ExecuteInput{
		Plan:        plan,
		Extractor:   ex,
		Writer:      apiClient,
		PDFPath:     pdfPath,
		OutputDir:   outputDir,
		ExtractOpts: opts,
	})
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
	cmdutil.Output(cmd, apply)
	return nil
}

// runExtractDelete handles the `--delete` path: find child notes
// whose sci-extract sentinel matches this PDF's key and trash each
// one via the standard TrashItem call. Trash-only (never hard-delete)
// — users can restore from Zotero's trash if they change their mind.
//
// Sentinel matching is strict: a note must carry
// `<!-- sci-extract:<PDF_KEY>:<HASH> -->` for us to touch it. Notes
// merely tagged `docling` without the sentinel are left alone — tags
// can be sprayed around by users, but the sentinel comment is
// load-bearing and unlikely to be hand-edited.
func runExtractDelete(ctx context.Context, cmd *cli.Command, parentKey string, att *local.PDFAttachment) error {
	apiClient, err := requireAPIClient()
	if err != nil {
		return err
	}
	children, err := apiClient.ListNoteChildren(ctx, parentKey)
	if err != nil {
		return err
	}

	var matching []api.NoteChild
	for _, nc := range children {
		pdfKey, _, ok := extract.FindSentinel(nc.Body)
		if !ok || pdfKey != att.Key {
			continue
		}
		matching = append(matching, nc)
	}

	if len(matching) == 0 {
		cmdutil.Output(cmd, zot.ExtractDeleteResult{
			ParentKey: parentKey,
			PDFKey:    att.Key,
			PDFName:   att.Title,
		})
		return nil
	}

	msg := fmt.Sprintf("Trash %d sci-extract note(s) for %s?", len(matching), att.Title)
	if done, err := cmdutil.ConfirmOrSkip(extractYes, msg); done || err != nil {
		return err
	}

	result := zot.ExtractDeleteResult{
		ParentKey: parentKey,
		PDFKey:    att.Key,
		PDFName:   att.Title,
	}
	for _, nc := range matching {
		if err := apiClient.TrashItem(ctx, nc.Key); err != nil {
			if result.Failed == nil {
				result.Failed = map[string]string{}
			}
			result.Failed[nc.Key] = err.Error()
			continue
		}
		result.Trashed = append(result.Trashed, nc.Key)
	}
	cmdutil.Output(cmd, result)
	return nil
}

// runExtractOnly handles the `--no-note` path: run docling against the
// PDF, write everything to outputDir, and print the artifact paths.
// No Zotero API calls, no plan, no note.
func runExtractOnly(
	ctx context.Context,
	cmd *cli.Command,
	parentKey string,
	att *local.PDFAttachment,
	pdfPath, outputDir string,
	opts extract.ExtractOptions,
) error {
	if !extractApply {
		// In dry-run we tell the user what would happen — useful for
		// confirming the output path before committing to a 15s run.
		cmdutil.Output(cmd, zot.ExtractPlanResult{
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
	cmdutil.Output(cmd, zot.ExtractArtifactResult{
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
