package hygiene

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestCitekeysFromRows_CountsBuckets(t *testing.T) {
	t.Parallel()
	rows := []local.CiteKeyRow{
		// Canonical v2 key → Valid, no finding.
		{Key: "AAAA1111", Title: "Deep Learning", CitationKey: "smith2020-deeplear-AAAA1111"},
		// BBT camelCase → NonCanonical, soft finding.
		{Key: "BBBB2222", Title: "Some Paper", CitationKey: "jonesSomePaper2021"},
		// BibTeX-illegal whitespace → Invalid, error finding.
		{Key: "CCCC3333", Title: "Other", CitationKey: "has space"},
		// No stored key at all → Unstored, no finding.
		{Key: "DDDD4444", Title: "Unkeyed"},
		// Legacy BBT line in extra → resolves to non-canonical.
		{Key: "EEEE5555", Title: "Legacy", Extra: "Citation Key: legacyKey1999\nnote text"},
	}
	rep := CitekeysFromRows(rows)
	if rep.Scanned != 5 {
		t.Errorf("Scanned = %d, want 5", rep.Scanned)
	}
	s := rep.Stats.(CitekeysStats)
	if s.Stored != 4 {
		t.Errorf("Stored = %d, want 4", s.Stored)
	}
	if s.Unstored != 1 {
		t.Errorf("Unstored = %d, want 1", s.Unstored)
	}
	if s.Valid != 1 {
		t.Errorf("Valid = %d, want 1 (only AAAA1111)", s.Valid)
	}
	if s.NonCanonical != 2 {
		t.Errorf("NonCanonical = %d, want 2 (BBB + EEE)", s.NonCanonical)
	}
	if s.Invalid != 1 {
		t.Errorf("Invalid = %d, want 1 (CCC has whitespace)", s.Invalid)
	}
	if s.Collisions != 0 {
		t.Errorf("Collisions = %d, want 0", s.Collisions)
	}
}

func TestCitekeysFromRows_FindingsShape(t *testing.T) {
	t.Parallel()
	rows := []local.CiteKeyRow{
		{Key: "BADKEY01", Title: "Broken", CitationKey: "has{brace"},
		{Key: "SOFTKEY1", Title: "Soft", CitationKey: "jollyCamelCase2020"},
	}
	rep := CitekeysFromRows(rows)
	if len(rep.Findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(rep.Findings))
	}
	byKey := map[string]Finding{}
	for _, f := range rep.Findings {
		byKey[f.ItemKey] = f
	}
	bad := byKey["BADKEY01"]
	if bad.Severity != SevError {
		t.Errorf("BADKEY01 severity = %v, want SevError", bad.Severity)
	}
	if bad.Kind != "invalid" {
		t.Errorf("BADKEY01 kind = %q, want invalid", bad.Kind)
	}
	if !bad.Fixable {
		t.Errorf("BADKEY01 should be Fixable — the fix command can overwrite it")
	}
	if !strings.Contains(bad.Message, "`has{brace`") {
		t.Errorf("BADKEY01 message should quote the key: %q", bad.Message)
	}
	soft := byKey["SOFTKEY1"]
	if soft.Severity != SevWarn {
		t.Errorf("SOFTKEY1 severity = %v, want SevWarn", soft.Severity)
	}
	if soft.Kind != "non-canonical" {
		t.Errorf("SOFTKEY1 kind = %q, want non-canonical", soft.Kind)
	}
}

func TestCitekeysFromRows_DetectsCollisions(t *testing.T) {
	t.Parallel()
	// Three items share the same (canonical!) cite-key. That's a bug even
	// though each key individually passes Validate — uniqueness is what
	// makes a cite-key useful.
	rows := []local.CiteKeyRow{
		{Key: "AAAA1111", Title: "First", CitationKey: "smith2020-dup-AAAA1111"},
		{Key: "BBBB2222", Title: "Second", CitationKey: "smith2020-dup-AAAA1111"},
		{Key: "CCCC3333", Title: "Third", CitationKey: "smith2020-dup-AAAA1111"},
	}
	rep := CitekeysFromRows(rows)
	s := rep.Stats.(CitekeysStats)
	if s.Collisions != 3 {
		t.Errorf("Collisions = %d, want 3 (one finding per participating item)", s.Collisions)
	}
	// One finding per item, all marked collision + SevError.
	collisions := 0
	for _, f := range rep.Findings {
		if f.Kind == "collision" {
			collisions++
			if f.Severity != SevError {
				t.Errorf("collision severity = %v, want SevError", f.Severity)
			}
		}
	}
	if collisions != 3 {
		t.Errorf("collision findings = %d, want 3", collisions)
	}
}

func TestCitekeysFromRows_IgnoresUnstoredItems(t *testing.T) {
	t.Parallel()
	// An item with neither citationKey nor extra contributes to Scanned
	// and Unstored but not to any other bucket and emits no finding.
	// (Fix command materializes a synthesized key for these; the check is
	// strictly read-only.)
	rows := []local.CiteKeyRow{
		{Key: "AAAA1111", Title: "No stored key"},
	}
	rep := CitekeysFromRows(rows)
	if len(rep.Findings) != 0 {
		t.Errorf("findings = %d, want 0 for unstored-only input", len(rep.Findings))
	}
	s := rep.Stats.(CitekeysStats)
	if s.Unstored != 1 || s.Stored != 0 {
		t.Errorf("stats = %+v, want Unstored=1 Stored=0", s)
	}
}

func TestCitekeysFromRows_PrefersNativeOverExtra(t *testing.T) {
	t.Parallel()
	// When both native citationKey and legacy extra are present, Resolve
	// wins with the native value — matches the export-time behavior so
	// the check can't disagree with what users see in their .bib output.
	rows := []local.CiteKeyRow{{
		Key:         "AAAA1111",
		Title:       "Conflict",
		CitationKey: "smith2020-conf-AAAA1111",
		Extra:       "Citation Key: legacyKey1999",
	}}
	rep := CitekeysFromRows(rows)
	s := rep.Stats.(CitekeysStats)
	if s.Valid != 1 || s.NonCanonical != 0 {
		t.Errorf("stats = %+v, want Valid=1 NonCanonical=0 (native wins)", s)
	}
}
