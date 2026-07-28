package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeStagedOutputs simulates a completed docling run for key in outDir:
// KEY.md, KEY.json (1 table, 2 pictures, 3 pages), KEY_artifacts/ with one
// PNG. Returns outDir for convenience.
func writeStagedOutputs(t *testing.T, outDir, key string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(outDir, key+"_artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "# Title\n\n![img](" + key + "_artifacts/image_000000.png)\n"
	if err := os.WriteFile(filepath.Join(outDir, key+".md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `{
	  "tables": [{"data": {"num_rows": 1, "num_cols": 2, "grid": [[{"text": "a"}, {"text": "b"}]]}}],
	  "pictures": [{}, {}],
	  "pages": {"1": {}, "2": {}, "3": {}}
	}`
	if err := os.WriteFile(filepath.Join(outDir, key+".json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(outDir, key+"_artifacts", "image_000000.png")
	if err := os.WriteFile(png, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	return outDir
}

func TestKeyLayout_FinalizeWritesLegacyShape(t *testing.T) {
	t.Parallel()
	staging := writeStagedOutputs(t, t.TempDir(), "ABCD1234")
	l := &KeyLayout{Dir: t.TempDir()}

	man, err := l.Finalize("ABCD1234", staging, "/real/paper.pdf", 12.5)
	if err != nil {
		t.Fatal(err)
	}

	keyDir := filepath.Join(l.Dir, "ABCD1234")
	for _, rel := range []string{
		"ABCD1234.md",
		"ABCD1234.json",
		filepath.Join("ABCD1234_artifacts", "image_000000.png"),
		filepath.Join("tables", "table-001.csv"),
		"result.json",
		".done",
	} {
		if _, err := os.Stat(filepath.Join(keyDir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	want := LayoutManifest{
		Key: "ABCD1234", Status: "success", Secs: 12.5,
		NTables: 1, NPictures: 2, NPages: 3,
		PDFPath: "/real/paper.pdf",
	}
	if *man != want {
		t.Errorf("manifest = %+v, want %+v", *man, want)
	}

	// result.json round-trips with the legacy field names.
	raw, err := os.ReadFile(filepath.Join(keyDir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"key", "status", "secs", "n_tables", "n_pictures", "n_pages", "pdf_path"} {
		if _, ok := onDisk[field]; !ok {
			t.Errorf("result.json missing legacy field %q: %s", field, raw)
		}
	}

	if !l.Done("ABCD1234") {
		t.Error("Done = false after Finalize")
	}

	// Staged outputs were moved, not copied.
	if _, err := os.Stat(filepath.Join(staging, "ABCD1234.md")); !os.IsNotExist(err) {
		t.Errorf("staging markdown still present after Finalize, stat err = %v", err)
	}
}

// TestKeyLayout_FinalizeReplacesExisting: re-extraction overwrites the key
// dir wholesale — stale artifacts from the previous run must not survive.
func TestKeyLayout_FinalizeReplacesExisting(t *testing.T) {
	t.Parallel()
	l := &KeyLayout{Dir: t.TempDir()}
	stale := filepath.Join(l.Dir, "K1", "leftover.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	staging := writeStagedOutputs(t, t.TempDir(), "K1")
	if _, err := l.Finalize("K1", staging, "/p.pdf", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file survived re-finalize, stat err = %v", err)
	}
}

// TestKeyLayout_FinalizeRequiresJSON: the DoclingDocument JSON is what zen's
// claim-grounding gate verifies quotes against — a run that produced only
// markdown must fail loudly instead of writing a half-usable dir.
func TestKeyLayout_FinalizeRequiresJSON(t *testing.T) {
	t.Parallel()
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, "K2.md"), []byte("# md only"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &KeyLayout{Dir: t.TempDir()}
	if _, err := l.Finalize("K2", staging, "/p.pdf", 1); err == nil {
		t.Error("expected error for missing DoclingDocument JSON")
	}
	if l.Done("K2") {
		t.Error("failed Finalize must not leave a Done dir")
	}
}

// TestKeyLayout_Done: requires .done AND the two payload files, so a
// corrupted dir (marker present, payload gone) re-extracts instead of
// silently serving nothing.
func TestKeyLayout_Done(t *testing.T) {
	t.Parallel()
	l := &KeyLayout{Dir: t.TempDir()}

	if l.Done("NOPE") {
		t.Error("Done on missing dir")
	}

	keyDir := filepath.Join(l.Dir, "K3")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, ".done"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if l.Done("K3") {
		t.Error("Done with marker but no payload")
	}
	for _, name := range []string{"K3.md", "K3.json"} {
		if err := os.WriteFile(filepath.Join(keyDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !l.Done("K3") {
		t.Error("Done = false with marker + payload")
	}
}

func TestStageKeyPDF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "some paper (final).pdf")
	if err := os.WriteFile(real, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	staging := t.TempDir()
	staged, err := StageKeyPDF(staging, "KEY99", real)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(staging, "KEY99.pdf"); staged != want {
		t.Errorf("staged = %q, want %q", staged, want)
	}
	if stemFor(staged) != "KEY99" {
		t.Errorf("stem = %q, want KEY99 (docling names outputs by stem)", stemFor(staged))
	}
	body, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("staged link unreadable: %v", err)
	}
	if string(body) != "pdf" {
		t.Errorf("staged content = %q", body)
	}
}
