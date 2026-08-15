package zot

// The document surfaces — `zot bib`, `zot export`, `zot search --export` and
// `zot item export` — write a file to stdout. Anything sci has to say ABOUT
// the run (cite-key stats, skipped items, unresolved references) belongs on
// stderr via cmdutil.Diagnostician, or `… --format csl-json > refs.json`
// produces a file that is not JSON.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/zot/bib"
)

// Compile-time proof that every streaming document result splits its output.
var (
	_ cmdutil.Diagnostician = LibraryExportResult{}
	_ cmdutil.Diagnostician = LibraryDumpResult{}
	_ cmdutil.Diagnostician = BibResult{}
	_ cmdutil.Diagnostician = ExportResult{}
)

func TestLibraryExportResult_StreamingBodyIsTheWholeStdout(t *testing.T) {
	t.Parallel()
	r := LibraryExportResult{
		Format: "csl-json",
		Body:   "[\n  {\"id\": \"a\"}\n]",
		Stats:  ExportStats{Total: 1, Synthesized: 1, Skipped: 2},
	}
	if got, want := r.Human(), r.Body+"\n"; got != want {
		t.Errorf("Human() = %q, want the body and nothing else (%q)", got, want)
	}
	diag := r.Diagnostics()
	for _, want := range []string{"1 item(s)", "0 pinned", "1 synthesized", "skipped 2"} {
		if !strings.Contains(diag, want) {
			t.Errorf("Diagnostics() = %q, want it to mention %q", diag, want)
		}
	}
}

func TestLibraryExportResult_WithOutPathKeepsTheFooterInHuman(t *testing.T) {
	t.Parallel()
	// -o writes the document to disk, so human output is a status report,
	// not a document — splitting it across two streams would only scatter
	// one message.
	r := LibraryExportResult{
		Format:  "biblatex",
		OutPath: "refs.bib",
		Stats:   ExportStats{Total: 3, Pinned: 3},
	}
	if got := r.Human(); !strings.Contains(got, "refs.bib") || !strings.Contains(got, "3 item(s)") {
		t.Errorf("Human() = %q, want the write confirmation and the stats", got)
	}
	if got := r.Diagnostics(); got != "" {
		t.Errorf("Diagnostics() = %q, want empty when -o was passed", got)
	}
}

func TestLibraryDumpResult_StreamingBodyIsTheWholeStdout(t *testing.T) {
	t.Parallel()
	r := LibraryDumpResult{
		Scope: "personal",
		Body:  "{\"kind\":\"item\"}\n",
		Stats: DumpStats{Items: 1, Collections: 2},
	}
	if got, want := r.Human(), r.Body; got != want {
		t.Errorf("Human() = %q, want the body and nothing else (%q)", got, want)
	}
	diag := r.Diagnostics()
	for _, want := range []string{"1 item(s)", "2 collection(s)", "completeness sidecar"} {
		if !strings.Contains(diag, want) {
			t.Errorf("Diagnostics() = %q, want it to mention %q", diag, want)
		}
	}
}

// C2: "dumped all library to …" is not English, and the merged pool is
// plural by definition.
func TestLibraryDumpResult_MergedScopeReadsAsPlural(t *testing.T) {
	t.Parallel()
	r := LibraryDumpResult{Scope: "all", OutPath: "dump.ndjson", Stats: DumpStats{Items: 1}}
	got := r.Human()
	if strings.Contains(got, "all library ") {
		t.Errorf("Human() = %q, want the merged pool named in the plural", got)
	}
	if !strings.Contains(got, "all libraries") {
		t.Errorf("Human() = %q, want it to say \"all libraries\"", got)
	}
}

