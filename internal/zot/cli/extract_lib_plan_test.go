package cli

// Tests for `zot extract-lib --plan` (the only read-only mode — a bare
// run still extracts), the duplicate-content warning, and the quiet-mode
// guard. The docling constructor is swapped via the newBatchExtractor
// seam so every test proves docling is never even constructed.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/extract"
)

// planFakeExtractor counts invocations; it must never be reached by
// --plan and never produces output.
type planFakeExtractor struct{ calls *atomic.Int32 }

func (f *planFakeExtractor) Extract(context.Context, extract.ExtractOptions) (*extract.ExtractResult, error) {
	f.calls.Add(1)
	return nil, errors.New("planFakeExtractor: must not run")
}

func (f *planFakeExtractor) ExtractBatch(context.Context, extract.ExtractOptions, []string, extract.ProgressFunc) (*extract.BatchExtractResult, error) {
	f.calls.Add(1)
	return nil, errors.New("planFakeExtractor: must not run")
}

// seedExtractLibFixture builds the extract-lib test world on top of the
// orient fixture: KEY1 already has a docling note (already-done), KEY2
// and KEY3 get PDF attachments holding IDENTICAL bytes with different
// mtimes (the duplicate case the full mtime-bearing fingerprint would
// miss). HOME is redirected so the markdown cache lands in a temp dir.
// Returns the dataDir and the swap-counter for the docling constructor.
func seedExtractLibFixture(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dataDir := withOrientConfig(t)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "zotero.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	stmts := []string{
		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified) VALUES
			(300, 1, 1, 'ATT1KEY0', 1, '2024-01-01 10:05:00', '2024-01-01 10:05:00', '2024-01-01 10:05:00'),
			(301, 1, 1, 'ATT2KEY0', 1, '2024-02-01 10:05:00', '2024-02-01 10:05:00', '2024-02-01 10:05:00'),
			(302, 1, 1, 'ATT3KEY0', 1, '2024-03-01 10:05:00', '2024-03-01 10:05:00', '2024-03-01 10:05:00')`,
		`INSERT INTO itemAttachments (itemID, parentItemID, linkMode, contentType, path) VALUES
			(300, 1, 1, 'application/pdf', 'storage:paper1.pdf'),
			(301, 2, 1, 'application/pdf', 'storage:paper2.pdf'),
			(302, 3, 1, 'application/pdf', 'storage:paper3.pdf')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}

	write := func(attKey, name, body string) string {
		p := filepath.Join(dataDir, "storage", attKey, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("ATT1KEY0", "paper1.pdf", "unique-content-one")
	write("ATT2KEY0", "paper2.pdf", "same-bytes-duplicate-scan")
	p3 := write("ATT3KEY0", "paper3.pdf", "same-bytes-duplicate-scan")
	// Different mtime, same bytes — separate downloads of the same PDF.
	if err := os.Chtimes(p3, time.Now().Add(-24*time.Hour), time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	var extractorCalls atomic.Int32
	newBatchExtractor = func() (extract.Extractor, error) {
		return &planFakeExtractor{calls: &extractorCalls}, nil
	}
	t.Cleanup(func() {
		newBatchExtractor = func() (extract.Extractor, error) { return extract.NewDoclingExtractor() }
		extractLibDevice, extractLibOut = "", ""
		extractLibNumThreads, extractLibJobs, extractLibLimit = 0, 0, 0
		extractLibYes, extractLibForce, extractLibReextract = false, false, false
		extractLibApply, extractLibHTML, extractLibPlan = false, false, false
	})
	return dataDir, &extractorCalls
}

// planEnvelope is the --json envelope for --plan output.
type planEnvelope struct {
	OK       bool                     `json:"ok"`
	Data     zot.ExtractLibPlanResult `json:"data"`
	Warnings []cmdutil.Warning        `json:"warnings"`
}

func runPlan(t *testing.T, args ...string) planEnvelope {
	t.Helper()
	out, err := runOrient(t, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	var env planEnvelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("parse envelope: %v\n%s", err, out)
	}
	return env
}

func TestExtractLib_PlanWithApply_Conflicts(t *testing.T) {
	_, calls := seedExtractLibFixture(t)

	_, err := runOrient(t, "--json", "--library", "personal", "extract-lib", "--plan", "--apply")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("err = %v, want CodedError", err)
	}
	if coded.Code != cmdutil.CodeConflict {
		t.Errorf("code = %s, want %s", coded.Code, cmdutil.CodeConflict)
	}
	if coded.Fix != "sci zot extract-lib --plan" {
		t.Errorf("fix = %q", coded.Fix)
	}
	if calls.Load() != 0 {
		t.Error("docling reached despite the conflict")
	}
}

func TestExtractLib_Plan_NeverInvokesDoclingAndWritesNothing(t *testing.T) {
	_, calls := seedExtractLibFixture(t)
	// Belt and braces: with an empty PATH, even an accidental
	// exec.LookPath("docling") would fail loudly.
	t.Setenv("PATH", "")

	env := runPlan(t, "--json", "--library", "personal", "extract-lib", "--plan")
	if !env.OK {
		t.Fatal("plan not ok")
	}
	if calls.Load() != 0 {
		t.Errorf("extractor invoked %d times under --plan", calls.Load())
	}

	// No cache writes: the markdown cache dir was never created.
	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		t.Errorf("cache dir has %d entries after --plan, want none", len(entries))
	}
}

