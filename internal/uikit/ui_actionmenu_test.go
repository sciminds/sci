package uikit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ── helpers ───────────────────────────────────────────────────────────

func keyMsg(s string) tea.Msg {
	return tea.KeyPressMsg{Code: rune(s[0])}
}

func specialKey(code rune) tea.Msg {
	return tea.KeyPressMsg{Code: code}
}

func threeActions() []Action {
	return []Action{
		{Name: "Copy BibTeX", Hint: "to clipboard"},
		{Name: "Open PDF", Hint: "in default viewer"},
		{Name: "Open in Zotero"},
	}
}

// ── Constructor ───────────────────────────────────────────────────────

func TestActionMenuNewDefaults(t *testing.T) {
	m := NewActionMenu("Title", threeActions())
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", m.Cursor())
	}
	if m.Picked() != -1 {
		t.Errorf("picked = %d, want -1", m.Picked())
	}
	if m.Dismissed() {
		t.Error("should not be dismissed on creation")
	}
}

// ── Cursor movement ──────────────────────────────────────────────────

func TestActionMenuCursorDown(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	m, _ = m.Update(keyMsg("j"))
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", m.Cursor())
	}
	m, _ = m.Update(specialKey(tea.KeyDown))
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d, want 2", m.Cursor())
	}
}

func TestActionMenuCursorUp(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	m, _ = m.Update(keyMsg("j"))
	m, _ = m.Update(keyMsg("j"))
	m, _ = m.Update(keyMsg("k"))
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", m.Cursor())
	}
	m, _ = m.Update(specialKey(tea.KeyUp))
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", m.Cursor())
	}
}

func TestActionMenuCursorClampsAtBounds(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	// Up from 0 stays at 0
	m, _ = m.Update(keyMsg("k"))
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0 (clamped top)", m.Cursor())
	}
	// Down past end stays at last
	m, _ = m.Update(keyMsg("j"))
	m, _ = m.Update(keyMsg("j"))
	m, _ = m.Update(keyMsg("j"))
	m, _ = m.Update(keyMsg("j"))
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d, want 2 (clamped bottom)", m.Cursor())
	}
}

// ── Pick ──────────────────────────────────────────────────────────────

func TestActionMenuPick(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	m, _ = m.Update(keyMsg("j")) // cursor → 1
	m, _ = m.Update(specialKey(tea.KeyEnter))
	if m.Picked() != 1 {
		t.Errorf("picked = %d, want 1", m.Picked())
	}
	if m.Dismissed() {
		t.Error("picking should not dismiss")
	}
}

func TestActionMenuPickFirstItem(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	m, _ = m.Update(specialKey(tea.KeyEnter))
	if m.Picked() != 0 {
		t.Errorf("picked = %d, want 0", m.Picked())
	}
}

// ── Dismiss ──────────────────────────────────────────────────────────

func TestActionMenuDismissEsc(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	m, _ = m.Update(specialKey(tea.KeyEscape))
	if !m.Dismissed() {
		t.Error("esc should dismiss")
	}
	if m.Picked() != -1 {
		t.Errorf("picked = %d, want -1 after dismiss", m.Picked())
	}
}

func TestActionMenuDismissCtrlC(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	// ctrl+c is a special key with modifier
	m, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.Dismissed() {
		t.Error("ctrl+c should dismiss")
	}
}

// ── Disabled items ───────────────────────────────────────────────────

func TestActionMenuSkipsDisabledOnMove(t *testing.T) {
	actions := []Action{
		{Name: "A"},
		{Name: "B", Disabled: "not available"},
		{Name: "C"},
	}
	m := NewActionMenu("T", actions)
	// Down from 0 should skip 1 (disabled), land on 2
	m, _ = m.Update(keyMsg("j"))
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d, want 2 (skipped disabled)", m.Cursor())
	}
	// Up from 2 should skip 1, land on 0
	m, _ = m.Update(keyMsg("k"))
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0 (skipped disabled)", m.Cursor())
	}
}

func TestActionMenuCannotPickDisabled(t *testing.T) {
	actions := []Action{
		{Name: "A", Disabled: "nope"},
		{Name: "B"},
	}
	// Constructor should start on first enabled item
	m := NewActionMenu("T", actions)
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1 (first enabled)", m.Cursor())
	}
}

func TestActionMenuEnterOnDisabledNoPick(t *testing.T) {
	actions := []Action{
		{Name: "A", Disabled: "nope"},
		{Name: "B"},
	}
	m := NewActionMenu("T", actions)
	// Force cursor to disabled item for safety
	m.cursor = 0
	m, _ = m.Update(specialKey(tea.KeyEnter))
	if m.Picked() != -1 {
		t.Errorf("picked = %d, want -1 (disabled)", m.Picked())
	}
}

// ── Non-key messages are ignored ─────────────────────────────────────

func TestActionMenuIgnoresNonKeyMsg(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.Cursor() != 0 {
		t.Errorf("cursor changed on non-key msg: %d", m.Cursor())
	}
	if m.Picked() != -1 {
		t.Error("picked changed on non-key msg")
	}
}

// ── View ──────────────────────────────────────────────────────────────

func TestActionMenuViewContainsTitle(t *testing.T) {
	m := NewActionMenu("My Actions", threeActions())
	out := m.View(80)
	if !strings.Contains(out, "My Actions") {
		t.Error("view should contain the title")
	}
}

func TestActionMenuViewContainsActionNames(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	out := m.View(80)
	for _, a := range threeActions() {
		if !strings.Contains(out, a.Name) {
			t.Errorf("view should contain action name %q", a.Name)
		}
	}
}

func TestActionMenuViewContainsHints(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	out := m.View(80)
	if !strings.Contains(out, "enter") {
		t.Error("view should contain 'enter' hint")
	}
	if !strings.Contains(out, "esc") {
		t.Error("view should contain 'esc' hint")
	}
}

func TestActionMenuViewShowsDisabledReason(t *testing.T) {
	actions := []Action{
		{Name: "A", Disabled: "no attachment"},
		{Name: "B"},
	}
	m := NewActionMenu("T", actions)
	out := m.View(80)
	if !strings.Contains(out, "no attachment") {
		t.Error("view should show disabled reason")
	}
}

func TestActionMenuViewShowsHint(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	out := m.View(80)
	if !strings.Contains(out, "to clipboard") {
		t.Error("view should show action hint")
	}
}

func TestActionMenuViewCursorOnSelected(t *testing.T) {
	m := NewActionMenu("T", threeActions())
	out := m.View(80)
	if !strings.Contains(out, IconCursor) {
		t.Error("view should show cursor icon on active item")
	}
}
