package ui

import (
	"testing"
)

func TestSpinnerModel_Init(t *testing.T) {
	m := newSpinnerModel("Loading…")
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a tick Cmd")
	}
}

func TestSpinnerModel_DoneMsg(t *testing.T) {
	m := newSpinnerModel("Loading…")
	updated, cmd := m.Update(doneMsg{err: nil})
	sm := updated.(spinnerModel)

	if !sm.done {
		t.Error("expected done=true after doneMsg")
	}
	if sm.err != nil {
		t.Errorf("expected nil error, got %v", sm.err)
	}
	// Should return a quit command.
	if cmd == nil {
		t.Error("expected quit Cmd after doneMsg")
	}
}

func TestSpinnerModel_StatusMsg(t *testing.T) {
	m := newSpinnerModel("Loading…")
	updated, _ := m.Update(statusMsg("step 1"))
	sm := updated.(spinnerModel)

	if sm.status != "step 1" {
		t.Errorf("status = %q, want %q", sm.status, "step 1")
	}
}

func TestSpinnerModel_ViewShowsTitle(t *testing.T) {
	m := newSpinnerModel("Installing…")
	v := m.View()
	got := v.Content
	if got == "" {
		t.Error("expected non-empty view")
	}
}

func TestSpinnerModel_ViewEmptyWhenDone(t *testing.T) {
	m := newSpinnerModel("Installing…")
	m.done = true
	v := m.View()
	if v.Content != "" {
		t.Errorf("expected empty view when done, got %q", v.Content)
	}
}
