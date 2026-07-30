package zot

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/extract"
)

// TestExtractLibResult_JSONKeysAreFrozen is the byte-stability fence
// for the completed-run result: agents parse this shape, so any new
// field must go on ExtractLibPlanResult (or a new type), never here.
func TestExtractLibResult_JSONKeysAreFrozen(t *testing.T) {
	t.Parallel()
	r := ExtractLibResult{
		Total: 1, Created: 1, Skipped: 1, Cached: 1, Failed: 1,
		Errors:         map[string]string{"K": "boom"},
		Duration:       time.Second,
		BackfilledTags: 1, BackfillFailed: 1,
		LayoutDir: "/x", LayoutWritten: 1,
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(m))
	want := []string{
		"backfill_failed", "backfilled_tags", "cached", "created",
		"duration_ns", "errors", "failed", "layout_dir",
		"layout_written", "skipped", "total",
	}
	if !slices.Equal(got, want) {
		t.Errorf("ExtractLibResult JSON keys changed:\n got %v\nwant %v", got, want)
	}
}

func planSurveyFixture() extract.Survey {
	done := 5
	return extract.Survey{
		Candidates:      10,
		AlreadyDone:     5,
		LayoutDone:      &done,
		Cached:          1,
		NeedsExtraction: 3,
		ArtifactsOnly:   1,
		PlanErrors:      1,
		Selected:        make([]extract.BatchItem, 4),
		Remaining:       0,
		PagesKnownItems: 3, PagesUnknownItems: 1, PagesTotal: 400, Extrapolated: true,
		Buckets: []extract.PageBucket{{Label: "26-50", Min: 26, Max: 50, Items: 3, Pages: 100}},
		ETA:     10 * time.Minute,
		Duplicates: []extract.DuplicateGroup{{
			Content: "500-beef", Pages: 332, Queued: 2,
			Members: []extract.DuplicateMember{
				{ParentKey: "KA", PDFName: "book.pdf", Disposition: extract.DispExtract},
				{ParentKey: "KB", PDFName: "book2.pdf", Disposition: extract.DispExtract},
			},
		}},
		DuplicateWasted: 1,
		Errors:          map[string]string{"KZ": "hash: gone"},
	}
}

func TestExtractLibPlanResult_JSONShape(t *testing.T) {
	t.Parallel()
	r := NewExtractLibPlanResult(planSurveyFixture(), "mps", 2, "/extracts")
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["mode"] != "plan" || m["apply"] != false {
		t.Errorf("mode/apply = %v/%v, want plan/false", m["mode"], m["apply"])
	}
	if m["layout_done"] != float64(5) {
		t.Errorf("layout_done = %v, want 5", m["layout_done"])
	}
	if m["duplicate_wasted_extractions"] != float64(1) {
		t.Errorf("duplicate_wasted_extractions = %v", m["duplicate_wasted_extractions"])
	}
	if m["eta"] == nil || m["pages"] == nil {
		t.Error("pages/eta missing despite estimator data")
	}

	// Classic mode: layout_done is an explicit null, not absent.
	classic := NewExtractLibPlanResult(extract.Survey{Candidates: 1}, "mps", 0, "")
	raw, _ = json.Marshal(classic)
	var m2 map[string]any
	_ = json.Unmarshal(raw, &m2)
	if v, present := m2["layout_done"]; !present || v != nil {
		t.Errorf("classic layout_done = %v (present=%v), want explicit null", v, present)
	}
	if v, present := m2["pages"]; !present || v != nil {
		t.Errorf("degraded pages = %v (present=%v), want explicit null", v, present)
	}
}

func TestExtractLibPlanResult_Warnings(t *testing.T) {
	t.Parallel()
	r := NewExtractLibPlanResult(planSurveyFixture(), "mps", 2, "")
	warns := r.Warnings()
	if len(warns) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warns))
	}
	if warns[0].Code != cmdutil.CodeDuplicate {
		t.Errorf("code = %s, want %s", warns[0].Code, cmdutil.CodeDuplicate)
	}
	if warns[0].Fix != "sci zot doctor duplicates" {
		t.Errorf("fix = %q", warns[0].Fix)
	}

	clean := NewExtractLibPlanResult(extract.Survey{Candidates: 2}, "mps", 0, "")
	if clean.Warnings() != nil {
		t.Errorf("no duplicates must mean nil warnings, got %v", clean.Warnings())
	}
}

func TestExtractLibPlanResult_Human(t *testing.T) {
	t.Parallel()
	// Zero candidates: no panic, honest message.
	empty := NewExtractLibPlanResult(extract.Survey{}, "mps", 0, "")
	if !strings.Contains(empty.Human(), "no items with PDF attachments") {
		t.Errorf("zero-candidate human output: %q", empty.Human())
	}

	// Duplicate block names both keys and the triage command lives in
	// the warning, not the body (cmdutil.Output renders Warnings).
	full := NewExtractLibPlanResult(planSurveyFixture(), "mps", 2, "/extracts")
	h := full.Human()
	for _, want := range []string{"KA", "KB", "332 pages", "duplicate PDF content", "rough ETA"} {
		if !strings.Contains(h, want) {
			t.Errorf("human output missing %q:\n%s", want, h)
		}
	}

	// Degraded (no estimator): never a fabricated number.
	deg := NewExtractLibPlanResult(extract.Survey{Candidates: 3, NeedsExtraction: 3}, "mps", 0, "")
	if !strings.Contains(deg.Human(), "no ETA") {
		t.Errorf("degraded human output must say no ETA:\n%s", deg.Human())
	}
	if strings.Contains(deg.Human(), "rough ETA:") {
		t.Error("degraded output fabricated an ETA")
	}
}
