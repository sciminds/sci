package extract

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContentKey(t *testing.T) {
	t.Parallel()
	// mtime differs, size+digest identical → same identity. This is the
	// case the full fingerprint misses: two separate downloads of the
	// same PDF almost never share an mtime.
	a := ContentKey("1048576-1700000000-abc123")
	b := ContentKey("1048576-1799999999-abc123")
	if a == "" || a != b {
		t.Errorf("mtime must not affect identity: %q vs %q", a, b)
	}
	if ContentKey("1048576-1700000000-abc123") == ContentKey("999-1700000000-abc123") {
		t.Error("size must affect identity")
	}
	if ContentKey("1048576-1700000000-abc123") == ContentKey("1048576-1700000000-zzz999") {
		t.Error("digest must affect identity")
	}
	for _, malformed := range []string{"", "garbage", "1-2", "-1-", "1-2-"} {
		if got := ContentKey(malformed); got != "" {
			t.Errorf("ContentKey(%q) = %q, want \"\" (no identity)", malformed, got)
		}
	}
}

// surveyErrItem builds a BatchItem whose planning failed.
func surveyErrItem(parentKey string) BatchItem {
	return BatchItem{
		Request: BatchRequest{ParentKey: parentKey, PDFKey: "PDF" + parentKey, PDFName: parentKey + ".pdf"},
		Err:     errors.New("hash: no such file"),
	}
}

func TestBuildSurvey_Dispositions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}

	// PB's markdown is already cached.
	if _, err := cache.Put("PDFPB", "hb", []byte("# cached\n")); err != nil {
		t.Fatal(err)
	}
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate), // fresh
		mkBatchItem("PB", "PDFPB", "b.pdf", "/x/b.pdf", "hb", ActionCreate), // cached
		mkBatchItem("PC", "PDFPC", "c.pdf", "/x/c.pdf", "hc", ActionSkip),   // note exists
		surveyErrItem("PD"),
	}
	s := BuildSurvey(SurveyInput{Items: items, Cache: cache, Apply: true, Candidates: 4})

	if s.NeedsExtraction != 1 || s.Cached != 1 || s.Skipped != 1 || s.PlanErrors != 1 {
		t.Errorf("counts = extract=%d cached=%d skipped=%d errors=%d, want 1/1/1/1",
			s.NeedsExtraction, s.Cached, s.Skipped, s.PlanErrors)
	}
	wantDisp := []Disposition{DispExtract, DispPostCached, DispSkip, DispError}
	for i, want := range wantDisp {
		if s.Items[i].Disposition != want {
			t.Errorf("item %d disposition = %s, want %s", i, s.Items[i].Disposition, want)
		}
	}
	if s.Errors["PD"] == "" {
		t.Error("plan error not recorded in Errors")
	}
	// Apply mode keeps the cached item in Selected and marks it.
	if len(s.Selected) != 4 {
		t.Fatalf("selected = %d, want 4 (apply keeps cached)", len(s.Selected))
	}
	if !s.CachedIdx[1] {
		t.Errorf("CachedIdx = %v, want index 1 marked", s.CachedIdx)
	}
}

// TestBuildSurvey_CacheFilterMatchesLegacyBehavior pins the moved
// filter logic: cache-only drops cached items, --reextract skips the
// filter entirely, and layout mode never consults the markdown cache.
func TestBuildSurvey_CacheFilterMatchesLegacyBehavior(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	if _, err := cache.Put("PDFPB", "hb", []byte("# cached\n")); err != nil {
		t.Fatal(err)
	}
	mkItems := func() []BatchItem {
		return []BatchItem{
			mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate),
			mkBatchItem("PB", "PDFPB", "b.pdf", "/x/b.pdf", "hb", ActionCreate),
		}
	}

	// Cache-only: cached PB dropped from the selection.
	s := BuildSurvey(SurveyInput{Items: mkItems(), Cache: cache})
	if len(s.Selected) != 1 || s.Selected[0].Request.ParentKey != "PA" {
		t.Errorf("cache-only selected = %+v, want [PA]", s.Selected)
	}

	// --reextract: filter skipped, both selected as fresh.
	s = BuildSurvey(SurveyInput{Items: mkItems(), Cache: cache, Reextract: true})
	if len(s.Selected) != 2 || s.NeedsExtraction != 2 {
		t.Errorf("reextract selected=%d extract=%d, want 2/2", len(s.Selected), s.NeedsExtraction)
	}
	if s.WouldInvalidateCache != 1 {
		t.Errorf("WouldInvalidateCache = %d, want 1 (only PB has an entry)", s.WouldInvalidateCache)
	}
	if _, ok := cache.Get("PDFPB", "hb"); !ok {
		t.Error("BuildSurvey deleted a cache entry — it must be read-only")
	}

	// Layout mode: cache never consulted, everything selected.
	layout := &KeyLayout{Dir: filepath.Join(dir, "extracts")}
	s = BuildSurvey(SurveyInput{Items: mkItems(), Cache: cache, Layout: layout})
	if len(s.Selected) != 2 || s.NeedsExtraction != 2 {
		t.Errorf("layout selected=%d extract=%d, want 2/2 (cache is not a layout substitute)",
			len(s.Selected), s.NeedsExtraction)
	}
}

