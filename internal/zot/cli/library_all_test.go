package cli

// Tests for `--library all` — the merged read pool zen asked for (G1).
// search and bib accept it: one call spans personal + shared with a
// single interleaved ranking and per-row `library` provenance. Every
// other command rejects it with a rewrite hint, and the conflicts
// (--content, --remote, --notes) error instead of silently answering a
// narrower question than the one asked.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
)

// withSharedOrientConfig is withOrientConfig plus the shared group the
// seeded DB now carries (groupID 777), so `--library all` can resolve.
func withSharedOrientConfig(t *testing.T) string {
	t.Helper()
	dataDir := withOrientConfig(t)
	cfg, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.SharedGroupID = "777"
	cfg.SharedGroupName = "sciminds-test"
	if err := zot.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return dataDir
}

// runZotAll is the runItemRead harness with the search flag state reset
// between tests (package-level Destinations leak otherwise).
func runZotAll(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	t.Cleanup(func() {
		searchContent = false
		searchRemote = false
		searchNotes = false
		searchFull = false
		searchExport = false
		searchLimit = 50
	})
	return runItemRead(t, args...)
}

func TestSearch_LibraryAll_MergesWithProvenance(t *testing.T) {
	withSharedOrientConfig(t)

	out, err := runZotAll(t, "--json", "--library", "all", "search", "paper")
	if err != nil {
		t.Fatalf("search --library all: %v\n%s", err, string(out))
	}
	var result struct {
		LibraryID int64        `json:"library_id"`
		Items     []local.Item `json:"items"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	byKey := map[string]local.Item{}
	for _, it := range result.Items {
		byKey[it.Key] = it
	}
	if _, ok := byKey["KEY1"]; !ok {
		t.Fatalf("merged search missing personal item KEY1: %s", string(out))
	}
	if _, ok := byKey["GRPKEY01"]; !ok {
		t.Fatalf("merged search missing shared item GRPKEY01: %s", string(out))
	}
	if byKey["KEY1"].Library != "personal" || byKey["GRPKEY01"].Library != "shared" {
		t.Errorf("per-row provenance wrong: KEY1=%q GRPKEY01=%q",
			byKey["KEY1"].Library, byKey["GRPKEY01"].Library)
	}
	// No single library owns a merged pool; per-row `library` rules.
	if result.LibraryID != 0 {
		t.Errorf("top-level library_id = %d under all, want 0", result.LibraryID)
	}
}

func TestSearch_LibraryAll_ContentConflicts(t *testing.T) {
	withSharedOrientConfig(t)

	_, err := runZotAll(t, "--library", "all", "search", "paper", "--content")
	if err == nil {
		t.Fatal("--content with --library all must conflict — the content index is per-library and a silent partial answer is worse than an error")
	}
	if !strings.Contains(err.Error(), "per-library") {
		t.Errorf("err should explain the per-library limitation: %v", err)
	}
}

func TestSearch_LibraryAll_RemoteConflicts(t *testing.T) {
	withSharedOrientConfig(t)

	_, err := runZotAll(t, "--library", "all", "search", "paper", "--remote")
	if err == nil {
		t.Fatal("--remote with --library all must conflict — the Web API has no merged endpoint")
	}
}

func TestSearch_LibraryAll_NotesFilterConflicts(t *testing.T) {
	withSharedOrientConfig(t)

	_, err := runZotAll(t, "--library", "all", "search", "paper", "--notes")
	if err == nil {
		t.Fatal("--notes with --library all must conflict until its query is converted — it would silently filter against the personal library only")
	}
}

func TestItemRead_LibraryAll_RejectedWithGuidance(t *testing.T) {
	withSharedOrientConfig(t)

	_, err := runZotAll(t, "--library", "all", "item", "read", "KEY1")
	if err == nil {
		t.Fatal("item read must reject --library all until its full read path is converted")
	}
	msg := err.Error()
	if !strings.Contains(msg, "search and bib") || !strings.Contains(msg, "single library") {
		t.Errorf("rejection should name which commands accept all and why this one can't: %v", err)
	}
}

func TestBib_LibraryAll_ResolvesAcrossLibraries(t *testing.T) {
	withSharedOrientConfig(t)

	dir := t.TempDir()
	md := filepath.Join(dir, "ms.md")
	// One DOI lives in the personal library (KEY1), one in the shared
	// group (GRPKEY01) — the merged pool must resolve both in one pass.
	body := "Cites https://doi.org/10.1038/nature12373 and https://doi.org/10.1000/shared1.\n"
	if err := os.WriteFile(md, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runZotAll(t, "--json", "--library", "all", "bib", md)
	if err != nil {
		t.Fatalf("bib --library all: %v\n%s", err, string(out))
	}
	var result struct {
		References int   `json:"references"`
		Resolved   int   `json:"resolved"`
		Unresolved []any `json:"unresolved"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	if len(result.Unresolved) != 0 {
		t.Errorf("both DOIs are in the merged pool; unresolved = %v\n%s", result.Unresolved, string(out))
	}
	if result.Resolved != 2 {
		t.Errorf("resolved = %d, want both DOIs\n%s", result.Resolved, string(out))
	}
}
