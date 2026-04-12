package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	projnew "github.com/sciminds/cli/internal/proj/new"
)

func testFiles() []projnew.ConfigFile {
	return []projnew.ConfigFile{
		{Path: ".gitignore", Changed: true},
		{Path: "pyproject.toml", Changed: false, Exists: true},
		{Path: "README.md", Changed: true},
	}
}

func testModel() Model {
	return New(Options{Dir: "/tmp/test", Files: testFiles()})
}

// TestViewAtZeroSize ensures the proj config TUI can render View() before any
// WindowSizeMsg arrives (width=0, height=0).
func TestViewAtZeroSize(t *testing.T) {
	t.Parallel()
	m := testModel()
	_ = m.View() // must not panic
}

func TestInitialPhase(t *testing.T) {
	t.Parallel()
	m := testModel()
	if m.phase != phaseSelecting {
		t.Errorf("initial phase = %d, want phaseSelecting", m.phase)
	}
}

func TestQuitOnQ(t *testing.T) {
	t.Parallel()
	m := testModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("pressing q should produce a quit cmd")
	}
}

func TestQuitOnEsc(t *testing.T) {
	t.Parallel()
	m := testModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Error("pressing esc should produce a quit cmd")
	}
}

func TestCtrlCQuits(t *testing.T) {
	t.Parallel()
	m := testModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("ctrl+c should produce a quit cmd")
	}
}

func TestWindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	rm := updated.(Model)
	if rm.width != 120 || rm.height != 40 {
		t.Errorf("dimensions = %dx%d, want 120x40", rm.width, rm.height)
	}
}

func TestApplyDoneMsg(t *testing.T) {
	t.Parallel()
	m := testModel()
	// Mark first file as applied
	m.files[0].applied = true
	m.phase = phaseApplying

	updated, _ := m.Update(applyDoneMsg{err: nil})
	rm := updated.(Model)
	if rm.phase != phaseDone {
		t.Errorf("phase = %d, want phaseDone", rm.phase)
	}
	if rm.Err != nil {
		t.Errorf("unexpected error: %v", rm.Err)
	}
	if len(rm.Result.Changed) != 1 {
		t.Errorf("changed = %d, want 1", len(rm.Result.Changed))
	}
}

func TestApplyDoneMsgWithError(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.phase = phaseApplying

	updated, _ := m.Update(applyDoneMsg{err: errTest})
	rm := updated.(Model)
	if rm.phase != phaseDone {
		t.Errorf("phase = %d, want phaseDone", rm.phase)
	}
	if rm.Err != errTest {
		t.Errorf("err = %v, want %v", rm.Err, errTest)
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }

func TestViewApplyingShowsSpinner(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.phase = phaseApplying
	m.width = 80
	m.height = 24
	v := m.View()
	if !strings.Contains(v.Content, "Applying") {
		t.Errorf("applying view should contain 'Applying', got %q", v.Content)
	}
}

func TestViewDoneShowsResults(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.phase = phaseDone
	m.width = 80
	m.height = 24
	m.files[0].applied = true
	v := m.View()
	if !strings.Contains(v.Content, ".gitignore") {
		t.Errorf("done view should contain applied file name, got %q", v.Content)
	}
}

func TestFileEntryStatusLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file projnew.ConfigFile
		want string
	}{
		{"create", projnew.ConfigFile{Changed: true, Exists: false}, "create"},
		{"overwrite", projnew.ConfigFile{Changed: true, Exists: true}, "overwrite"},
		{"up to date", projnew.ConfigFile{Changed: false, Exists: true}, "up to date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fileEntry{file: tt.file}
			if got := f.statusLabel(); got != tt.want {
				t.Errorf("statusLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDonePhaseEnterQuits(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.phase = phaseDone
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Error("enter at phaseDone should produce a quit cmd")
	}
}
