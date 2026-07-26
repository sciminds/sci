package cli

// mistakes_test.go — the zot mistake corpus (Phase B of the agent-surface
// hardening pass). Every common agent mistake must yield a CodedError whose
// Fix resubmits cleanly or whose Try says what to do next — no fix-less
// dead ends. The corpus convention test at the bottom is the audit gate.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/local"
)

// --- missing --library ---

func TestInsertLibraryFix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "flags and args preserved",
			argv: []string{"/usr/local/bin/sci", "zot", "search", "--json", "foo"},
			want: "sci zot --library personal search --json foo",
		},
		{
			name: "arg with spaces quoted",
			argv: []string{"sci", "zot", "search", "social cognition"},
			want: "sci zot --library personal search 'social cognition'",
		},
		{
			name: "no zot token yields empty fix",
			argv: []string{"sci", "db", "tables"},
			want: "",
		},
		{
			name: "empty argv yields empty fix",
			argv: nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := insertLibraryFix(tc.argv); got != tc.want {
				t.Errorf("insertLibraryFix(%v) = %q, want %q", tc.argv, got, tc.want)
			}
		})
	}
}

func TestErrLibraryRequired_CodedWithFixAndTry(t *testing.T) {
	t.Parallel()
	err := errLibraryRequiredArgs("--json mode is non-interactive so we won't prompt",
		[]string{"sci", "zot", "search", "foo", "--json"})
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatal("errLibraryRequired should return a *CodedError")
	}
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage", coded.Code)
	}
	if coded.Fix != "sci zot --library personal search foo --json" {
		t.Errorf("Fix = %q", coded.Fix)
	}
	if !strings.Contains(coded.Try, "shared") {
		t.Errorf("Try should mention the shared library, got %q", coded.Try)
	}
}

// --- search flag conflicts ---

func TestSearch_RemoteExport_CodeConflict(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--json", "--library", "personal", "search", "--remote", "--export", "foo")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeConflict {
		t.Errorf("Code = %q, want conflict", coded.Code)
	}
	if coded.Try == "" {
		t.Error("conflict must carry a Try nudge")
	}
}

func TestSearch_FulltextRemote_CodeConflict(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--json", "--library", "personal", "search", "--fulltext", "--remote", "foo")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeConflict {
		t.Errorf("Code = %q, want conflict", coded.Code)
	}
}

// --- item read: cite-key absorption + coded not-found ---

// seedCitekey adds a citationKey field value to an existing fixture item.
// High IDs avoid colliding with seedOrientDB's rows.
func seedCitekey(t *testing.T, dataDir string, itemID int, citekey string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dataDir+"/zotero.sqlite")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, stmt := range []string{
		`INSERT OR IGNORE INTO fields (fieldID, fieldName) VALUES (100, 'citationKey')`,
		`INSERT INTO itemDataValues (valueID, value) VALUES (100, '` + citekey + `')`,
		`INSERT INTO itemData (itemID, fieldID, valueID) VALUES (` + strconv.Itoa(itemID) + `, 100, 100)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed citekey: %v (%s)", err, stmt)
		}
	}
}

func TestItemRead_CiteKeyArg_Absorbed(t *testing.T) {
	dataDir := withOrientConfig(t)
	seedCitekey(t, dataDir, 1, "smith2024-paper-one")

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "smith2024-paper-one")
	if err != nil {
		t.Fatalf("item read by cite key should be absorbed, got: %v", err)
	}
	var result local.Item
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Key != "KEY1" {
		t.Errorf("cite key resolved to %q, want KEY1", result.Key)
	}
}

func TestItemRead_ShortLegacyKey_StillReadsDirectly(t *testing.T) {
	withOrientConfig(t)

	// KEY3 is 4 chars — not a Zotero key shape, not a cite key. The direct
	// read must keep working (regression guard for the absorption path).
	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "KEY3")
	if err != nil {
		t.Fatalf("item read KEY3: %v", err)
	}
	var result local.Item
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Key != "KEY3" {
		t.Errorf("got %q, want KEY3", result.Key)
	}
}

func TestItemRead_NotFound_CodedWithRemoteFix(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "ZZZZ9999")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeNotFound {
		t.Errorf("Code = %q, want not-found", coded.Code)
	}
	if !strings.Contains(coded.Fix, "--remote") || !strings.HasPrefix(coded.Fix, "sci ") {
		t.Errorf("Fix should be a complete --remote resubmit, got %q", coded.Fix)
	}
	if !strings.Contains(coded.Fix, "ZZZZ9999") {
		t.Errorf("Fix should carry the key, got %q", coded.Fix)
	}
}

// --- collection resolution: did-you-mean + coded ambiguity ---

func TestResolveCollectionKey_NotFound_SuggestsNearest(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "AAAA1111", Name: "Active reading"},
		{Key: "BBBB2222", Name: "Archive"},
	}}
	_, _, err := resolveCollectionKey(db, "active readng")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeNotFound {
		t.Errorf("Code = %q, want not-found", coded.Code)
	}
	if !strings.Contains(coded.Try, "Active reading") {
		t.Errorf("Try should suggest the nearest collection name, got %q", coded.Try)
	}
}

func TestResolveCollectionKey_NoNearMatch_StillGuides(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "AAAA1111", Name: "Archive"},
	}}
	_, _, err := resolveCollectionKey(db, "zzzzzzzzzz")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Try == "" {
		t.Error("even with no near match, Try must point at collection list")
	}
}

func TestResolveCollectionKey_Ambiguous_Coded(t *testing.T) {
	t.Parallel()
	db := &fakeCollectionReader{collections: []local.Collection{
		{Key: "AAAA1111", Name: "notes"},
		{Key: "BBBB2222", Name: "notes"},
	}}
	_, _, err := resolveCollectionKey(db, "notes")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeAmbiguous {
		t.Errorf("Code = %q, want ambiguous", coded.Code)
	}
}

// --- not configured ---

func TestRequireConfigCoded_NotConfigured(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	_, err := requireConfigCoded()
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeNotConfigured {
		t.Errorf("Code = %q, want not-configured", coded.Code)
	}
	if coded.Fix != "sci zot setup" {
		t.Errorf("Fix = %q, want 'sci zot setup'", coded.Fix)
	}
}

// --- corpus convention: no fix-less dead ends ---

func TestCorpus_EveryMistakeHasFixOrTry(t *testing.T) {
	t.Parallel()
	corpus := []*cmdutil.CodedError{
		mustCoded(t, errLibraryRequiredArgs("reason", []string{"sci", "zot", "search", "x"})),
	}
	for _, coded := range corpus {
		if coded.Fix == "" && coded.Try == "" {
			t.Errorf("corpus entry %q (%s) is a fix-less dead end", coded.Message, coded.Code)
		}
		if coded.Fix != "" {
			if !strings.HasPrefix(coded.Fix, "sci ") {
				t.Errorf("Fix %q must be a complete sci command", coded.Fix)
			}
			if strings.ContainsAny(coded.Fix, "\n") {
				t.Errorf("Fix %q must be a single line", coded.Fix)
			}
		}
	}
}

func mustCoded(t *testing.T, err error) *cmdutil.CodedError {
	t.Helper()
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want CodedError, got %T: %v", err, err)
	}
	return coded
}
