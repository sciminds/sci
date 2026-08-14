package cli

// Tests for `--library all` — the merged read pool over every configured library.
// search and bib accept it: one call spans personal + shared with a
// single interleaved ranking and per-row `library` provenance. Every
// other command rejects it with a rewrite hint, and the --notes conflict
// errors instead of silently answering a narrower question than the one
// asked.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/zot"
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

// runZotAll runs argv through the same harness runItemRead builds. Each
// invocation parses its own flags off the command tree, so nothing leaks
// between tests and the alias exists only to name the intent.
func runZotAll(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
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

// TestSearch_LibraryAll_EmptyHintIsFollowable: a zero-hit hint is read the
// same way a Fix is — as the next thing to run. `all` is already the widest
// pool there is, so a hint that suggests widening would hand back a command
// that cannot help.
func TestSearch_LibraryAll_EmptyHintIsFollowable(t *testing.T) {
	withSharedOrientConfig(t)

	out, err := runZotAll(t, "--json", "--library", "all", "search", "zzzznosuchterm")
	if err != nil {
		t.Fatalf("search --library all: %v\n%s", err, string(out))
	}
	var result struct {
		Count int    `json:"count"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	if result.Count != 0 {
		t.Fatalf("expected an empty result to hint on, got %d hits", result.Count)
	}
	// An agent follows a hint the way it follows a Fix, so the hint has to
	// be true under the scope that produced it. `all` is already the widest
	// pool there is, so the honest hint states what WAS searched instead of
	// naming a follow-up that would miss for the same reason.
	if strings.Contains(result.Hint, "--library personal") {
		t.Errorf("hint must not send a merged search back to one library — that narrows, it does not widen: %q", result.Hint)
	}
	if strings.Contains(result.Hint, "--library all") {
		t.Errorf("hint must not suggest widening to the scope the search already used: %q", result.Hint)
	}
	if !strings.Contains(result.Hint, "metadata") {
		t.Errorf("hint should say what the search actually matched, got %q", result.Hint)
	}
}

// TestSearch_SingleLibrary_EmptyHintStillWidens pins the other half: with a
// single library resolved, widening to the merged pool is the honest next
// step, and it is a scope sci actually has.
func TestSearch_SingleLibrary_EmptyHintStillWidens(t *testing.T) {
	withSharedOrientConfig(t)

	out, err := runZotAll(t, "--json", "--library", "personal", "search", "zzzznosuchterm")
	if err != nil {
		t.Fatalf("search --library personal: %v\n%s", err, string(out))
	}
	var result struct {
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	if !strings.Contains(result.Hint, "--library all") {
		t.Errorf("single-library hint should widen to the merged pool, got %q", result.Hint)
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
	// The list must stay complete: `export --format ndjson` and `browse`
	// accept `all` too, and an error naming only two of the four teaches a
	// caller that the other two refuse it.
	for _, want := range []string{"search", "bib", "export --format ndjson", "browse", "single library"} {
		if !strings.Contains(msg, want) {
			t.Errorf("rejection should name %q — which commands accept all, and why this one can't: %v", want, err)
		}
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