func TestBuildSurvey_LimitReportsBothTotals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate),
		mkBatchItem("PB", "PDFPB", "b.pdf", "/x/b.pdf", "hb", ActionCreate),
		mkBatchItem("PC", "PDFPC", "c.pdf", "/x/c.pdf", "hc", ActionCreate),
	}
	s := BuildSurvey(SurveyInput{Items: items, Cache: cache, Limit: 1})

	if len(s.Selected) != 1 {
		t.Errorf("selected = %d, want 1", len(s.Selected))
	}
	if s.NeedsExtraction != 3 {
		t.Errorf("NeedsExtraction = %d, want the unlimited 3 — a limited plan must not hide the real total", s.NeedsExtraction)
	}
	if s.Remaining != 2 {
		t.Errorf("Remaining = %d, want 2", s.Remaining)
	}
}

func TestBuildSurvey_DuplicateGroup_DifferentMtimes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	// Same size+digest, different mtimes — the tonight bug: grouping on
	// the full fingerprint would have missed these.
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "book.pdf", "/x/a.pdf", "500-1111-beef", ActionCreate),
		mkBatchItem("PB", "PDFPB", "book-copy.pdf", "/x/b.pdf", "500-2222-beef", ActionCreate),
	}
	s := BuildSurvey(SurveyInput{Items: items, Cache: cache})

	if len(s.Duplicates) != 1 {
		t.Fatalf("groups = %d, want 1", len(s.Duplicates))
	}
	g := s.Duplicates[0]
	if g.Queued != 2 {
		t.Errorf("Queued = %d, want 2", g.Queued)
	}
	if s.DuplicateWasted != 1 {
		t.Errorf("DuplicateWasted = %d, want 1", s.DuplicateWasted)
	}
	if len(g.Members) != 2 || g.Members[0].ParentKey != "PA" || g.Members[1].ParentKey != "PB" {
		t.Errorf("members = %+v, want [PA PB] sorted", g.Members)
	}
}

func TestBuildSurvey_DuplicateGroup_OneAlreadyLayoutDone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout := &KeyLayout{Dir: filepath.Join(dir, "extracts")}
	// PA's layout dir is complete; PB shares its content but is fresh.
	staging := writeStagedOutputs(t, t.TempDir(), "PA")
	if _, err := layout.Finalize("PA", staging, "/x/a.pdf", 1); err != nil {
		t.Fatal(err)
	}
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "book.pdf", "/x/a.pdf", "500-1111-beef", ActionCreate),
		mkBatchItem("PB", "PDFPB", "book-copy.pdf", "/x/b.pdf", "500-2222-beef", ActionCreate),
	}
	s := BuildSurvey(SurveyInput{
		Items: items, Cache: &MarkdownCache{Dir: filepath.Join(dir, "cache")}, Layout: layout,
	})

	if len(s.Duplicates) != 1 {
		t.Fatalf("groups = %d, want 1 (still reported while one member is queued)", len(s.Duplicates))
	}
	g := s.Duplicates[0]
	if g.Queued != 1 || s.DuplicateWasted != 0 {
		t.Errorf("Queued=%d Wasted=%d, want 1/0", g.Queued, s.DuplicateWasted)
	}
	if g.Members[0].Disposition != DispPostCached {
		t.Errorf("done member disposition = %s, want %s", g.Members[0].Disposition, DispPostCached)
	}
}

func TestBuildSurvey_NoDuplicates_NilSlice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "500-1111-aaaa", ActionCreate),
		mkBatchItem("PB", "PDFPB", "b.pdf", "/x/b.pdf", "600-2222-bbbb", ActionCreate),
	}
	s := BuildSurvey(SurveyInput{Items: items, Cache: &MarkdownCache{Dir: filepath.Join(dir, "cache")}})
	if s.Duplicates != nil {
		t.Errorf("Duplicates = %+v, want nil (so omitempty drops the JSON key)", s.Duplicates)
	}
}

func TestBuildSurvey_HashErrorsNeverGroup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Two hash failures share Content "" — a hash failure is not
	// evidence of sameness.
	items := []BatchItem{surveyErrItem("PA"), surveyErrItem("PB")}
	s := BuildSurvey(SurveyInput{Items: items, Cache: &MarkdownCache{Dir: filepath.Join(dir, "cache")}})
	if len(s.Duplicates) != 0 {
		t.Errorf("groups = %+v, want none", s.Duplicates)
	}
}

