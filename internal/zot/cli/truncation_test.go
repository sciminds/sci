package cli

// truncation_test.go — Phase D: a LIMITed page must never read as the whole
// result set. search and item list carry Total + Truncated in JSON and a
// "showing N of M" footer for humans.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
)

func TestSearch_LimitSlice_ReportsTotalAndTruncated(t *testing.T) {
	withOrientConfig(t) // 4 items titled Paper One..Four

	out, err := runItemRead(t, "--json", "--library", "personal", "search", "--limit", "2", "Paper")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var res zot.ListResult
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Count != 2 {
		t.Errorf("count = %d, want 2", res.Count)
	}
	if res.Total != 4 {
		t.Errorf("total = %d, want 4", res.Total)
	}
	if !res.Truncated {
		t.Error("truncated should be true when count < total")
	}
}

func TestSearch_NoSlice_NotTruncated(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "search", "Paper")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var res zot.ListResult
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Truncated {
		t.Errorf("truncated should be false at count=%d total=%d", res.Count, res.Total)
	}
	if res.Total != res.Count {
		t.Errorf("total %d should equal count %d when nothing was sliced", res.Total, res.Count)
	}
}

func TestItemList_LimitSlice_ReportsTotalAndTruncated(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "list", "--limit", "1")
	if err != nil {
		t.Fatalf("item list: %v", err)
	}
	var res zot.ListResult
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Count != 1 {
		t.Errorf("count = %d, want 1", res.Count)
	}
	if res.Total != 4 {
		t.Errorf("total = %d, want 4", res.Total)
	}
	if !res.Truncated {
		t.Error("truncated should be true")
	}
}

func TestListResult_Human_TruncationFooter(t *testing.T) {
	t.Parallel()
	res := zot.ListResult{
		Count:     1,
		Total:     213,
		Truncated: true,
		Items:     []local.Item{{Key: "ABCD1234", Title: "One"}},
	}
	human := res.Human()
	if !strings.Contains(human, "showing 1 of 213") {
		t.Errorf("footer should read 'showing 1 of 213', got:\n%s", human)
	}
	if !strings.Contains(human, "--limit") {
		t.Errorf("footer should point at --limit, got:\n%s", human)
	}
}
