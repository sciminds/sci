package cli

// Tests for `zot info --orient` — agent-bootstrap signals over a seeded DB.
// The minimal-DB helper in info_test.go (seedMinimalDB) is too sparse for
// orient queries to return useful rows, so this file maintains its own
// richer fixture.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// seedOrientDB builds a personal-library zotero.sqlite with:
//   - 4 content items (3 unique tags, one with has-markdown)
//   - 1 collection containing 3 items (top collection by count)
//   - varied dateAdded so RecentlyAdded ordering is testable
func seedOrientDB(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, "zotero.sqlite")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`CREATE TABLE version (schema TEXT PRIMARY KEY, version INTEGER)`,
		`CREATE TABLE libraries (libraryID INTEGER PRIMARY KEY, type TEXT, version INTEGER)`,
		`CREATE TABLE groups (libraryID INTEGER PRIMARY KEY, groupID INTEGER UNIQUE, name TEXT)`,
		`CREATE TABLE itemTypes (itemTypeID INTEGER PRIMARY KEY, typeName TEXT UNIQUE)`,
		`CREATE TABLE fields (fieldID INTEGER PRIMARY KEY, fieldName TEXT UNIQUE)`,
		`CREATE TABLE itemDataValues (valueID INTEGER PRIMARY KEY, value TEXT UNIQUE)`,
		`CREATE TABLE items (itemID INTEGER PRIMARY KEY, itemTypeID INTEGER, libraryID INTEGER, key TEXT, version INTEGER, dateAdded TEXT, dateModified TEXT, clientDateModified TEXT)`,
		`CREATE TABLE itemData (itemID INTEGER, fieldID INTEGER, valueID INTEGER, PRIMARY KEY (itemID, fieldID))`,
		`CREATE TABLE deletedItems (itemID INTEGER PRIMARY KEY)`,
		`CREATE TABLE creators (creatorID INTEGER PRIMARY KEY, firstName TEXT, lastName TEXT, fieldMode INTEGER)`,
		`CREATE TABLE creatorTypes (creatorTypeID INTEGER PRIMARY KEY, creatorType TEXT)`,
		`CREATE TABLE itemCreators (itemID INTEGER, creatorID INTEGER, creatorTypeID INTEGER, orderIndex INTEGER, PRIMARY KEY (itemID, orderIndex))`,
		`CREATE TABLE tags (tagID INTEGER PRIMARY KEY, name TEXT UNIQUE, type INTEGER)`,
		`CREATE TABLE itemTags (itemID INTEGER, tagID INTEGER, type INTEGER, PRIMARY KEY (itemID, tagID))`,
		`CREATE TABLE collections (collectionID INTEGER PRIMARY KEY, collectionName TEXT, libraryID INTEGER, parentCollectionID INTEGER, key TEXT)`,
		`CREATE TABLE collectionItems (collectionID INTEGER, itemID INTEGER, PRIMARY KEY (collectionID, itemID))`,
		`CREATE TABLE itemAttachments (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, linkMode INTEGER, contentType TEXT, path TEXT)`,
		`CREATE TABLE itemNotes (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, note TEXT, title TEXT)`,

		`INSERT INTO version VALUES ('userdata', 125)`,
		`INSERT INTO libraries VALUES (1, 'user', 1)`,
		`INSERT INTO itemTypes VALUES (1, 'journalArticle')`,
		`INSERT INTO fields VALUES (1, 'title'), (2, 'date'), (3, 'DOI'), (4, 'publicationTitle')`,
		`INSERT INTO itemDataValues VALUES
			(1, 'Paper One'),
			(2, 'Paper Two'),
			(3, 'Paper Three'),
			(4, 'Paper Four'),
			(5, '2024-01-15')`,

		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified) VALUES
			(1, 1, 1, 'KEY1', 1, '2024-01-01 10:00:00', '2024-01-01 10:00:00', '2024-01-01 10:00:00'),
			(2, 1, 1, 'KEY2', 1, '2024-02-01 10:00:00', '2024-02-01 10:00:00', '2024-02-01 10:00:00'),
			(3, 1, 1, 'KEY3', 1, '2024-03-01 10:00:00', '2024-03-01 10:00:00', '2024-03-01 10:00:00'),
			(4, 1, 1, 'KEY4', 1, '2024-04-01 10:00:00', '2024-04-01 10:00:00', '2024-04-01 10:00:00')`,

		`INSERT INTO itemData VALUES
			(1, 1, 1), (1, 2, 5),
			(2, 1, 2),
			(3, 1, 3),
			(4, 1, 4)`,

		// Tag taxonomy:
		// "ml"          → 3 items (1, 2, 3)   — top user tag
		// "neuro"       → 1 item  (4)         — second
		// "has-markdown"→ 2 items (1, 2)      — must be filtered from TopTags
		`INSERT INTO tags VALUES
			(1, 'ml', 0),
			(2, 'neuro', 0),
			(3, 'has-markdown', 0)`,
		`INSERT INTO itemTags VALUES
			(1, 1, 0), (1, 3, 0),
			(2, 1, 0), (2, 3, 0),
			(3, 1, 0),
			(4, 2, 0)`,

		`INSERT INTO collections VALUES
			(1, 'Active reading', 1, NULL, 'COLLA'),
			(2, 'Archive', 1, NULL, 'COLLB')`,
		`INSERT INTO collectionItems VALUES
			(1, 1), (1, 2), (1, 3),
			(2, 4)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

