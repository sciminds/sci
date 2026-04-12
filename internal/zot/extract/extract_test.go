package extract

import (
	"strings"
	"testing"
)

// argString is a tiny helper: joins argv with spaces so tests can do
// substring assertions without fighting slice ordering.
func argString(args []string) string { return strings.Join(args, " ") }

// argIndex returns the index of the first occurrence of want in args,
// or -1.
func argIndex(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// FullDefaults must produce md + json + referenced images + table
// CSVs. Table CSVs come from docling's always-on TableFormer — no
// enrichment involved.
func TestFullDefaults_ShapeLocked(t *testing.T) {
	t.Parallel()
	d := FullDefaults()
	if !d.TablesAsCSV {
		t.Error("FullDefaults must produce CSV tables")
	}
	if d.TableMode != TableAccurate {
		t.Errorf("TableMode = %q, want %q", d.TableMode, TableAccurate)
	}
	if d.ImageMode != ImageReferenced {
		t.Errorf("ImageMode = %q, want %q", d.ImageMode, ImageReferenced)
	}
	if !hasFormat(d.Formats, FormatMarkdown) || !hasFormat(d.Formats, FormatJSON) {
		t.Errorf("Formats = %v, want both md and json", d.Formats)
	}
}

func TestZoteroDefaults_IsMinimal(t *testing.T) {
	t.Parallel()
	d := ZoteroDefaults()
	if d.ImageMode != ImagePlaceholder {
		t.Errorf("ImageMode = %q, want %q", d.ImageMode, ImagePlaceholder)
	}
	if d.TableMode != TableAccurate {
		t.Errorf("TableMode = %q, want %q", d.TableMode, TableAccurate)
	}
	if d.TablesAsCSV {
		t.Error("zotero defaults must not emit CSVs")
	}
	if len(d.Formats) != 1 || d.Formats[0] != FormatMarkdown {
		t.Errorf("Formats = %v, want [md]", d.Formats)
	}
}

// TestBuildDoclingArgs_Zotero locks the Zotero-mode command shape: clean
// markdown with no enrichments, no JSON, no CSVs. If we ever regress this
// the note bodies would start carrying megabytes of base64 or broken
// enrichment output.
func TestBuildDoclingArgs_Zotero(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"

	args := buildDoclingArgs(opts, "/tmp/paper.pdf")
	got := argString(args)

	mustContain := []string{
		"--from pdf",
		"--to md",
		"--image-export-mode placeholder",
		"--table-mode accurate",
		"--output /tmp/out",
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("args missing %q; got:\n%s", s, got)
		}
	}
	mustNotContain := []string{
		"--to json",
		"--enrich-code",
		"--enrich-formula",
		"--enrich-picture-classes",
		"--enrich-picture-description",
		"--enrich-chart-extraction",
		"--image-export-mode referenced",
		"--image-export-mode embedded",
	}
	for _, s := range mustNotContain {
		if strings.Contains(got, s) {
			t.Errorf("zotero args must NOT contain %q; got:\n%s", s, got)
		}
	}
	// PDF path must be the last positional arg.
	if args[len(args)-1] != "/tmp/paper.pdf" {
		t.Errorf("last arg = %q, want /tmp/paper.pdf", args[len(args)-1])
	}
}

// TestBuildDoclingArgs_FullMode locks the full-extraction command
// shape: md + json, referenced images, perf knobs passed through
// verbatim. No enrichments — they're intentionally not wired.
func TestBuildDoclingArgs_FullMode(t *testing.T) {
	t.Parallel()
	opts := ExtractOptions{
		OutputDir:   "/tmp/out",
		Formats:     []OutputFormat{FormatMarkdown, FormatJSON},
		ImageMode:   ImageReferenced,
		TableMode:   TableAccurate,
		TablesAsCSV: true,
		Device:      "mps",
		NumThreads:  8,
	}
	args := buildDoclingArgs(opts, "/tmp/paper.pdf")
	got := argString(args)

	mustContain := []string{
		"--from pdf",
		"--to md",
		"--to json",
		"--image-export-mode referenced",
		"--table-mode accurate",
		"--device mps",
		"--num-threads 8",
		"--output /tmp/out",
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("full-mode args missing %q; got:\n%s", s, got)
		}
	}
	// Enrichment flags must never appear — we removed the fields.
	forbidden := []string{
		"--enrich-code",
		"--enrich-formula",
		"--enrich-picture-classes",
		"--enrich-picture-description",
		"--enrich-chart-extraction",
	}
	for _, f := range forbidden {
		if strings.Contains(got, f) {
			t.Errorf("full-mode args must NOT contain %q; got:\n%s", f, got)
		}
	}
	if args[len(args)-1] != "/tmp/paper.pdf" {
		t.Errorf("last arg = %q, want /tmp/paper.pdf", args[len(args)-1])
	}
}

