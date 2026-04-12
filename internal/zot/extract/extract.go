package extract

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OutputFormat mirrors docling's `--to`. We only wire the two formats we
// actually consume: FormatMarkdown (the note body) and FormatJSON
// (DoclingDocument, needed for table post-processing).
type OutputFormat string

const (
	FormatMarkdown OutputFormat = "md"
	FormatJSON     OutputFormat = "json"
)

// ImageMode mirrors docling's `--image-export-mode`. Zotero flow uses
// ImagePlaceholder; vault-export flow uses ImageReferenced.
type ImageMode string

const (
	ImagePlaceholder ImageMode = "placeholder"
	ImageReferenced  ImageMode = "referenced"
	ImageEmbedded    ImageMode = "embedded"
)

// TableMode mirrors docling's `--table-mode`.
type TableMode string

const (
	TableAccurate TableMode = "accurate"
	TableFast     TableMode = "fast"
)

// ExtractOptions is the full option surface of an extraction run. Two
// shapes are blessed for CLI callers:
//
//   - ZoteroDefaults() — minimal, clean markdown, no enrichments, for
//     posting a Zotero child note.
//   - Any struct with OutputDir set + enrichments toggled — full
//     extraction mode, writes JSON + referenced image artifacts + CSVs.
//
// Advanced programmatic callers can mix and match freely.
type ExtractOptions struct {
	// Required
	PDFPath   string
	OutputDir string

	// Output selection — empty means [FormatMarkdown].
	Formats []OutputFormat

	// Rendering
	ImageMode ImageMode // "" → ImagePlaceholder
	TableMode TableMode // "" → TableAccurate

	// Post-processing: walk the JSON output and write one CSV per table
	// to <OutputDir>/<stem>_tables/table-NNN.csv. Implicitly promotes
	// FormatJSON into Formats.
	TablesAsCSV bool

	// Performance
	NumThreads int    // 0 = docling default (4)
	Device     string // "" = docling auto

	// OCR
	DisableOCR bool // --no-ocr (docling defaults to OCR on)
	ForceOCR   bool // --force-ocr
}

// ZoteroDefaults is the minimum-surface option set for the `zot item
// extract` command when the user hasn't passed `--out`. Pure: the CLI
// copies it, sets PDFPath + OutputDir, and goes.
func ZoteroDefaults() ExtractOptions {
	return ExtractOptions{
		Formats:   []OutputFormat{FormatMarkdown},
		ImageMode: ImagePlaceholder,
		TableMode: TableAccurate,
		Device:    "mps",
	}
}

// FullDefaults is the option set used when the user passes `--out`:
// markdown + JSON, referenced PNG artifacts, and tables post-processed
// to CSV. Docling enrichments (code/formula/picture/chart) are
// intentionally not wired — they download extra models, roughly triple
// wall time, and the chart-extraction enrichment has unpatched upstream
// packaging holes as of docling 2.86.
func FullDefaults() ExtractOptions {
	return ExtractOptions{
		Device:      "mps",
		Formats:     []OutputFormat{FormatMarkdown, FormatJSON},
		ImageMode:   ImageReferenced,
		TableMode:   TableAccurate,
		TablesAsCSV: true,
	}
}

// ExtractResult is what the Extractor returns on success. All paths are
// absolute. Fields that weren't requested are zero-valued.
type ExtractResult struct {
	MarkdownPath string
	JSONPath     string
	ImagePaths   []string
	TablePaths   []string
	ToolVersion  string // e.g. "docling 2.86.0"
	Duration     time.Duration
	// FromCache is true when the markdown was served from the
	// MarkdownCache and no docling invocation occurred. Duration is the
	// cache hit's trivial read time, not the original extraction time.
	FromCache bool
}

// BatchExtractResult holds the per-PDF outcomes of a single docling
// batch invocation. Results maps the input PDF path to its outcome.
// FailedDocs lists PDF paths that docling reported as failed.
type BatchExtractResult struct {
	Results     map[string]*ExtractResult // pdfPath → result
	FailedDocs  []string
	ToolVersion string
	Duration    time.Duration
}

// ProgressFunc is called for each parsed docling log event during batch
// extraction. Implementations must be safe to call from a goroutine
// (the stderr reader runs concurrently with the docling process).
type ProgressFunc func(ev *DoclingEvent)

// Extractor is the narrow interface the orchestrator uses. Production
// impl is DoclingExtractor; tests substitute a fake that writes fixture
// markdown without shelling out.
type Extractor interface {
	// Extract converts a single PDF. Used by `zot item extract`.
	Extract(ctx context.Context, opts ExtractOptions) (*ExtractResult, error)
	// ExtractBatch converts multiple PDFs in a single process invocation
	// (models loaded once). Used by `zot extract-lib`.
	ExtractBatch(ctx context.Context, opts ExtractOptions, pdfs []string, onProgress ProgressFunc) (*BatchExtractResult, error)
}