func TestExtractLib_Plan_JSONShapeAndDuplicates(t *testing.T) {
	seedExtractLibFixture(t)

	env := runPlan(t, "--json", "--library", "personal", "extract-lib", "--plan")
	d := env.Data
	if d.Mode != "plan" || d.Apply {
		t.Errorf("mode/apply = %s/%v", d.Mode, d.Apply)
	}
	// KEY1 has the docling note → already done; KEY2 + KEY3 remain.
	if d.Candidates != 3 || d.AlreadyDone != 1 || d.NeedsExtraction != 2 {
		t.Errorf("candidates/done/extract = %d/%d/%d, want 3/1/2", d.Candidates, d.AlreadyDone, d.NeedsExtraction)
	}
	if d.LayoutDone != nil {
		t.Errorf("layout_done = %v, want nil in classic mode", *d.LayoutDone)
	}
	// Same bytes, different mtimes → one duplicate group, one wasted run.
	if d.DuplicateGroups != 1 || d.DuplicateWasted != 1 {
		t.Fatalf("dup groups/wasted = %d/%d, want 1/1", d.DuplicateGroups, d.DuplicateWasted)
	}
	members := d.Duplicates[0].Members
	if len(members) != 2 || members[0].ParentKey != "KEY2" || members[1].ParentKey != "KEY3" {
		t.Errorf("members = %+v, want KEY2+KEY3", members)
	}
	if len(env.Warnings) != 1 || env.Warnings[0].Code != cmdutil.CodeDuplicate {
		t.Errorf("warnings = %+v, want one %s", env.Warnings, cmdutil.CodeDuplicate)
	}
	// The estimator is wired but the stub PDFs are unparseable → they
	// count as honest unknowns and no ETA is fabricated.
	if d.Pages == nil || d.Pages.Known != 0 || d.Pages.Unknown != 2 {
		t.Errorf("pages = %+v, want 0 known / 2 unknown", d.Pages)
	}
	if d.ETA != nil {
		t.Errorf("eta = %+v, want null (no known pages)", d.ETA)
	}
}

func TestExtractLib_Plan_LimitReportsBothTotals(t *testing.T) {
	seedExtractLibFixture(t)

	env := runPlan(t, "--json", "--library", "personal", "extract-lib", "--plan", "--limit", "1")
	d := env.Data
	if d.Selected != 1 || d.NeedsExtraction != 2 || d.Remaining != 1 {
		t.Errorf("selected/extract/remaining = %d/%d/%d, want 1/2/1", d.Selected, d.NeedsExtraction, d.Remaining)
	}
}

func TestExtractLib_Plan_ReextractDoesNotDeleteCache(t *testing.T) {
	dataDir, _ := seedExtractLibFixture(t)

	// Seed a cache entry for KEY2's PDF the way a prior run would have.
	hash, err := extract.HashPDF(filepath.Join(dataDir, "storage", "ATT2KEY0", "paper2.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cache := &extract.MarkdownCache{Dir: cacheDir}
	if _, err := cache.Put("ATT2KEY0", hash, []byte("# cached\n")); err != nil {
		t.Fatal(err)
	}

	env := runPlan(t, "--json", "--library", "personal", "extract-lib", "--plan", "--reextract")
	if env.Data.WouldInvalidateCache != 1 {
		t.Errorf("would_invalidate_cache = %d, want 1", env.Data.WouldInvalidateCache)
	}
	if _, ok := cache.Get("ATT2KEY0", hash); !ok {
		t.Error("--plan --reextract deleted the cache entry — plan must be read-only")
	}
}

func TestExtractLib_QuietWithoutChoice_Refuses(t *testing.T) {
	_, calls := seedExtractLibFixture(t)

	_, err := runOrient(t, "--json", "--library", "personal", "extract-lib")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("err = %v, want CodedError — --json used to silently launch a full docling run", err)
	}
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %s, want %s", coded.Code, cmdutil.CodeUsage)
	}
	if !strings.Contains(coded.Fix, "--plan") {
		t.Errorf("fix = %q, want a --plan rewrite", coded.Fix)
	}
	if calls.Load() != 0 {
		t.Error("docling reached despite the refusal")
	}
}

func TestExtractLib_Reextract_CacheSurvivesDeclinedConfirm(t *testing.T) {
	dataDir, calls := seedExtractLibFixture(t)
	t.Setenv("SCI_ASSUME", "no")

	hash, err := extract.HashPDF(filepath.Join(dataDir, "storage", "ATT2KEY0", "paper2.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cache := &extract.MarkdownCache{Dir: cacheDir}
	if _, err := cache.Put("ATT2KEY0", hash, []byte("# cached\n")); err != nil {
		t.Fatal(err)
	}

	// Human-mode run, declined at the confirm prompt.
	if _, err := runOrient(t, "--library", "personal", "extract-lib", "--reextract"); err != nil {
		t.Fatalf("declined confirm must exit cleanly: %v", err)
	}
	if _, ok := cache.Get("ATT2KEY0", hash); !ok {
		t.Error("declining the confirm deleted the cache — the --reextract purge must run after consent")
	}
	if calls.Load() != 0 {
		t.Error("docling ran despite the declined confirm")
	}
}
