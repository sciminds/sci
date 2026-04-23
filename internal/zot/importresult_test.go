package zot

import (
	"strings"
	"testing"
)

func TestImportBatchResult_HumanShowsCounters(t *testing.T) {
	t.Parallel()
	r := ImportBatchResult{
		Total: 3, Recognized: 2, Imported: 1, Failed: 0,
		Duration: "2.5s",
		Items: []ImportBatchItem{
			{Path: "/x/a.pdf", Recognized: true, Title: "A"},
			{Path: "/x/b.pdf", Recognized: true, Title: "B"},
			{Path: "/x/c.pdf"},
		},
	}
	out := r.Human()
	for _, want := range []string{"3/3", "2 recognized", "1 not recognized", "0 failed", "2.5s"} {
		if !strings.Contains(out, want) {
			t.Errorf("Human() missing %q\n--output--\n%s", want, out)
		}
	}
}

func TestImportBatchResult_HumanListsFailures(t *testing.T) {
	t.Parallel()
	r := ImportBatchResult{
		Total: 2, Imported: 1, Failed: 1, Duration: "1s",
		Items: []ImportBatchItem{
			{Path: "/x/ok.pdf"},
			{Path: "/x/bad.pdf", Error: "upload: status 500"},
		},
	}
	out := r.Human()
	if !strings.Contains(out, "bad.pdf") || !strings.Contains(out, "status 500") {
		t.Errorf("failure line missing from Human()\n%s", out)
	}
	// Successful items should NOT be re-listed below the summary.
	if strings.Contains(out, "ok.pdf") {
		t.Errorf("successful items should not appear as detail lines\n%s", out)
	}
}

func TestImportBatchResult_HumanCapsFailureList(t *testing.T) {
	t.Parallel()
	// 12 failures should render maxBatchFailureLines (10) lines + a "+N more" line.
	items := make([]ImportBatchItem, 12)
	for i := range items {
		items[i] = ImportBatchItem{Path: "/x/f.pdf", Error: "boom"}
	}
	r := ImportBatchResult{Total: 12, Failed: 12, Duration: "1s", Items: items}
	out := r.Human()
	lines := strings.Count(out, "f.pdf — boom")
	if lines != maxBatchFailureLines {
		t.Errorf("rendered %d failure detail lines, want %d (cap)", lines, maxBatchFailureLines)
	}
	if !strings.Contains(out, "+2 more") {
		t.Errorf("expected '+2 more' overflow line\n%s", out)
	}
}

func TestImportBatchResult_HumanShowsSkippedCount(t *testing.T) {
	t.Parallel()
	r := ImportBatchResult{Total: 1, Imported: 1, Skipped: 4, Duration: "1s",
		Items: []ImportBatchItem{{Path: "/x/a.pdf"}}}
	out := r.Human()
	if !strings.Contains(out, "skipped 4 non-PDF") {
		t.Errorf("missing skipped-non-PDF line\n%s", out)
	}
}
