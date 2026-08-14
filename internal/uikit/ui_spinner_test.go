package uikit

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRunnerModel_Init(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Loading…")
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a tick Cmd")
	}
}

func TestRunnerModel_DoneMsg(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Loading…")
	updated, cmd := m.Update(doneMsg{err: nil})
	rm := updated.(runnerModel)

	if !rm.done {
		t.Error("expected done=true after doneMsg")
	}
	if rm.err != nil {
		t.Errorf("expected nil error, got %v", rm.err)
	}
	if cmd == nil {
		t.Error("expected quit Cmd after doneMsg")
	}
}

func TestRunnerModel_StatusMsg(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Loading…")
	updated, _ := m.Update(statusMsg("step 1"))
	rm := updated.(runnerModel)

	if rm.status != "step 1" {
		t.Errorf("status = %q, want %q", rm.status, "step 1")
	}
}

func TestRunnerModel_ViewShowsTitle(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Installing…")
	v := m.View()
	got := v.Content
	if got == "" {
		t.Error("expected non-empty view")
	}
}

func TestRunnerModel_ViewEmptyWhenDone(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Installing…")
	m.done = true
	v := m.View()
	if v.Content != "" {
		t.Errorf("expected empty view when done, got %q", v.Content)
	}
}

// TestRunnerModel_CtrlCReportsInterrupted keeps the abort path covered.
//
// It lived with the progress runner's tests until that runner was deleted;
// the behavior is the spinner's too, and losing it would have left ctrl+c —
// the one path where the model overrides the work's own error — untested.
func TestRunnerModel_CtrlCReportsInterrupted(t *testing.T) {
	m := newRunnerModel("working")
	got, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	rm := got.(runnerModel)

	if !rm.done {
		t.Error("ctrl+c should mark the model done")
	}
	if !errors.Is(rm.err, ErrInterrupted) {
		t.Errorf("err = %v, want ErrInterrupted", rm.err)
	}
	if cmd == nil {
		t.Error("ctrl+c should return a quit command")
	}
}
