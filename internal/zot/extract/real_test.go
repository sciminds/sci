package extract

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realCKDPath returns the filesystem path of the real Chronic Kidney
// Disease test PDF in the user's Zotero library, or an empty string if
// it's not available (CI, fresh clone, etc.). $DOCLING_PDF overrides
// the default path so developers with a different library layout can
// still run the smoke.
func realCKDPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("DOCLING_PDF"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Desktop", "zotero", "storage", "7T798XVD", "undefined")
}

// TestDoclingExtractor_RealCKD_Zotero runs the real docling binary
// against the CKD paper in the user's Zotero library. Opt-in:
//
//	DOCLING=1 just test-pkg ./internal/zot/extract
//
// Gated because:
//   - docling loads ~100MB of models on first run
//   - wall time is ~10-15s even warm
//   - needs a real PDF at a known path
//
// Asserts only smoke-level invariants: markdown exists, is non-trivial,
// and contains a phrase from the paper body. The argv contract is
// exercised by TestBuildDoclingArgs_*; this test's job is to prove
// that contract still produces a well-formed docling invocation.
func TestDoclingExtractor_RealCKD_Zotero(t *testing.T) {
	if os.Getenv("DOCLING") == "" {
		t.Skip("set DOCLING=1 to run real docling smoke test")
	}
	pdf := realCKDPath(t)
	if pdf == "" {
		t.Skip("no home dir; cannot locate CKD PDF")
	}
	if _, err := os.Stat(pdf); err != nil {
		t.Skipf("CKD PDF not available at %s: %v", pdf, err)
	}

	ext, err := NewDoclingExtractor()
	if err != nil {
		t.Skipf("docling not on PATH: %v", err)
	}

	// Silence docling's progress stream so test output stays clean.
	// If the test ever fails, re-run with -v + DOCLING_DEBUG=1 to surface it.
	var sink bytes.Buffer
	if os.Getenv("DOCLING_DEBUG") != "" {
		ext.Stderr = os.Stderr
	} else {
		ext.Stderr = &sink
	}

	opts := ZoteroDefaults()
	opts.PDFPath = pdf
	opts.OutputDir = t.TempDir()

	res, err := ext.Extract(context.Background(), opts)
	if err != nil {
		t.Fatalf("Extract: %v\n--- docling stream ---\n%s", err, sink.String())
	}
	if res.MarkdownPath == "" {
		t.Fatal("MarkdownPath empty")
	}
	body, err := os.ReadFile(res.MarkdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 5000 {
		t.Errorf("markdown only %d bytes, expected >5000 for a 15-page paper", len(body))
	}
	// Body keywords from the CKD paper abstract — not a brittle golden,
	// just proves we parsed real text instead of an empty shell.
	for _, kw := range []string{"kidney", "glomerular"} {
		if !strings.Contains(strings.ToLower(string(body)), kw) {
			t.Errorf("markdown missing expected keyword %q", kw)
		}
	}
	// Zotero-mode must NOT leave base64 images behind — if a regression
	// switches to ImageEmbedded the file would balloon and break notes.
	if strings.Contains(string(body), "data:image/png;base64") {
		t.Error("zotero mode produced base64 images; ImagePlaceholder regression")
	}
	// Placeholder mode emits HTML comments; our MarkdownToNoteHTML
	// replaces them later. Here we just confirm they survived docling.
	if !strings.Contains(string(body), "<!-- image -->") {
		t.Error("expected placeholder image markers in docling output")
	}
	if res.ToolVersion == "" || !strings.HasPrefix(res.ToolVersion, "docling") {
		t.Errorf("ToolVersion = %q, want 'docling X.Y.Z'", res.ToolVersion)
	}
	if res.Duration <= 0 {
		t.Error("Duration not recorded")
	}
}

// TestDoclingExtractor_RealCKD_Full exercises the full-extraction mode
// (referenced images, JSON, tables as CSV). Gated behind DOCLING_FULL=1
// independently of DOCLING=1 because the extra artifacts roughly double
// wall time and chew more disk.
func TestDoclingExtractor_RealCKD_Full(t *testing.T) {
	if os.Getenv("DOCLING_FULL") == "" {
		t.Skip("set DOCLING_FULL=1 to run full-extraction smoke test (downloads enrichment models)")
	}
	pdf := realCKDPath(t)
	if pdf == "" {
		t.Skip("no home dir; cannot locate CKD PDF")
	}
	if _, err := os.Stat(pdf); err != nil {
		t.Skipf("CKD PDF not available at %s: %v", pdf, err)
	}
	ext, err := NewDoclingExtractor()
	if err != nil {
		t.Skipf("docling not on PATH: %v", err)
	}
	var sink bytes.Buffer
	ext.Stderr = &sink

	opts := FullDefaults()
	opts.PDFPath = pdf
	opts.OutputDir = t.TempDir()

	res, err := ext.Extract(context.Background(), opts)
	if err != nil {
		t.Fatalf("Extract: %v\n--- docling stream ---\n%s", err, sink.String())
	}
	if res.JSONPath == "" {
		t.Error("JSONPath empty in full mode")
	}
	if _, err := os.Stat(res.JSONPath); err != nil {
		t.Errorf("JSON not on disk: %v", err)
	}
	// CKD has 6 pictures in referenced mode.
	if len(res.ImagePaths) == 0 {
		t.Error("no referenced images extracted")
	}
	// CKD has at least one table (the KDIGO heatmap).
	if len(res.TablePaths) == 0 {
		t.Error("no tables extracted as CSV")
	}
	// Each table file should be non-empty.
	for _, p := range res.TablePaths {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("table %s stat: %v", p, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("table %s is empty", p)
		}
	}
}
