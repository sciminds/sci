package cliui

import (
	"testing"
)

func TestRunnerModel_Init(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Loading…", false)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a tick Cmd")
	}
}

func TestRunnerModel_DoneMsg(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Loading…", false)
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
	m := newRunnerModel("Loading…", false)
	updated, _ := m.Update(statusMsg("step 1"))
	rm := updated.(runnerModel)

	if rm.status != "step 1" {
		t.Errorf("status = %q, want %q", rm.status, "step 1")
	}
}

func TestRunnerModel_ViewShowsTitle(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Installing…", false)
	v := m.View()
	got := v.Content
	if got == "" {
		t.Error("expected non-empty view")
	}
}

func TestRunnerModel_ViewEmptyWhenDone(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Installing…", false)
	m.done = true
	v := m.View()
	if v.Content != "" {
		t.Errorf("expected empty view when done, got %q", v.Content)
	}
}