// DoclingExtractor wraps the `docling` CLI binary.
type DoclingExtractor struct {
	// Binary is the absolute path to the docling executable.
	Binary string
	// Stderr, if non-nil, receives docling's stdout+stderr stream. Use
	// this to surface progress in a TUI or to silence it in tests.
	// nil means os.Stderr.
	Stderr io.Writer
}

// NewDoclingExtractor resolves `docling` on PATH and returns an
// extractor wired to it. Returns a helpful error if the binary is
// missing — the CLI layer should surface it verbatim so users know to
// run `sci doctor` (which installs docling via `uv`).
func NewDoclingExtractor() (*DoclingExtractor, error) {
	path, err := exec.LookPath("docling")
	if err != nil {
		return nil, fmt.Errorf("docling binary not found on PATH: %w (install via `sci doctor` or `uv tool install docling`)", err)
	}
	return &DoclingExtractor{Binary: path}, nil
}

// Extract runs docling with the given options, post-processes tables
// if requested, and returns paths to every artifact produced.
func (d *DoclingExtractor) Extract(ctx context.Context, opts ExtractOptions) (*ExtractResult, error) {
	if opts.PDFPath == "" {
		return nil, fmt.Errorf("extract: PDFPath required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("extract: OutputDir required")
	}
	if _, err := os.Stat(opts.PDFPath); err != nil {
		return nil, fmt.Errorf("extract: stat PDF: %w", err)
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("extract: mkdir output: %w", err)
	}

	args := buildDoclingArgs(opts, opts.PDFPath)
	cmd := exec.CommandContext(ctx, d.Binary, args...)
	stderr := d.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout = stderr
	cmd.Stderr = stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docling run: %w", err)
	}
	dur := time.Since(start)

	stem := stemFor(opts.PDFPath)
	mdPath := filepath.Join(opts.OutputDir, stem+".md")
	if _, err := os.Stat(mdPath); err != nil {
		return nil, fmt.Errorf("docling produced no markdown at %s: %w", mdPath, err)
	}
	result := &ExtractResult{
		MarkdownPath: mdPath,
		ToolVersion:  d.Version(ctx),
		Duration:     dur,
	}

	// JSON output (either requested explicitly or promoted by TablesAsCSV).
	if hasFormat(opts.Formats, FormatJSON) || opts.TablesAsCSV {
		result.JSONPath = filepath.Join(opts.OutputDir, stem+".json")
	}

	// Referenced images live in <stem>_artifacts/.
	if opts.ImageMode == ImageReferenced {
		artifacts := filepath.Join(opts.OutputDir, stem+"_artifacts")
		if entries, err := os.ReadDir(artifacts); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					result.ImagePaths = append(result.ImagePaths, filepath.Join(artifacts, e.Name()))
				}
			}
			sort.Strings(result.ImagePaths)
		}
	}

	// Post-process tables.
	if opts.TablesAsCSV {
		csvDir := filepath.Join(opts.OutputDir, stem+"_tables")
		paths, err := writeTablesAsCSV(result.JSONPath, csvDir)
		if err != nil {
			return nil, fmt.Errorf("extract tables: %w", err)
		}
		result.TablePaths = paths
	}

	return result, nil
}

