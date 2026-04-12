package extract

import (
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
}

// Extractor is the narrow interface the orchestrator uses. Production
// impl is DoclingExtractor; tests substitute a fake that writes fixture
// markdown without shelling out.
type Extractor interface {
	Extract(ctx context.Context, opts ExtractOptions) (*ExtractResult, error)
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

	args := buildDoclingArgs(opts)
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
// no filesystem, no exec — unit-tested directly. The positional PDF
// path is always the last element.
func buildDoclingArgs(opts ExtractOptions) []string {
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

	args := []string{"--from", "pdf"}
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
	args = append(args, opts.PDFPath)
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
