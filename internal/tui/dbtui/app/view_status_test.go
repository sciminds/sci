package app

import (
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// makeStatusModel builds a minimal Model for status-bar tests.
func makeStatusModel(readOnly bool) *Model {
	tab := makeTab([]string{"id", "name"}, [][]string{{"1", "alice"}, {"2", "bob"}})
	tab.ReadOnly = readOnly
	return &Model{
		zones:  zone.New(),
		styles: ui.TUI,
		tabs:   []Tab{*tab},
		active: 0,
		mode:   modeNormal,
		width:  200,
	}
}

// hintIDs extracts the IDs from a hint slice for easy comparison.
func hintIDs(hints []statusHint) []string {
	ids := make([]string, len(hints))
	for i, h := range hints {
		ids[i] = h.ID
	}
	return ids
}

// hasHintID returns true if the hint slice contains an entry with the given ID.
func hasHintID(hints []statusHint, id string) bool {
	for _, h := range hints {
		if h.ID == id {
			return true
		}
	}
	return false
}

// hintByID returns the hint with the given ID, or nil.
func hintByID(hints []statusHint, id string) *statusHint {
	for i := range hints {
		if hints[i].ID == id {
			return &hints[i]
		}
	}
	return nil
}

// ── Nav mode: writable tab should contain all expected hints ────────────────

func TestNavHintsWritableContainsAllExpected(t *testing.T) {
	m := makeStatusModel(false)
	hints := m.normalModeHints("NAV")

	expected := []string{"mode", "edit", "visual", "search", "sort", "pin", "filter", "preview", "hide", "rename", "tables", "help"}
	for _, id := range expected {
		if !hasHintID(hints, id) {
			t.Errorf("missing hint %q; got IDs: %v", id, hintIDs(hints))
		}
	}
}

// ── Nav mode: read-only tab hides edit and visual ───────────────────────────

func TestNavHintsReadOnlyHidesEditAndVisual(t *testing.T) {
	m := makeStatusModel(true)
	hints := m.normalModeHints("NAV")

	if hasHintID(hints, "edit") {
		t.Error("read-only tab should not have 'edit' hint")
	}
	if hasHintID(hints, "visual") {
		t.Error("read-only tab should not have 'visual' hint")
	}
}

// ── Nav mode: read-only tab still has read-only hints ───────────────────────

func TestNavHintsReadOnlyKeepsReadHints(t *testing.T) {
	m := makeStatusModel(true)
	hints := m.normalModeHints("NAV")

	for _, id := range []string{"mode", "search", "sort", "pin", "filter", "preview", "tables", "help"} {
		if !hasHintID(hints, id) {
			t.Errorf("read-only tab should still have %q hint; got IDs: %v", id, hintIDs(hints))
		}
	}
}

// ── Ordering: mode first, help last ─────────────────────────────────────────

func TestNavHintsOrdering(t *testing.T) {
	m := makeStatusModel(false)
	hints := m.normalModeHints("NAV")

	if len(hints) == 0 {
		t.Fatal("no hints")
	}
	if hints[0].ID != "mode" {
		t.Errorf("first hint should be 'mode', got %q", hints[0].ID)
	}
	if hints[len(hints)-1].ID != "help" {
		t.Errorf("last hint should be 'help', got %q", hints[len(hints)-1].ID)
	}
}

// ── Priority tiers ──────────────────────────────────────────────────────────

func TestNavHintsPriorityTiers(t *testing.T) {
	m := makeStatusModel(false)
	hints := m.normalModeHints("NAV")

	// Required hints: mode and help.
	for _, id := range []string{"mode", "help"} {
		h := hintByID(hints, id)
		if h == nil {
			t.Fatalf("missing %q", id)
		}
		if !h.Required {
			t.Errorf("%q should be Required", id)
		}
	}

	// Tier 1 (priority 1) — core actions: should compact before dropping.
	for _, id := range []string{"edit", "visual", "search", "sort"} {
		h := hintByID(hints, id)
		if h == nil {
			continue // edit/visual absent on read-only, tested separately
		}
		if h.Priority != 1 {
			t.Errorf("%q should have Priority 1, got %d", id, h.Priority)
		}
		if h.Compact == "" {
			t.Errorf("%q should have a Compact form", id)
		}
	}

	// Tier 2 (priority 2) — common ops.
	for _, id := range []string{"pin", "filter", "preview"} {
		h := hintByID(hints, id)
		if h == nil {
			t.Fatalf("missing %q", id)
		}
		if h.Priority != 2 {
			t.Errorf("%q should have Priority 2, got %d", id, h.Priority)
		}
	}

	// Tier 3 (priority 3) — least common, first to drop.
	for _, id := range []string{"hide", "rename", "tables"} {
		h := hintByID(hints, id)
		if h == nil {
			t.Fatalf("missing %q", id)
		}
		if h.Priority != 3 {
			t.Errorf("%q should have Priority 3, got %d", id, h.Priority)
		}
	}
}

// ── All non-required hints have a Compact form for graceful degradation ─────

func TestNavHintsAllNonRequiredHaveCompact(t *testing.T) {
	m := makeStatusModel(false)
	hints := m.normalModeHints("NAV")

	for _, h := range hints {
		if !h.Required && h.Compact == "" {
			t.Errorf("hint %q (priority %d) should have a Compact form for narrow terminals", h.ID, h.Priority)
		}
	}
}
