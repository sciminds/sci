package hygiene

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestScanSubobjectDOIs_FromFieldValues(t *testing.T) {
	t.Parallel()
	rows := []local.FieldValue{
		// Subobject DOIs — must be flagged.
		{Key: "F1", Title: "Frontiers paper", Field: "DOI", Value: "10.3389/fnhum.2013.00015/abstract"},
		{Key: "F2", Title: "Frontiers full", Field: "DOI", Value: "10.3389/fpsyg.2014.01427/full"},
		{Key: "P1", Title: "PLOS table", Field: "DOI", Value: "10.1371/journal.pcbi.1000808.t001"},
		{Key: "P2", Title: "PLOS supplement", Field: "DOI", Value: "10.1371/journal.pone.0002597.s007"},
		{Key: "N1", Title: "PNAS supplement", Field: "DOI", Value: "10.1073/pnas.0908104107/-/DCSupplemental"},
		// Clean DOIs — must NOT generate findings.
		{Key: "OK1", Title: "Nature", Field: "DOI", Value: "10.1038/nature12373"},
		{Key: "OK2", Title: "Frontiers parent", Field: "DOI", Value: "10.3389/fpsyg.2024.123"},
		// Non-DOI fields are ignored.
		{Key: "X1", Title: "Other", Field: "url", Value: "10.3389/foo/abstract"},
	}
	rep := ScanSubobjectDOIs(rows)
	if rep.Check != "dois" {
		t.Errorf("Check = %q, want dois", rep.Check)
	}
	if rep.Scanned != len(rows) {
		t.Errorf("Scanned = %d, want %d", rep.Scanned, len(rows))
	}
	if len(rep.Findings) != 5 {
		t.Fatalf("findings = %d, want 5: %+v", len(rep.Findings), rep.Findings)
	}
	for _, f := range rep.Findings {
		if f.Severity != SevWarn {
			t.Errorf("%s severity = %v, want SevWarn", f.ItemKey, f.Severity)
		}
		if f.Kind != "subobject" {
			t.Errorf("%s kind = %q, want subobject", f.ItemKey, f.Kind)
		}
		if !f.Fixable {
			t.Errorf("%s should be Fixable", f.ItemKey)
		}
		if !strings.Contains(f.Message, "subobject DOI") {
			t.Errorf("%s message = %q, want subobject prefix", f.ItemKey, f.Message)
		}
	}

	stats, ok := rep.Stats.(SubobjectDOIStats)
	if !ok {
		t.Fatalf("Stats is %T, want SubobjectDOIStats", rep.Stats)
	}
	if stats.Subobject != 5 {
		t.Errorf("Subobject = %d, want 5", stats.Subobject)
	}
}

func TestScanSubobjectDOIs_EmptyInput(t *testing.T) {
	t.Parallel()
	rep := ScanSubobjectDOIs(nil)
	if len(rep.Findings) != 0 {
		t.Errorf("nil rows should yield no findings, got %+v", rep.Findings)
	}
	if rep.Check != "dois" {
		t.Errorf("Check = %q, want dois", rep.Check)
	}
}

func TestScanSubobjectDOIs_OnlyCleanDOIs(t *testing.T) {
	t.Parallel()
	rows := []local.FieldValue{
		{Key: "A", Title: "ok", Field: "DOI", Value: "10.1038/nature12373"},
		{Key: "B", Title: "ok", Field: "DOI", Value: "10.1371/journal.pone.0002597"},
	}
	rep := ScanSubobjectDOIs(rows)
	if len(rep.Findings) != 0 {
		t.Errorf("clean rows should yield no findings, got %+v", rep.Findings)
	}
}
