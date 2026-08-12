package zot

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/pkg/local"
)

func libSample() []local.Item {
	return []local.Item{
		{
			// Pinned — user typed their own key.
			Key:   "PINNED01",
			Type:  "journalArticle",
			Title: "Pinned Paper",
			Date:  "2020",
			Creators: []local.Creator{
				{Type: "author", Last: "Smith", First: "Jane"},
			},
			Fields: map[string]string{"citationKey": "smith2020pinned"},
		},
		{
			// Synthesized — no pinned key.
			Key:   "SYNTH001",
			Type:  "journalArticle",
			Title: "Deep Learning Foundations",
			Date:  "2019",
			Creators: []local.Creator{
				{Type: "author", Last: "Jones"},
			},
		},
	}
}

// nonBibSample is what ListAll hands the exporter: real papers plus the
// note / attachment / annotation rows the NDJSON mirror needs and a
// bibliography must not carry. The three non-papers are deliberately
// titleless, which is the shape they had in the live library — 36
// titleless `@misc` entries in the exported .bib.
func nonBibSample() []local.Item {
	return []local.Item{
		{
			Key:      "PAPER001",
			Type:     "journalArticle",
			Title:    "Predictive Coding",
			Date:     "2020",
			Creators: []local.Creator{{Type: "author", Last: "Friston"}},
		},
		{Key: "NOTE0001", Type: "note"},
		{Key: "ATTACH01", Type: "attachment"},
		{Key: "ANNOT001", Type: "annotation"},
	}
}

func TestExportLibrary_DropsNonBibliographicItems(t *testing.T) {
	t.Parallel()
	body, stats, err := ExportLibrary(nonBibSample(), ExportBibLaTeX, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"NOTE0001", "ATTACH01", "ANNOT001"} {
		if strings.Contains(body, key) {
			t.Errorf("non-bibliographic item %s reached the bibliography:\n%s", key, body)
		}
	}
	// A titleless @misc is the symptom: nothing consuming the .bib can
	// render or verify it, and it inflates the reference count.
	if strings.Contains(body, "@misc{") {
		t.Errorf("bibliography carries a titleless @misc entry:\n%s", body)
	}
	if n := strings.Count(body, "@"); n != 1 {
		t.Errorf("entry count = %d, want 1 (the one paper):\n%s", n, body)
	}
	// Total is what landed in the file; Skipped says what didn't, so the
	// drop is reported rather than silent.
	if stats.Total != 1 {
		t.Errorf("Total = %d, want 1", stats.Total)
	}
	if stats.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3", stats.Skipped)
	}
}