// TablesAsCSV needs DoclingDocument JSON to post-process. If the caller
// forgets to request FormatJSON, the argv builder must promote it
// silently — otherwise the post-processor would silently find no JSON
// file and emit nothing.
func TestBuildDoclingArgs_TablesAsCSV_PromotesJSON(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"
	opts.TablesAsCSV = true // Formats still [md] — must auto-add json

	got := argString(buildDoclingArgs(opts, "/tmp/p.pdf"))
	if !strings.Contains(got, "--to json") {
		t.Errorf("TablesAsCSV must promote --to json; got:\n%s", got)
	}
}

// --num-threads 0 / --device "" must be omitted — docling has its own
// defaults and we don't want to override them with zero values.
func TestBuildDoclingArgs_ZeroPerfKnobsOmitted(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"
	opts.Device = ""    // explicitly clear — ZoteroDefaults now sets "mps"
	opts.NumThreads = 0 // already zero
	args := buildDoclingArgs(opts, "/tmp/p.pdf")
	if argIndex(args, "--device") != -1 {
		t.Errorf("--device must not appear when Device empty; got: %s", argString(args))
	}
	if argIndex(args, "--num-threads") != -1 {
		t.Errorf("--num-threads must not appear when NumThreads zero; got: %s", argString(args))
	}
}

func TestBuildDoclingArgs_EmptyFormats_DefaultsToMarkdown(t *testing.T) {
	t.Parallel()
	opts := ExtractOptions{OutputDir: "/tmp/out"}
	got := argString(buildDoclingArgs(opts, "/tmp/p.pdf"))
	if !strings.Contains(got, "--to md") {
		t.Errorf("empty Formats must default to md; got:\n%s", got)
	}
}

// TestBuildDoclingArgs_MultiplePDFs: batch mode appends all PDF paths
// as positional args after the flags.
func TestBuildDoclingArgs_MultiplePDFs(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"
	pdfs := []string{"/a/one.pdf", "/b/two.pdf", "/c/three.pdf"}
	args := buildDoclingArgs(opts, pdfs...)
	got := argString(args)

	// All three PDFs must be present as the last 3 args.
	if len(args) < 3 {
		t.Fatalf("args too short: %v", args)
	}
	tail := args[len(args)-3:]
	for i, want := range pdfs {
		if tail[i] != want {
			t.Errorf("tail[%d] = %q, want %q", i, tail[i], want)
		}
	}
	// Flags still present.
	if !strings.Contains(got, "--from pdf") {
		t.Errorf("missing --from pdf; got:\n%s", got)
	}
	if !strings.Contains(got, "--output /tmp/out") {
		t.Errorf("missing --output; got:\n%s", got)
	}
}

// TestBuildDoclingArgs_NoPDFs: edge case — zero PDFs should produce
// args with just the flags (caller should not invoke docling, but the
// builder should not panic).
func TestBuildDoclingArgs_NoPDFs(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"
	args := buildDoclingArgs(opts)
	got := argString(args)
	if !strings.Contains(got, "--from pdf") {
		t.Errorf("missing flags; got:\n%s", got)
	}
}

// TestBuildDoclingArgs_DeviceMPS: when Device is "mps" it appears in args.
func TestBuildDoclingArgs_DeviceMPS(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"
	args := buildDoclingArgs(opts, "/tmp/a.pdf")
	got := argString(args)
	if !strings.Contains(got, "--device mps") {
		t.Errorf("args missing --device mps; got: %s", got)
	}
}

// TestZoteroDefaults_DeviceMPS: ZoteroDefaults must default to mps.
func TestZoteroDefaults_DeviceMPS(t *testing.T) {
	t.Parallel()
	d := ZoteroDefaults()
	if d.Device != "mps" {
		t.Errorf("Device = %q, want \"mps\"", d.Device)
	}
}

// TestFullDefaults_DeviceMPS: FullDefaults must default to mps.
func TestFullDefaults_DeviceMPS(t *testing.T) {
	t.Parallel()
	d := FullDefaults()
	if d.Device != "mps" {
		t.Errorf("Device = %q, want \"mps\"", d.Device)
	}
}

// TestBuildDoclingArgs_VerboseFlag: batch mode must include -v so
// the progress parser gets structured log lines.
func TestBuildDoclingArgs_VerboseFlag(t *testing.T) {
	t.Parallel()
	opts := ZoteroDefaults()
	opts.OutputDir = "/tmp/out"
	args := buildDoclingArgs(opts, "/tmp/a.pdf")
	if argIndex(args, "-v") == -1 {
		t.Errorf("args missing -v flag; got: %s", argString(args))
	}
}

func TestStemFor(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/a/b/paper.pdf":        "paper",
		"/a/b/Webster 2017.pdf": "Webster 2017",
		"/a/b/undefined":        "undefined", // CKD library reality
		"noext":                 "noext",
		"/a/multi.dot.name.pdf": "multi.dot.name",
	}
	for in, want := range cases {
		if got := stemFor(in); got != want {
			t.Errorf("stemFor(%q) = %q, want %q", in, got, want)
		}
	}
}
