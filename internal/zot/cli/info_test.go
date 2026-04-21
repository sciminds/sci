package cli

// Tests for `zot info` — two-library summary when --library is absent, and
// narrowed output when --library is supplied.

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

var testJSONOutput bool

// withTestConfig writes a fake zot config + empty zotero.sqlite into a
// temp XDG dir and cleans up after the test. Returns the dataDir path so
// callers can point the (fixture-less) DB open at it if they want.
// For stats-level tests, Stats() against a completely empty DB is enough
// to prove the two-library dispatch works end-to-end.
func withTestConfig(t *testing.T, userID, sharedGroupID string) string {
	t.Helper()
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	dataDir := t.TempDir()
	// Minimal zotero.sqlite containing both the user and group rows plus
	// the groups-table join target that local.ForGroupByAPIID needs.
	seedMinimalDB(t, dataDir, sharedGroupID)

	cfg := &zot.Config{
		APIKey:          "k",
		UserID:          userID,
		SharedGroupID:   sharedGroupID,
		SharedGroupName: "sciminds",
		DataDir:         dataDir,
	}
	if err := zot.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return dataDir
}

// seedMinimalDB creates a zotero.sqlite at dir/zotero.sqlite with just
// enough tables for local.Stats() to return empty rows for both libraries.
func seedMinimalDB(t *testing.T, dir, sharedGroupID string) {
	t.Helper()
	path := filepath.Join(dir, "zotero.sqlite")
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
		`CREATE TABLE itemAttachments (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, linkMode INTEGER, contentType TEXT, path TEXT)`,
		`CREATE TABLE itemNotes (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, note TEXT, title TEXT)`,
		`INSERT INTO version VALUES ('userdata', 125)`,
		`INSERT INTO libraries VALUES (1, 'user', 1)`,
		`INSERT INTO itemTypes VALUES (1, 'journalArticle')`,
		`INSERT INTO fields VALUES (1, 'title'), (2, 'date'), (3, 'DOI'), (4, 'publicationTitle')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	// Only seed the group rows when a shared group is configured; an
	// empty sharedGroupID means the account has no shared library.
	if sharedGroupID != "" {
		if _, err := db.Exec(`INSERT INTO libraries VALUES (2, 'group', 1)`); err != nil {
			t.Fatalf("seed group library: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO groups VALUES (2, ?, 'sciminds')`, sharedGroupID); err != nil {
			t.Fatalf("seed group row: %v", err)
		}
	}
}

// runInfo builds a root matching the entry points and runs it with args.
// Captures stdout so JSON output can be parsed by tests. Returns the raw
// bytes plus any Run error.
func runInfo(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	// Capture stdout while the command runs.
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

	root := &cli.Command{
		Name: "zot",
		Flags: append([]cli.Flag{
			cmdutil.JSONFlag(&testJSONOutput),
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

func TestInfo_NoLibraryFlag_SummarizesBoth(t *testing.T) {
	withTestConfig(t, "42", "6506098")

	out, err := runInfo(t, "--json", "info")
	if err != nil {
		t.Fatalf("info: %v\n%s", err, string(out))
	}

	// JSON output is the MultiStatsResult shape.
	// Trim any leading non-JSON bytes (progress output) the same way
	// cmdutil does before returning the payload.
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON object in output: %q", string(out))
	}
	var result zot.MultiStatsResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse MultiStatsResult: %v\nraw: %s", err, string(out[jsonStart:]))
	}
	if len(result.PerLibrary) != 2 {
		t.Errorf("per_library len = %d, want 2 (personal + shared)", len(result.PerLibrary))
	}
	// Labels should reflect each scope.
	labels := []string{result.PerLibrary[0].Library, result.PerLibrary[1].Library}
	foundPersonal, foundShared := false, false
	for _, lab := range labels {
		if lab == "personal" {
			foundPersonal = true
		}
		if lab == "shared (sciminds)" {
			foundShared = true
		}
	}
	if !foundPersonal || !foundShared {
		t.Errorf("labels = %v, want personal + shared(sciminds)", labels)
	}
}

func TestInfo_WithLibraryFlag_NarrowsToScope(t *testing.T) {
	withTestConfig(t, "42", "6506098")

	out, err := runInfo(t, "--json", "info", "--library", "personal")
	if err != nil {
		t.Fatalf("info --library personal: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON in output: %q", string(out))
	}
	var result zot.StatsResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse StatsResult: %v\nraw: %s", err, string(out[jsonStart:]))
	}
	if result.Library != "personal" {
		t.Errorf("Library = %q, want personal", result.Library)
	}
}

func TestInfo_SharedNotConfigured_PersonalOnly(t *testing.T) {
	// Account without a configured shared group: info should still render
	// personal, not error out.
	withTestConfig(t, "42", "")

	out, err := runInfo(t, "--json", "info")
	if err != nil {
		t.Fatalf("info: %v\n%s", err, string(out))
	}
	jsonStart := bytes.IndexByte(out, '{')
	if jsonStart < 0 {
		t.Fatalf("no JSON in output: %q", string(out))
	}
	var result zot.MultiStatsResult
	if err := json.Unmarshal(out[jsonStart:], &result); err != nil {
		t.Fatalf("parse MultiStatsResult: %v", err)
	}
	if len(result.PerLibrary) != 1 {
		t.Errorf("per_library len = %d, want 1 (personal only)", len(result.PerLibrary))
	}
}
