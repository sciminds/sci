package zot_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
)

func sampleDump() zot.DumpInput {
	return zot.DumpInput{
		Scope:         "all",
		SchemaVersion: 125,
		Items: []local.Item{
			{
				Key: "AAAA1111", Library: "personal", Type: "journalArticle",
				Version: 42, Title: "Conversational features", Date: "2022-03-01 2022",
				Year: 2022, DOI: "10.1000/abc", Citekey: "schmidt2022-conversational-AAAA1111",
				Creators:    []local.Creator{{Type: "author", First: "K", Last: "Schmidt", OrderIdx: 0}},
				Tags:        []string{"social"},
				Collections: []string{"COLL0001"},
				Attachments: []local.Attachment{{Key: "ATT00001", ParentKey: "AAAA1111", ContentType: "application/pdf", Filename: "paper.pdf"}},
			},
			{
				Key: "BBBB2222", Library: "shared", Type: "book",
				Version: 7, Title: "Argonauts of the Western Pacific", Year: 1961,
			},
		},
		Collections: []local.Collection{
			{Key: "COLL0001", Name: "Social Cognition", Library: "personal", ItemCount: 12},
		},
	}
}

// decodeNDJSON parses each line into a generic map, failing on any line
// that is not a self-contained JSON object — the property that makes the
// format streamable and resumable on the consumer side.
func decodeNDJSON(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for i, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d is not a JSON object: %v\n%s", i+1, err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestDumpNDJSONEmitsKindTaggedRecords(t *testing.T) {
	var buf bytes.Buffer
	stats, err := zot.DumpNDJSON(&buf, sampleDump())
	if err != nil {
		t.Fatalf("DumpNDJSON: %v", err)
	}
	if stats.Items != 2 || stats.Collections != 1 {
		t.Fatalf("stats = %+v, want 2 items / 1 collection", stats)
	}

	recs := decodeNDJSON(t, buf.String())
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	// Collections precede items so a streaming consumer can resolve an
	// item's collection keys without buffering the whole dump.
	wantKinds := []string{"collection", "item", "item"}
	for i, want := range wantKinds {
		if got := recs[i]["kind"]; got != want {
			t.Errorf("record %d kind = %v, want %s", i, got, want)
		}
	}
}

func TestDumpNDJSONCarriesLibraryProvenanceOnEveryRecord(t *testing.T) {
	var buf bytes.Buffer
	if _, err := zot.DumpNDJSON(&buf, sampleDump()); err != nil {
		t.Fatalf("DumpNDJSON: %v", err)
	}
	// Under --library all the top-level scope is meaningless; per-row
	// provenance is the only correct answer. Every record must carry it.
	for i, rec := range decodeNDJSON(t, buf.String()) {
		lib, ok := rec["library"].(string)
		if !ok || lib == "" {
			t.Errorf("record %d has no library provenance: %v", i, rec)
		}
	}
}

func TestDumpNDJSONPreservesItemShape(t *testing.T) {
	var buf bytes.Buffer
	if _, err := zot.DumpNDJSON(&buf, sampleDump()); err != nil {
		t.Fatalf("DumpNDJSON: %v", err)
	}
	recs := decodeNDJSON(t, buf.String())
	item := recs[1]

	// The dump is a mirror, not a projection: the consumer builds its item
	// plane straight off these fields, so a rename here is a breaking change.
	for _, k := range []string{"key", "type", "version", "title", "doi", "citekey", "creators", "tags", "collections", "attachments"} {
		if _, ok := item[k]; !ok {
			t.Errorf("item record missing %q: %v", k, item)
		}
	}
	if item["version"].(float64) != 42 {
		t.Errorf("version = %v, want 42", item["version"])
	}
	// Zotero stores date as "YYYY-MM-DD originalText"; the raw value must
	// survive so the consumer can parse it itself rather than trusting a
	// lossy pre-clean.
	if got := item["date"]; got != "2022-03-01 2022" {
		t.Errorf("date = %v, want the raw Zotero form", got)
	}
}

func TestDumpNDJSONOmitsLocalPaths(t *testing.T) {
	var buf bytes.Buffer
	if _, err := zot.DumpNDJSON(&buf, sampleDump()); err != nil {
		t.Fatalf("DumpNDJSON: %v", err)
	}
	// Attachment paths are mbp-local. The dump is consumed on other
	// machines where they resolve to nothing, so a path in the dump is a
	// broken promise rather than a convenience.
	if strings.Contains(buf.String(), "/Users/") || strings.Contains(buf.String(), "local_path") {
		t.Errorf("dump leaked a local filesystem path:\n%s", buf.String())
	}
}

func TestWriteDumpMetaLandsAfterTheBody(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "zotero-items.ndjson")
	if err := os.WriteFile(out, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := zot.DumpMeta{
		Scope:         "all",
		SchemaVersion: 125,
		Stats:         zot.DumpStats{Items: 2, Collections: 1},
	}
	path, err := zot.WriteDumpMeta(out, meta)
	if err != nil {
		t.Fatalf("WriteDumpMeta: %v", err)
	}
	if want := out + ".meta.json"; path != want {
		t.Fatalf("meta path = %s, want %s", path, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got zot.DumpMeta
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("meta is not valid JSON: %v", err)
	}
	// dumped_at is stamped by the writer, not the caller — it is what tells
	// a consumer how stale its item plane is.
	if got.DumpedAt == "" {
		t.Error("meta has no dumped_at")
	}
	if got.ProducedBy == "" {
		t.Error("meta has no produced_by")
	}
	if got.Stats.Items != 2 {
		t.Errorf("meta stats = %+v, want 2 items", got.Stats)
	}
	// The sidecar is the completeness signal: it must record the body's
	// digest so a truncated dump cannot be mistaken for a whole one.
	if got.SHA256 == "" {
		t.Error("meta has no sha256 of the body")
	}
}

func TestWriteDumpMetaFailsWhenBodyMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := zot.WriteDumpMeta(filepath.Join(dir, "nope.ndjson"), zot.DumpMeta{})
	if err == nil {
		t.Fatal("want an error when the body does not exist")
	}
}
