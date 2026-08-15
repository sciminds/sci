package uikit

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ── SafeCmd ───────────────────────────────────────────────────────────

type stubMsg struct{ n int }

func TestSafeCmdPassesThrough(t *testing.T) {
	cmd := SafeCmd(func() tea.Msg { return stubMsg{n: 7} })
	msg := cmd()
	s, ok := msg.(stubMsg)
	if !ok {
		t.Fatalf("expected stubMsg, got %T", msg)
	}
	if s.n != 7 {
		t.Errorf("n = %d, want 7", s.n)
	}
}

func TestSafeCmdRecoversPanic(t *testing.T) {
	cmd := SafeCmd(func() tea.Msg { panic("boom") })
	msg := cmd()
	cp, ok := msg.(CommandPanicMsg)
	if !ok {
		t.Fatalf("expected CommandPanicMsg, got %T", msg)
	}
	if cp.Value != "boom" {
		t.Errorf("Value = %v, want boom", cp.Value)
	}
	if len(cp.Stack) == 0 {
		t.Error("expected non-empty stack trace")
	}
}
