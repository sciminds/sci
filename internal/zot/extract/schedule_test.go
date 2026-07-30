package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestPlanChunks_Empty(t *testing.T) {
	t.Parallel()
	if got := planChunks(nil, 45, 5); len(got) != 0 {
		t.Errorf("planChunks(nil) = %v, want none", got)
	}
}

func TestPlanChunks_SortsDescendingAndIsolates(t *testing.T) {
	t.Parallel()
	// slot: 0=10pp 1=400pp 2=20pp 3=5pp — the book must come first,
	// alone; the papers pool descending.
	got := planChunks([]int{10, 400, 20, 5}, 45, 5)
	want := [][]int{{1}, {2, 0, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("chunks = %v, want %v", got, want)
	}
}

func TestPlanChunks_IsolatesEveryOversize(t *testing.T) {
	t.Parallel()
	got := planChunks([]int{400, 10, 10, 10, 10, 10, 90}, 60, 5)
	want := [][]int{{0}, {6}, {1, 2, 3, 4, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("chunks = %v, want %v", got, want)
	}
}

func TestPlanChunks_ThresholdIsInclusive(t *testing.T) {
	t.Parallel()
	got := planChunks([]int{45, 44}, 45, 5)
	want := [][]int{{0}, {1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cost == threshold must isolate: %v, want %v", got, want)
	}
}

func TestPlanChunks_ChunkSizing(t *testing.T) {
	t.Parallel()
	costs := make([]int, 13)
	for i := range costs {
		costs[i] = 10
	}
	got := planChunks(costs, 45, 5)
	sizes := make([]int, len(got))
	for i, c := range got {
		sizes[i] = len(c)
	}
	if !slices.Equal(sizes, []int{5, 5, 3}) {
		t.Errorf("chunk sizes = %v, want [5 5 3]", sizes)
	}
}

func TestPlanChunks_AllOversize(t *testing.T) {
	t.Parallel()
	got := planChunks([]int{400, 300, 200, 100}, 45, 5)
	if len(got) != 4 {
		t.Fatalf("chunks = %v, want 4 singletons", got)
	}
	for _, c := range got {
		if len(c) != 1 {
			t.Errorf("oversize chunk %v not isolated", c)
		}
	}
}

// TestPlanChunks_Partition is the load-bearing one: chunks must
// partition the slot space exactly — no duplicates, no omissions —
// because workers write outcomes[slot] without a lock on that basis.
func TestPlanChunks_Partition(t *testing.T) {
	t.Parallel()
	costs := make([]int, 100)
	for i := range costs {
		costs[i] = (i*37)%97 + 1 // deterministic pseudo-random spread incl. oversize
	}
	chunks := planChunks(costs, 45, 5)
	var all []int
	for _, c := range chunks {
		all = append(all, c...)
	}
	slices.Sort(all)
	for i, slot := range all {
		if slot != i {
			t.Fatalf("partition broken at position %d: slot %d (duplicate or omission)", i, slot)
		}
	}
	if len(all) != 100 {
		t.Fatalf("partition covers %d of 100 slots", len(all))
	}
}

func TestPlanChunks_RespectsMaxDoclingBatch(t *testing.T) {
	t.Parallel()
	costs := make([]int, 120)
	for i := range costs {
		costs[i] = 1
	}
	for _, c := range planChunks(costs, 45, 500) {
		if len(c) > maxDoclingBatch {
			t.Errorf("chunk of %d exceeds maxDoclingBatch %d", len(c), maxDoclingBatch)
		}
	}
}

func TestPlanChunks_ClampsBadTarget(t *testing.T) {
	t.Parallel()
	for _, target := range []int{0, -3} {
		got := planChunks([]int{1, 1, 1}, 45, target) // must not panic (lo.Chunk panics on <=0)
		if len(got) != 3 {
			t.Errorf("target %d: chunks = %v, want 3 singletons at clamped size 1", target, got)
		}
	}
}

func TestPlanChunks_Deterministic(t *testing.T) {
	t.Parallel()
	costs := []int{10, 400, 20, 5, 90, 10, 10}
	if !reflect.DeepEqual(planChunks(costs, 45, 5), planChunks(costs, 45, 5)) {
		t.Error("planChunks is not deterministic")
	}
}

func TestEffectiveJobs(t *testing.T) {
	t.Parallel()
	cases := []struct{ jobs, chunks, want int }{
		{0, 3, 1},
		{4, 2, 2},
		{2, 9, 2},
		{-1, 5, 1},
		{4, 0, 0},
	}
	for _, tc := range cases {
		if got := effectiveJobs(tc.jobs, tc.chunks); got != tc.want {
			t.Errorf("effectiveJobs(%d, %d) = %d, want %d", tc.jobs, tc.chunks, got, tc.want)
		}
	}
}

// ── page-count estimator ──

func TestFallbackPages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		size int64
		want int
	}{
		{0, 1},
		{1, 1},
		{100 << 10, 1},
		{1 << 20, 10},
		{6_600_000, 64},
		{50 << 20, 512},
	}
	for _, tc := range cases {
		if got := fallbackPages(tc.size); got != tc.want {
			t.Errorf("fallbackPages(%d) = %d, want %d", tc.size, got, tc.want)
		}
	}
}

// TestFallbackPages_IsolatesLargeScans is the regression line for the
// observed 6.6MB/178-page/53-minute monograph: if the parser ever fails
// on it, the size heuristic must still isolate it.
func TestFallbackPages_IsolatesLargeScans(t *testing.T) {
	t.Parallel()
	if got := fallbackPages(6_600_000); got < isolateChunkPages {
		t.Errorf("fallbackPages(6.6MB) = %d, below the %d isolation threshold", got, isolateChunkPages)
	}
}

func TestEstimatePages_MissingFile(t *testing.T) {
	t.Parallel()
	if got := EstimatePages(filepath.Join(t.TempDir(), "gone.pdf")); got != 1 {
		t.Errorf("missing file = %d, want 1 (docling fails it in seconds — don't isolate it)", got)
	}
}

func TestEstimatePages_NotAPDF(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "stub.pdf")
	if err := os.WriteFile(p, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := EstimatePages(p); got != 1 {
		t.Errorf("3-byte stub = %d, want fallbackPages(3) = 1 — every batch-test stub must keep working", got)
	}
}

// writeMinimalPDF assembles a syntactically valid n-page PDF with a
// correct xref table so pdfcpu parses it without repair heuristics.
func writeMinimalPDF(t *testing.T, path string, pages int) {
	t.Helper()
	var buf []byte
	var offsets []int
	add := func(s string) {
		offsets = append(offsets, len(buf))
		buf = append(buf, s...)
	}
	buf = append(buf, "%PDF-1.4\n"...)
	kids := ""
	for i := range pages {
		kids += fmt.Sprintf("%d 0 R ", i+3)
	}
	add("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	add(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", kids, pages))
	for i := range pages {
		add(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n", i+3))
	}
	xrefAt := len(buf)
	xref := fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		xref += fmt.Sprintf("%010d 00000 n \n", off)
	}
	xref += fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefAt)
	buf = append(buf, xref...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPageCount_RealPDF(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "three.pdf")
	writeMinimalPDF(t, p, 3)
	n, err := PageCount(p)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 3 {
		t.Errorf("pages = %d, want 3", n)
	}
	if got := EstimatePages(p); got != 3 {
		t.Errorf("EstimatePages = %d, want 3", got)
	}
}

// TestEstimatePages_NoConfigDirSideEffect guards the pdfcpu landmine:
// its default configuration materialises a pdfcpu/ config dir on first
// use, and sci must never write into a user's config tree as a side
// effect of counting pages.
func TestEstimatePages_NoConfigDirSideEffect(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p := filepath.Join(t.TempDir(), "one.pdf")
	writeMinimalPDF(t, p, 1)
	if got := EstimatePages(p); got != 1 {
		t.Fatalf("EstimatePages = %d, want 1", got)
	}
	for _, root := range []string{home, xdg} {
		matches, _ := filepath.Glob(filepath.Join(root, "*", "*pdfcpu*"))
		direct, _ := filepath.Glob(filepath.Join(root, "*pdfcpu*"))
		if len(matches)+len(direct) > 0 {
			t.Errorf("pdfcpu created config state under %s: %v %v", root, matches, direct)
		}
	}
}