// ExtractBatch runs docling once over multiple PDFs. Models load a
// single time; each PDF is processed sequentially within the same
// process. onProgress fires for every parsed log event from docling's
// stderr (nil is safe — events are simply discarded).
//
// Returns per-PDF results keyed by the original PDF path. PDFs that
// docling could not convert appear in FailedDocs but are not an error
// — callers decide how to handle them.
func (d *DoclingExtractor) ExtractBatch(ctx context.Context, opts ExtractOptions, pdfs []string, onProgress ProgressFunc) (*BatchExtractResult, error) {
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("extractbatch: OutputDir required")
	}
	if len(pdfs) == 0 {
		return &BatchExtractResult{Results: map[string]*ExtractResult{}}, nil
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("extractbatch: mkdir output: %w", err)
	}

	args := buildDoclingArgs(opts, pdfs...)
	cmd := exec.CommandContext(ctx, d.Binary, args...)

	// Capture stderr for progress parsing. Docling's structured log
	// lines go to stderr; stdout is unused by docling CLI.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("extractbatch: stderr pipe: %w", err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("extractbatch: start: %w", err)
	}

	// Parse stderr line-by-line in the foreground while docling runs.
	scanner := bufio.NewScanner(stderrPipe)
	var failedDocs []string
	sink := d.Stderr
	for scanner.Scan() {
		line := scanner.Text()
		// Mirror to the configured sink (TUI, os.Stderr, or nil).
		if sink != nil {
			_, _ = fmt.Fprintln(sink, line)
		}
		ev := ParseDoclingEvent(line)
		if ev == nil {
			continue
		}
		if ev.Kind == EventFailed {
			failedDocs = append(failedDocs, ev.Document)
		}
		if onProgress != nil {
			onProgress(ev)
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("extractbatch: docling exit: %w", err)
	}
	dur := time.Since(start)

	toolVer := d.Version(ctx)

	// Collect results: walk the output dir and match each .md file back
	// to its source PDF via stem matching.
	stemToPDF := make(map[string]string, len(pdfs))
	for _, p := range pdfs {
		stemToPDF[stemFor(p)] = p
	}

	results := make(map[string]*ExtractResult, len(pdfs))
	for stem, pdfPath := range stemToPDF {
		mdPath := filepath.Join(opts.OutputDir, stem+".md")
		if _, err := os.Stat(mdPath); err != nil {
			continue // docling didn't produce output — likely in failedDocs
		}
		res := &ExtractResult{
			MarkdownPath: mdPath,
			ToolVersion:  toolVer,
			Duration:     dur, // total batch duration; per-item not available
		}
		// JSON output.
		if hasFormat(opts.Formats, FormatJSON) || opts.TablesAsCSV {
			jsonPath := filepath.Join(opts.OutputDir, stem+".json")
			if _, err := os.Stat(jsonPath); err == nil {
				res.JSONPath = jsonPath
			}
		}
		// Referenced images.
		if opts.ImageMode == ImageReferenced {
			artifacts := filepath.Join(opts.OutputDir, stem+"_artifacts")
			if entries, err := os.ReadDir(artifacts); err == nil {
				for _, e := range entries {
					if !e.IsDir() {
						res.ImagePaths = append(res.ImagePaths, filepath.Join(artifacts, e.Name()))
					}
				}
				sort.Strings(res.ImagePaths)
			}
		}
		results[pdfPath] = res
	}

	return &BatchExtractResult{
		Results:     results,
		FailedDocs:  failedDocs,
		ToolVersion: toolVer,
		Duration:    dur,
	}, nil
}

// Version probes `docling --version` and returns "docling X.Y.Z", or
// plain "docling" if parsing fails. Cheap (~50ms) — safe to call once
// per extraction.
func (d *DoclingExtractor) Version(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, d.Binary, "--version").Output()
	if err != nil {
		return "docling"
	}
	m := versionRE.FindSubmatch(out)
	if len(m) < 2 {
		return "docling"
	}
	return "docling " + string(m[1])
}

var versionRE = regexp.MustCompile(`Docling version:\s*(\S+)`)

// buildDoclingArgs constructs the argv for a docling invocation. Pure:
// no filesystem, no exec — unit-tested directly. PDF paths are
// appended as trailing positional args.
func buildDoclingArgs(opts ExtractOptions, pdfs ...string) []string {
	formats := opts.Formats
	if len(formats) == 0 {
		formats = []OutputFormat{FormatMarkdown}
	}
	// TablesAsCSV needs JSON; promote silently so the caller doesn't
	// have to remember. The test TablesAsCSV_PromotesJSON locks this.
	if opts.TablesAsCSV && !hasFormat(formats, FormatJSON) {
		formats = append(formats, FormatJSON)
	}

	imageMode := opts.ImageMode
	if imageMode == "" {
		imageMode = ImagePlaceholder
	}
	tableMode := opts.TableMode
	if tableMode == "" {
		tableMode = TableAccurate
	}

	args := []string{"-v", "--no-abort-on-error", "--from", "pdf"}
	for _, f := range formats {
		args = append(args, "--to", string(f))
	}
	args = append(args,
		"--image-export-mode", string(imageMode),
		"--table-mode", string(tableMode),
	)
	if opts.DisableOCR {
		args = append(args, "--no-ocr")
	}
	if opts.ForceOCR {
		args = append(args, "--force-ocr")
	}
	if opts.Device != "" {
		args = append(args, "--device", opts.Device)
	}
	if opts.NumThreads > 0 {
		args = append(args, "--num-threads", strconv.Itoa(opts.NumThreads))
	}
	args = append(args, "--output", opts.OutputDir)
	args = append(args, pdfs...)
	return args
}

func hasFormat(fs []OutputFormat, want OutputFormat) bool {
	for _, f := range fs {
		if f == want {
			return true
		}
	}
	return false
}

// stemFor returns the filename stem used by docling's output naming.
// docling names its outputs `<stem>.md`, `<stem>.json`,
// `<stem>_artifacts/`, where stem is the PDF basename minus its
// extension. Zotero's "undefined" attachments (no extension) keep their
// full basename.
func stemFor(pdfPath string) string {
	base := filepath.Base(pdfPath)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