func TestBibResult_StreamingBodyIsTheWholeStdout(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "csl-json", Body: "[]", Stats: ExportStats{Total: 1}},
		Files:      []string{"paper.qmd"},
		References: 6,
		Resolved:   1,
		Unresolved: []bib.Unresolved{{Ref: bib.Ref{Raw: "@nope2020", Kind: bib.KindCitekey}, Reason: "no match"}},
	}
	if got, want := r.Human(), "[]\n"; got != want {
		t.Errorf("Human() = %q, want the body and nothing else (%q)", got, want)
	}
	diag := r.Diagnostics()
	for _, want := range []string{"1 item(s)", "6 reference(s)", "unresolved (1)", "@nope2020"} {
		if !strings.Contains(diag, want) {
			t.Errorf("Diagnostics() = %q, want it to mention %q", diag, want)
		}
	}
}

func TestBibResult_WithOutPathKeepsEverythingInHuman(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "biblatex", OutPath: "refs.bib", Stats: ExportStats{Total: 1}},
		Files:      []string{"paper.qmd"},
		References: 2,
		Resolved:   1,
		Unresolved: []bib.Unresolved{{Ref: bib.Ref{Raw: "@nope2020", Kind: bib.KindCitekey}, Reason: "no match"}},
	}
	human := r.Human()
	for _, want := range []string{"refs.bib", "2 reference(s)", "unresolved (1)"} {
		if !strings.Contains(human, want) {
			t.Errorf("Human() = %q, want it to mention %q", human, want)
		}
	}
	if got := r.Diagnostics(); got != "" {
		t.Errorf("Diagnostics() = %q, want empty when -o was passed", got)
	}
}

// I4: `item export` is part of the same family and reported nothing at all.
func TestExportResult_StreamingSplitsBodyAndFooter(t *testing.T) {
	t.Parallel()
	r := ExportResult{Key: "ABC12345", Format: "csl-json", Body: "[{}]"}
	if got, want := r.Human(), "[{}]\n"; got != want {
		t.Errorf("Human() = %q, want the body and nothing else (%q)", got, want)
	}
	diag := r.Diagnostics()
	for _, want := range []string{"1 item(s)", "ABC12345", "csl-json"} {
		if !strings.Contains(diag, want) {
			t.Errorf("Diagnostics() = %q, want it to mention %q", diag, want)
		}
	}
}

func TestExportResult_WithOutPathReportsTheWrite(t *testing.T) {
	t.Parallel()
	r := ExportResult{Key: "ABC12345", Format: "biblatex", OutPath: "ref.bib"}
	if got := r.Human(); !strings.Contains(got, "ref.bib") || !strings.Contains(got, "biblatex") {
		t.Errorf("Human() = %q, want the write confirmation", got)
	}
	if got := r.Diagnostics(); got != "" {
		t.Errorf("Diagnostics() = %q, want empty when -o was passed", got)
	}
}

// I3: one field, one name. `notes list` emitted note_key while `notes read`,
// `item read`, `item list` and `search` all emit key — so a consumer paging
// one verb into the other had to rename the field mid-pipeline.
func TestRealNotesListResult_NamesTheNoteKeyKey(t *testing.T) {
	t.Parallel()
	r := RealNotesListResult{
		Count: 1,
		Total: 1,
		Notes: []local.NoteSummary{{
			NoteKey:   "NOTE0001",
			ParentKey: "KEY1",
			Body:      "b",
			DateAdded: "2024-01-01 00:00:00",
		}},
	}
	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Notes []map[string]any `json:"notes"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Notes) != 1 {
		t.Fatalf("notes = %v, want 1", wire.Notes)
	}
	if _, ok := wire.Notes[0]["note_key"]; ok {
		t.Errorf("notes list still emits note_key: %s", raw)
	}
	if got := wire.Notes[0]["key"]; got != "NOTE0001" {
		t.Errorf("notes[0].key = %v, want NOTE0001: %s", got, raw)
	}
	// The rest of the projection must survive the rename untouched.
	for _, want := range []string{"parent_key", "body", "date_added"} {
		if _, ok := wire.Notes[0][want]; !ok {
			t.Errorf("notes[0] lost field %q: %s", want, raw)
		}
	}
}
