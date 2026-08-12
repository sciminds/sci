package cli

// Tests for `zot export --format ndjson` — the item-plane mirror. The
// serializer itself is covered in internal/zot/dump_test.go; these pin the
// CLI-side contract: sidecar ordering, the stdout/no-sidecar path, citekey
// enrichment, and the refusal on the bibliography pipeline.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
)

// dumpReader is a local.Reader stub exposing only what runLibraryDump
// touches. Embedding the interface means an unimplemented method panics
// loudly rather than compiling into a silent zero value.
type dumpReader struct {
	local.Reader
	collections []local.Collection
	tags        map[string][]string
	attachments map[string][]local.Attachment
	lastSync    time.Time
	hasSync     bool
	pendingWAL  int64
}

// HydrateAll mirrors the real bulk hydration: fill from the configured
// maps, leaving items absent from them untouched.
func (f dumpReader) HydrateAll(items []local.Item) error {
	for i := range items {
		if t, ok := f.tags[items[i].Key]; ok {
			items[i].Tags = t
		}
		if a, ok := f.attachments[items[i].Key]; ok {
			items[i].Attachments = a
		}
	}
	return nil
}

func (f dumpReader) ListCollections() ([]local.Collection, error) { return f.collections, nil }
func (f dumpReader) SchemaVersion() int                           { return 125 }
func (f dumpReader) LastSync() (time.Time, bool)                  { return f.lastSync, f.hasSync }
func (f dumpReader) PendingWAL() (int64, bool)                    { return f.pendingWAL, f.pendingWAL > 0 }
func (f dumpReader) Close() error                                 { return nil }

func dumpItems() []local.Item {
	return []local.Item{
		{Key: "AAAA1111", Library: "personal", Type: "journalArticle", Version: 3,
			Title: "Predictive coding", Year: 2020,
			Creators: []local.Creator{{Type: "author", Last: "Friston", OrderIdx: 0}}},
		{Key: "BBBB2222", Library: "shared", Type: "book", Version: 9,
			Title: "Argonauts of the Western Pacific", Year: 1961,
			Creators: []local.Creator{{Type: "author", Last: "Malinowski", OrderIdx: 0}}},
	}
}

func TestRunLibraryDump_WritesBodyThenSidecar(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "zotero-items.ndjson")
	db := dumpReader{
		collections: []local.Collection{{Key: "COLL0001", Name: "Priors", Library: "personal"}},
		tags:        map[string][]string{"AAAA1111": {"priors", "has-markdown"}},
		attachments: map[string][]local.Attachment{
			"AAAA1111": {{Key: "ATT00001", ParentKey: "AAAA1111", ContentType: "application/pdf", Filename: "friston2020.pdf"}},
		},
		lastSync: time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), hasSync: true,
		pendingWAL: 4096,
	}

	res, err := runLibraryDump(context.Background(), db, dumpItems(), out)
	if err != nil {
		t.Fatalf("runLibraryDump: %v", err)
	}
	if res.OutPath != out {
		t.Errorf("OutPath = %q, want %q", res.OutPath, out)
	}
	if res.MetaPath != out+".meta.json" {
		t.Errorf("MetaPath = %q, want the .meta.json sidecar", res.MetaPath)
	}
	if res.Stats.Items != 2 || res.Stats.Collections != 1 {
		t.Errorf("stats = %+v, want 2 items / 1 collection", res.Stats)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimRight(string(body), "\n"), "\n") + 1; n != 3 {
		t.Errorf("body has %d lines, want 3", n)
	}
	// ListAll returns bare rows; tags, collections, and attachments are
	// exactly the item-plane joins the mirror exists to carry. Shipping
	// the dump without them is the bug this asserts against.
	for _, want := range []string{`"tags"`, `"attachments"`, "friston2020.pdf", "has-markdown"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("dump body is missing hydrated field %s", want)
		}
	}

	raw, err := os.ReadFile(res.MetaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta zot.DumpMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v", err)
	}
	// The WAL gap is the local reader's known blind spot (immutable mode
	// cannot see uncheckpointed commits). Carrying it into the sidecar is
	// what lets the consumer report its own staleness honestly.
	if meta.PendingWAL != 4096 {
		t.Errorf("meta.PendingWAL = %d, want 4096", meta.PendingWAL)
	}
	if meta.LastSync == "" {
		t.Error("meta.LastSync is empty despite a known sync time")
	}
	if meta.SchemaVersion != 125 {
		t.Errorf("meta.SchemaVersion = %d, want 125", meta.SchemaVersion)
	}
}

func TestRunLibraryDump_NoOutSkipsSidecarAndSaysSo(t *testing.T) {
	res, err := runLibraryDump(context.Background(), dumpReader{}, dumpItems(), "")
	if err != nil {
		t.Fatalf("runLibraryDump: %v", err)
	}
	if res.OutPath != "" || res.MetaPath != "" {
		t.Errorf("streaming run wrote paths: out=%q meta=%q", res.OutPath, res.MetaPath)
	}
	if res.Body == "" {
		t.Fatal("streaming run produced no body")
	}
	// A dump with no completeness signal must announce itself; silence
	// here is how a partial file gets mistaken for a whole one.
	if !strings.Contains(res.Human(), "sidecar not written") {
		t.Errorf("human output does not flag the missing sidecar:\n%s", res.Human())
	}
}

func TestRunLibraryDump_EnrichesCitekeys(t *testing.T) {
	items := []local.Item{{
		Key: "CCCC3333", Library: "personal", Type: "journalArticle",
		Title: "Mentalizing", Year: 2019,
		// A BBT key stashed in Extra: stored-citationKey is empty, so
		// without enrichment the mirror's citekey column would be blank.
		Extra:    "Citation Key: jolly2019-mentalizing",
		Creators: []local.Creator{{Type: "author", Last: "Jolly", OrderIdx: 0}},
	}}
	res, err := runLibraryDump(context.Background(), dumpReader{}, items, "")
	if err != nil {
		t.Fatalf("runLibraryDump: %v", err)
	}
	if !strings.Contains(res.Body, "jolly2019-mentalizing") {
		t.Errorf("citekey was not enriched into the dump:\n%s", res.Body)
	}
}

func TestRunLibraryExport_RefusesNDJSON(t *testing.T) {
	_, err := runLibraryExport(dumpItems(), "ndjson", "")
	if err == nil {
		t.Fatal("want an error: ndjson is not a bibliography format")
	}
	// The error has to name the command that does work, not just say no.
	if !strings.Contains(err.Error(), "sci zot export --format ndjson") {
		t.Errorf("error does not name the right command: %v", err)
	}
}