func TestExportLibrary_CSLJSONDropsNonBibliographicItems(t *testing.T) {
	t.Parallel()
	body, stats, err := ExportLibrary(nonBibSample(), ExportCSLJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(body, `"id"`); n != 1 {
		t.Errorf("expected 1 id field, got %d:\n%s", n, body)
	}
	if stats.Total != 1 || stats.Skipped != 3 {
		t.Errorf("stats = %+v, want Total 1 / Skipped 3", stats)
	}
}

func TestExportLibrary_StatsCountsPinnedAndSynthesized(t *testing.T) {
	t.Parallel()
	_, stats, err := ExportLibrary(libSample(), ExportBibLaTeX, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2", stats.Total)
	}
	if stats.Pinned != 1 {
		t.Errorf("Pinned = %d, want 1", stats.Pinned)
	}
	if stats.Synthesized != 1 {
		t.Errorf("Synthesized = %d, want 1", stats.Synthesized)
	}
	if stats.Drifted != 0 {
		t.Errorf("Drifted = %d, want 0", stats.Drifted)
	}
}

func TestExportLibrary_EmitsPinnedKeyVerbatim(t *testing.T) {
	t.Parallel()
	body, _, err := ExportLibrary(libSample(), ExportBibLaTeX, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "@article{smith2020pinned,") {
		t.Errorf("missing pinned key header:\n%s", body)
	}
}

func TestExportLibrary_EmitsSynthesizedKeyWithZoteroSuffix(t *testing.T) {
	t.Parallel()
	body, _, err := ExportLibrary(libSample(), ExportBibLaTeX, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "@article{jones2019-deeplearfoun-SYNTH001,") {
		t.Errorf("missing synthesized key header:\n%s", body)
	}
}

func TestExportLibrary_ZoteroURIOnPinnedEntries(t *testing.T) {
	t.Parallel()
	body, _, err := ExportLibrary(libSample(), ExportBibLaTeX, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Pinned entries get a round-trip zotero:// URI in note (synthesized
	// entries don't need one — the key already contains the Zotero key).
	if !strings.Contains(body, "zotero://select/library/items/PINNED01") {
		t.Errorf("missing zotero:// URI on pinned entry:\n%s", body)
	}
	if strings.Contains(body, "zotero://select/library/items/SYNTH001") {
		t.Errorf("synthesized entry should not carry a zotero:// URI:\n%s", body)
	}
}

func TestExportLibrary_PreservesUserExtraNote(t *testing.T) {
	t.Parallel()
	items := []local.Item{{
		Key:      "PINNED01",
		Type:     "journalArticle",
		Title:    "Paper",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
		Fields: map[string]string{
			"citationKey": "smith2020paper",
			"extra":       "Citation Key: smith2020paper\nThis is my reading note.\nSecond line.",
		},
	}}
	body, _, err := ExportLibrary(items, ExportBibLaTeX, nil)
	if err != nil {
		t.Fatal(err)
	}
	// User prose must survive AND the zotero:// URI must be appended,
	// not overwritten.
	if !strings.Contains(body, "This is my reading note.") {
		t.Errorf("user note content lost:\n%s", body)
	}
	if !strings.Contains(body, "Second line.") {
		t.Errorf("user note second line lost:\n%s", body)
	}
	if !strings.Contains(body, "zotero://select/library/items/PINNED01") {
		t.Errorf("zotero URI not appended:\n%s", body)
	}
}

func TestExportLibrary_DriftDetectionEmitsIDs(t *testing.T) {
	t.Parallel()
	// Previous export synthesized "smith2020-deep" for ABCD1234. Author
	// has since been corrected from "Smith" to "Smithson" — new
	// synthesized prefix is "smithson2020-deep". The old key should
	// appear in biblatex `ids = {...}` for backward compatibility with
	// manuscripts that already cite the old form.
	items := []local.Item{{
		Key:      "ABCD1234",
		Type:     "journalArticle",
		Title:    "Deep Learning",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smithson"}},
	}}
	prev := Keymap{"ABCD1234": "smith2020-deeplear"}
	body, stats, err := ExportLibrary(items, ExportBibLaTeX, prev)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Drifted != 1 {
		t.Errorf("Drifted = %d, want 1", stats.Drifted)
	}
	// v2 title tokens for "Deep Learning" → deep + lear → "deeplear".
	if !strings.Contains(body, "@article{smithson2020-deeplear-ABCD1234,") {
		t.Errorf("new key missing:\n%s", body)
	}
	if !strings.Contains(body, "ids = {smith2020-deeplear-ABCD1234}") {
		t.Errorf("drift alias missing:\n%s", body)
	}
}

func TestExportLibrary_NoDriftWhenPrefixUnchanged(t *testing.T) {
	t.Parallel()
	items := []local.Item{{
		Key:      "ABCD1234",
		Type:     "journalArticle",
		Title:    "Deep Learning",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}}
	prev := Keymap{"ABCD1234": "smith2020-deeplear"}
	body, stats, _ := ExportLibrary(items, ExportBibLaTeX, prev)
	if stats.Drifted != 0 {
		t.Errorf("Drifted = %d, want 0", stats.Drifted)
	}
	if strings.Contains(body, "ids = {") {
		t.Errorf("ids alias emitted without drift:\n%s", body)
	}
}

func TestExportLibrary_ReturnedKeymapTracksSynthesizedPrefixes(t *testing.T) {
	t.Parallel()
	_, stats, _ := ExportLibrary(libSample(), ExportBibLaTeX, nil)
	// Only the synthesized entry should appear in the new keymap —
	// pinned keys are user-owned and not subject to drift detection.
	got := stats.Keymap
	if len(got) != 1 {
		t.Errorf("keymap len = %d, want 1 (synthesized only)", len(got))
	}
	if got["SYNTH001"] != "jones2019-deeplearfoun" {
		t.Errorf("SYNTH001 = %q, want jones2019-deeplearfoun", got["SYNTH001"])
	}
	if _, ok := got["PINNED01"]; ok {
		t.Errorf("pinned entry should not be tracked in keymap: %v", got)
	}
}

func TestExportLibrary_CSLJSONArray(t *testing.T) {
	t.Parallel()
	// CSL-JSON library export should be a single JSON array, not
	// concatenated per-item arrays.
	body, _, err := ExportLibrary(libSample(), ExportCSLJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("CSL-JSON body does not start with array:\n%s", body)
	}
	// Two items → exactly two "id":"…" occurrences.
	if n := strings.Count(body, `"id"`); n != 2 {
		t.Errorf("expected 2 id fields, got %d:\n%s", n, body)
	}
}

func TestKeymap_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".zotero-citekeymap.json")
	original := Keymap{
		"ABCD1234": "smith2020deep",
		"EFGH5678": "jones2019learning",
	}
	if err := SaveKeymap(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKeymap(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded["ABCD1234"] != "smith2020deep" {
		t.Errorf("round-trip mismatch: %v", loaded)
	}
}

func TestLoadKeymap_MissingFileReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, err := LoadKeymap(filepath.Join(dir, "nonexistent.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty keymap, got %v", m)
	}
}