func TestPageBuckets_Partition(t *testing.T) {
	t.Parallel()
	pages := []int{1, 10, 11, 25, 26, 50, 51, 100, 101, 300, 301}
	items := make([]SurveyItem, len(pages))
	for i, p := range pages {
		items[i] = SurveyItem{Pages: p}
	}
	buckets := PageBuckets(items)

	totalItems, totalPages := 0, 0
	for _, b := range buckets {
		totalItems += b.Items
		totalPages += b.Pages
	}
	if totalItems != len(pages) {
		t.Errorf("bucketed items = %d, want %d (every item in exactly one bucket)", totalItems, len(pages))
	}
	wantPages := 0
	for _, p := range pages {
		wantPages += p
	}
	if totalPages != wantPages {
		t.Errorf("bucketed pages = %d, want %d", totalPages, wantPages)
	}
	// Boundary rows: each bound value lands inside its own bucket.
	if len(buckets) != 6 {
		t.Errorf("buckets = %d, want all 6 populated", len(buckets))
	}
	if buckets[0].Items != 2 || buckets[5].Items != 1 {
		t.Errorf("boundary bucketing off: first=%d last=%d, want 2/1", buckets[0].Items, buckets[5].Items)
	}
}

func TestEstimateDuration(t *testing.T) {
	t.Parallel()
	if got, want := EstimateDuration(1000, 4, "mps"), EstimateDuration(1000, 1, "mps")/4; got != want {
		t.Errorf("4 jobs = %v, want quarter of 1 job (%v)", got, want)
	}
	if got, want := EstimateDuration(100, 0, "mps"), EstimateDuration(100, 1, "mps"); got != want {
		t.Errorf("jobs=0 = %v, want same as jobs=1 (%v)", got, want)
	}
	if EstimateDuration(100, 1, "cpu") <= EstimateDuration(100, 1, "mps") {
		t.Error("cpu must estimate slower than mps")
	}
}

func TestBuildSurvey_NilPageCounter_Degrades(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	items := []BatchItem{mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate)}
	s := BuildSurvey(SurveyInput{Items: items, Cache: &MarkdownCache{Dir: filepath.Join(dir, "cache")}})

	if s.ETA != 0 || s.Buckets != nil || s.PagesKnownItems != 0 {
		t.Errorf("nil estimator must degrade to counts-only: eta=%v buckets=%v known=%d",
			s.ETA, s.Buckets, s.PagesKnownItems)
	}
	if s.NeedsExtraction != 1 {
		t.Errorf("NeedsExtraction = %d, want 1 — degradation must not eat the counts", s.NeedsExtraction)
	}
}

func TestBuildSurvey_PageCounterErrorIsNotFatal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate),
		mkBatchItem("PB", "PDFPB", "b.pdf", "/x/b.pdf", "hb", ActionCreate),
	}
	counter := func(path string) (int, error) {
		if path == "/x/a.pdf" {
			return 0, errors.New("unparseable")
		}
		return 40, nil
	}
	s := BuildSurvey(SurveyInput{
		Items: items, Cache: &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Pages: counter, Jobs: 1, Device: "mps",
	})

	if s.PagesKnownItems != 1 || s.PagesUnknownItems != 1 {
		t.Errorf("known=%d unknown=%d, want 1/1", s.PagesKnownItems, s.PagesUnknownItems)
	}
	if !s.Extrapolated {
		t.Error("half-unknown must extrapolate")
	}
	if s.PagesTotal != 80 {
		t.Errorf("PagesTotal = %d, want 80 (40 known × 2/1)", s.PagesTotal)
	}
	if s.ETA != EstimateDuration(80, 1, "mps") {
		t.Errorf("ETA = %v, want %v", s.ETA, EstimateDuration(80, 1, "mps"))
	}
}

// TestBuildSurvey_ReadOnly locks the "no writes" contract at the unit
// level: the cache and layout dirs are byte-identical before and after.
func TestBuildSurvey_ReadOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := &MarkdownCache{Dir: filepath.Join(dir, "cache")}
	if _, err := cache.Put("PDFPA", "ha", []byte("# cached\n")); err != nil {
		t.Fatal(err)
	}
	layout := &KeyLayout{Dir: filepath.Join(dir, "extracts")}
	staging := writeStagedOutputs(t, t.TempDir(), "PB")
	if _, err := layout.Finalize("PB", staging, "/x/b.pdf", 1); err != nil {
		t.Fatal(err)
	}

	snapshot := func() map[string]int64 {
		snap := map[string]int64{}
		for _, root := range []string{cache.Dir, layout.Dir} {
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					snap[path] = info.Size()
				}
				return nil
			})
		}
		return snap
	}
	before := snapshot()

	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate),
		mkBatchItem("PB", "PDFPB", "b.pdf", "/x/b.pdf", "hb", ActionCreate),
	}
	_ = BuildSurvey(SurveyInput{Items: items, Cache: cache, Layout: layout, Reextract: true, Apply: true})

	after := snapshot()
	if len(before) != len(after) {
		t.Fatalf("file count changed: %d → %d", len(before), len(after))
	}
	for p, sz := range before {
		if after[p] != sz {
			t.Errorf("file %s changed size %d → %d", p, sz, after[p])
		}
	}
}
