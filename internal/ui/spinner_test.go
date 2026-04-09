package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSpinnerModel_SuspendHidesView(t *testing.T) {
	m := newSpinnerModel("Working…")
	// Simulate Init tick so spinner has content.
	m.spinner, _ = m.spinner.Update(m.spinner.Tick())

	// Visible before suspend.
	if v := m.View(); v.Content == "" {
		t.Error("spinner should be visible before suspend")
	}

	// Suspend hides.
	updated, _ := m.Update(spinnerSuspendMsg{})
	m = updated.(spinnerModel)
	if v := m.View(); v.Content != "" {
		t.Errorf("spinner should be hidden after suspend, got %q", v.Content)
	}

	// Resume restores.
	updated, _ = m.Update(spinnerResumeMsg{})
	m = updated.(spinnerModel)
	if v := m.View(); v.Content == "" {
		t.Error("spinner should be visible after resume")
	}
}

func TestSpinnerModel_DoneHidesView(t *testing.T) {
	m := newSpinnerModel("Loading…")
	m.spinner, _ = m.spinner.Update(m.spinner.Tick())

	updated, cmd := m.Update(spinnerDoneMsg{err: nil})
	m = updated.(spinnerModel)

	if !m.done {
		t.Error("expected done=true")
	}
	if m.err != nil {
		t.Errorf("unexpected error: %v", m.err)
	}
	if v := m.View(); v.Content != "" {
		t.Errorf("view should be empty after done, got %q", v.Content)
	}
	// Should issue Quit.
	if cmd == nil {
		t.Error("done should return tea.Quit cmd")
	}
}

func TestSpinnerModel_DonePreservesError(t *testing.T) {
	m := newSpinnerModel("Loading…")
	want := errors.New("something broke")

	updated, _ := m.Update(spinnerDoneMsg{err: want})
	m = updated.(spinnerModel)

	if !errors.Is(m.err, want) {
		t.Errorf("err = %v, want %v", m.err, want)
	}
}

func TestSpinnerModel_TitleUpdateClearsStatus(t *testing.T) {
	m := newSpinnerModel("Phase 1")
	m.spinner, _ = m.spinner.Update(m.spinner.Tick())

	// Set a status.
	updated, _ := m.Update(spinnerStatusMsg("detail"))
	m = updated.(spinnerModel)
	if m.status != "detail" {
		t.Errorf("status = %q, want %q", m.status, "detail")
	}

	// Title change clears status.
	updated, _ = m.Update(spinnerTitleMsg("Phase 2"))
	m = updated.(spinnerModel)
	if m.title != "Phase 2" {
		t.Errorf("title = %q, want %q", m.title, "Phase 2")
	}
	if m.status != "" {
		t.Errorf("status should be cleared after title change, got %q", m.status)
	}
}

func TestSpinnerModel_CtrlCQuits(t *testing.T) {
	m := newSpinnerModel("Working…")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("Ctrl+C should return tea.Quit cmd")
	}
}
