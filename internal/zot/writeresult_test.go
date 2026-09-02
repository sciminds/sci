package zot

import (
	"strings"
	"testing"
)

func TestWriteResult_HumanDefaultsToActionKindTarget(t *testing.T) {
	t.Parallel()
	got := WriteResult{Action: "created", Kind: "collection", Target: "ABCD1234"}.Human()
	if !strings.Contains(got, "created collection ABCD1234") {
		t.Errorf("Human() = %q", got)
	}
	explicit := WriteResult{Action: "added", Kind: "item", Target: "K", Message: "added item K to collection C"}.Human()
	if !strings.Contains(explicit, "added item K to collection C") {
		t.Errorf("Human() with Message = %q", explicit)
	}
}

// TestBulkWriteResult_HumanListsFailuresDeterministically: a batch report
// prints its failures in sorted key order, so two runs over the same input
// read the same and a diff of them is meaningful.
func TestBulkWriteResult_HumanListsFailuresDeterministically(t *testing.T) {
	t.Parallel()
	r := BulkWriteResult{
		Action: "added", Kind: "item", Total: 3,
		Success: []string{"AAAA0001"},
		Failed:  map[string]string{"ZZZZ0003": "not found", "MMMM0002": "412"},
	}
	got := r.Human()
	if !strings.Contains(got, "added 1/3 item(s)") {
		t.Errorf("headline missing: %q", got)
	}
	if strings.Index(got, "MMMM0002") > strings.Index(got, "ZZZZ0003") {
		t.Errorf("failures not sorted: %q", got)
	}
}