// withOrientConfig points zot at a temp XDG dir + dataDir seeded by
// seedOrientDB. Personal library only (no shared group).
func withOrientConfig(t *testing.T) string {
	t.Helper()
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	dataDir := t.TempDir()
	seedOrientDB(t, dataDir)

	cfg := &zot.Config{
		APIKey:  "k",
		UserID:  "42",
		DataDir: dataDir,
	}
	if err := zot.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return dataDir
}

// runOrient executes the cli with args and returns captured stdout.
// Mirrors runInfo but uses the orient-friendly seed.
func runOrient(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	done := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- buf
	}()

	var jsonFlag bool
	root := &cli.Command{
		Name: "zot",
		Flags: append([]cli.Flag{
			cmdutil.JSONFlag(&jsonFlag),
		}, PersistentFlags()...),
		Before:   ValidateLibraryBefore,
		Commands: Commands(),
	}
	full := slices.Concat([]string{"zot"}, args)
	runErr := root.Run(context.Background(), full)

	_ = w.Close()
	stdout := <-done
	return stdout, runErr
}

func TestInfo_OrientFlag_PopulatesAllFields(t *testing.T) {
	withOrientConfig(t)

	// Ensure no leftover state between subtests.
	t.Cleanup(func() { infoOrient = false })

	out, err := runOrient(t, "--json", "info", "--library", "personal", "--orient")
	if err != nil {
		t.Fatalf("info --orient: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON in output: %q", string(out))
	}
	var result zot.StatsResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out[jsonStart:]))
	}

	// Extraction coverage: 2/4 = 50%.
	if result.ExtractionCoverage == nil {
		t.Fatal("ExtractionCoverage is nil — should be populated under --orient")
	}
	if result.ExtractionCoverage.WithExtraction != 2 {
		t.Errorf("WithExtraction = %d, want 2", result.ExtractionCoverage.WithExtraction)
	}
	if result.ExtractionCoverage.TotalItems != 4 {
		t.Errorf("TotalItems = %d, want 4", result.ExtractionCoverage.TotalItems)
	}
	if result.ExtractionCoverage.Percent != 50.0 {
		t.Errorf("Percent = %.1f, want 50.0", result.ExtractionCoverage.Percent)
	}

	// Top tags: ml (3), neuro (1). has-markdown filtered.
	if len(result.TopTags) != 2 {
		t.Fatalf("TopTags len = %d, want 2 (ml, neuro): %+v", len(result.TopTags), result.TopTags)
	}
	if result.TopTags[0].Name != "ml" || result.TopTags[0].Count != 3 {
		t.Errorf("top tag = %+v, want {ml 3}", result.TopTags[0])
	}
	for _, tg := range result.TopTags {
		if tg.Name == "has-markdown" {
			t.Errorf("has-markdown should be filtered from TopTags")
		}
	}

	// Top collections: Active reading (3), Archive (1).
	if len(result.TopCollections) != 2 {
		t.Fatalf("TopCollections len = %d, want 2", len(result.TopCollections))
	}
	if result.TopCollections[0].Name != "Active reading" || result.TopCollections[0].Count != 3 {
		t.Errorf("top collection = %+v, want {Active reading 3}", result.TopCollections[0])
	}

	// Recent added: KEY4 (newest), KEY3, KEY2, KEY1. Limit is 5 but only 4
	// items exist.
	if len(result.RecentAdded) != 4 {
		t.Fatalf("RecentAdded len = %d, want 4", len(result.RecentAdded))
	}
	if result.RecentAdded[0].Key != "KEY4" {
		t.Errorf("most recent = %q, want KEY4", result.RecentAdded[0].Key)
	}
	if result.RecentAdded[0].DateAdded != "2024-04-01" {
		t.Errorf("DateAdded = %q, want 2024-04-01", result.RecentAdded[0].DateAdded)
	}
}

func TestInfo_NoOrientFlag_OmitsOrientFields(t *testing.T) {
	withOrientConfig(t)
	t.Cleanup(func() { infoOrient = false })

	out, err := runOrient(t, "--json", "info", "--library", "personal")
	if err != nil {
		t.Fatalf("info: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON: %q", string(out))
	}

	// Don't unmarshal into StatsResult — that would silently set fields to
	// zero/nil regardless. Inspect the raw object instead.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out[jsonStart:], &raw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, key := range []string{"extraction_coverage", "top_tags", "top_collections", "recent_added"} {
		if _, ok := raw[key]; ok {
			t.Errorf("key %q should be omitted without --orient (omitempty)", key)
		}
	}
}

func TestInfo_OrientFlag_NoLibraryFlag_PopulatesEachLibrary(t *testing.T) {
	withTestConfig(t, "42", "6506098") // both libraries configured (minimal DB)
	t.Cleanup(func() { infoOrient = false })

	out, err := runOrient(t, "--json", "info", "--orient")
	if err != nil {
		t.Fatalf("info --orient: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON: %q", string(out))
	}
	var result zot.MultiStatsResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(result.PerLibrary) != 2 {
		t.Fatalf("per_library len = %d, want 2", len(result.PerLibrary))
	}
	// Both libraries should carry an extraction_coverage block (even if
	// the count is zero) — proves orient flowed into both branches.
	for i, entry := range result.PerLibrary {
		if entry.ExtractionCoverage == nil {
			t.Errorf("[%d] ExtractionCoverage nil under --orient (multi-library path)", i)
		}
	}
}
