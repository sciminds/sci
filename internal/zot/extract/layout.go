package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// KeyLayout writes the persistent per-parent-key artifact layout that
// downstream consumers (zen's knowledge layer) read:
//
//	<Dir>/<KEY>/
//	  <KEY>.md                # docling markdown (referenced-image mode)
//	  <KEY>.json              # DoclingDocument — required: zen's claim-grounding
//	                          #   gate verifies verbatim quotes against its text elements
//	  <KEY>_artifacts/*.png   # referenced images
//	  tables/table-NNN.csv    # one CSV per table
//	  result.json             # LayoutManifest
//	  .done                   # completion marker, written last
//
// The shape matches the pre-existing docling-extracts corpus (built by a
// standalone Python driver) so consumers re-point rather than rebuild.
// Deliberate divergences from that legacy corpus: result.json status is
// "success" (not the Python enum repr "ConversionStatus.SUCCESS") and
// table CSVs are numbered table-001 (not table-01).
type KeyLayout struct {
	// Dir is the extract_dir root (Config.Extract.Dir or --out).
	Dir string
}

// LayoutManifest is the result.json sidecar of one key dir — same field
// names as the legacy corpus so one reader handles both.
type LayoutManifest struct {
	Key       string  `json:"key"`
	Status    string  `json:"status"`
	Secs      float64 `json:"secs"`
	NTables   int     `json:"n_tables"`
	NPictures int     `json:"n_pictures"`
	NPages    int     `json:"n_pages"`
	PDFPath   string  `json:"pdf_path"`
}

// KeyDir returns the directory that holds (or will hold) key's artifacts.
func (l *KeyLayout) KeyDir(key string) string { return filepath.Join(l.Dir, key) }

// MarkdownPath returns the path of key's extracted markdown.
func (l *KeyLayout) MarkdownPath(key string) string {
	return filepath.Join(l.KeyDir(key), key+".md")
}

// JSONPath returns the path of key's DoclingDocument JSON.
func (l *KeyLayout) JSONPath(key string) string {
	return filepath.Join(l.KeyDir(key), key+".json")
}

// Done reports whether key has a completed extraction on disk. It
// requires the .done marker AND both payload files, so a corrupted dir
// (marker present, payload missing) re-extracts instead of silently
// serving nothing.
func (l *KeyLayout) Done(key string) bool {
	for _, p := range []string{
		filepath.Join(l.KeyDir(key), ".done"),
		l.MarkdownPath(key),
		l.JSONPath(key),
	} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// StageKeyPDF symlinks pdfPath into stagingDir as <key>.pdf and returns
// the link path. Docling names its outputs after the input file's stem,
// so feeding it the staged link makes every artifact come out key-named
// (<key>.md, <key>.json, <key>_artifacts/) with internally consistent
// image references — no post-hoc rename or link rewriting.
func StageKeyPDF(stagingDir, key, pdfPath string) (string, error) {
	staged := filepath.Join(stagingDir, key+".pdf")
	if err := os.Symlink(pdfPath, staged); err != nil {
		return "", fmt.Errorf("stage %s: %w", key, err)
	}
	return staged, nil
}

// Finalize moves key's docling outputs from outDir (key-stem-named, per
// StageKeyPDF) into the layout, derives tables/ CSVs and the manifest
// from the DoclingDocument, and marks the dir done. An existing key dir
// is replaced wholesale — its contents are regenerable, and stale
// artifacts from a previous extraction must not survive a refresh.
// secs is the wall-clock extraction time recorded in the manifest;
// pdfPath is the real (unstaged) PDF location.
func (l *KeyLayout) Finalize(key, outDir, pdfPath string, secs float64) (*LayoutManifest, error) {
	srcMD := filepath.Join(outDir, key+".md")
	srcJSON := filepath.Join(outDir, key+".json")
	for _, src := range []string{srcMD, srcJSON} {
		if _, err := os.Stat(src); err != nil {
			return nil, fmt.Errorf("finalize %s: docling output missing: %w", key, err)
		}
	}

	keyDir := l.KeyDir(key)
	if err := os.RemoveAll(keyDir); err != nil {
		return nil, fmt.Errorf("finalize %s: clear existing dir: %w", key, err)
	}
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		return nil, fmt.Errorf("finalize %s: mkdir: %w", key, err)
	}

	if err := moveInto(srcMD, l.MarkdownPath(key)); err != nil {
		return nil, fmt.Errorf("finalize %s: %w", key, err)
	}
	if err := moveInto(srcJSON, l.JSONPath(key)); err != nil {
		return nil, fmt.Errorf("finalize %s: %w", key, err)
	}
	// Artifacts dir is optional — a text-only paper has no images.
	srcArt := filepath.Join(outDir, key+"_artifacts")
	if _, err := os.Stat(srcArt); err == nil {
		if err := moveInto(srcArt, filepath.Join(keyDir, key+"_artifacts")); err != nil {
			return nil, fmt.Errorf("finalize %s: %w", key, err)
		}
	}

	if _, err := writeTablesAsCSV(l.JSONPath(key), filepath.Join(keyDir, "tables")); err != nil {
		return nil, fmt.Errorf("finalize %s: %w", key, err)
	}

	man, err := buildManifest(l.JSONPath(key), key, pdfPath, secs)
	if err != nil {
		return nil, fmt.Errorf("finalize %s: %w", key, err)
	}
	raw, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("finalize %s: marshal manifest: %w", key, err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "result.json"), raw, 0o644); err != nil {
		return nil, fmt.Errorf("finalize %s: write manifest: %w", key, err)
	}

	// The marker goes last: a crash anywhere above leaves a dir that
	// Done() rejects, so the next run re-extracts instead of trusting
	// a partial layout.
	if err := os.WriteFile(filepath.Join(keyDir, ".done"), nil, 0o644); err != nil {
		return nil, fmt.Errorf("finalize %s: write done marker: %w", key, err)
	}
	return man, nil
}

// buildManifest derives the LayoutManifest counts from the
// DoclingDocument JSON (tables and pictures are arrays, pages a map
// keyed by page number — only the lengths matter here).
func buildManifest(jsonPath, key, pdfPath string, secs float64) (*LayoutManifest, error) {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", jsonPath, err)
	}
	var doc struct {
		Tables   []json.RawMessage          `json:"tables"`
		Pictures []json.RawMessage          `json:"pictures"`
		Pages    map[string]json.RawMessage `json:"pages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", jsonPath, err)
	}
	return &LayoutManifest{
		Key:       key,
		Status:    "success",
		Secs:      secs,
		NTables:   len(doc.Tables),
		NPictures: len(doc.Pictures),
		NPages:    len(doc.Pages),
		PDFPath:   pdfPath,
	}, nil
}

// moveInto renames src to dst, falling back to copy+delete when the
// rename crosses filesystems (staging lives in the system temp dir, the
// layout wherever the user pointed extract_dir).
func moveInto(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("move %s: %w", src, err)
	}
	if info.IsDir() {
		if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
			return fmt.Errorf("copy dir %s: %w", src, err)
		}
	} else {
		body, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("copy %s: %w", src, err)
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return fmt.Errorf("copy %s: %w", src, err)
		}
	}
	return os.RemoveAll(src)
}
