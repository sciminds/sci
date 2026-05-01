package uikit

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	mdRunTermW    = 80
	mdRunTermH    = 24
	mdRunWaitFor  = 3 * time.Second
	mdRunFinalFor = 5 * time.Second
)

func startMdProgramTeatest(t *testing.T, name, markdown string) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t,
		newMdProgram(name, markdown),
		teatest.WithInitialTermSize(mdRunTermW, mdRunTermH),
	)
	t.Cleanup(func() { _ = tm.Quit() })
	return tm
}

func mdRunFinalModel(t *testing.T, tm *teatest.TestModel) *mdProgram {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	return tm.FinalModel(t, teatest.WithFinalTimeout(mdRunFinalFor)).(*mdProgram)
}

func TestRunMdViewerRendersTitleAndBody(t *testing.T) {
	tm := startMdProgramTeatest(t, "notes.md", "# Hello World\n\nbody text")

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("notes.md")) &&
			bytes.Contains(bts, []byte("Hello World"))
	}, teatest.WithDuration(mdRunWaitFor))

	_ = mdRunFinalModel(t, tm)
}

func TestRunMdViewerQuitsOnQ(t *testing.T) {
	tm := startMdProgramTeatest(t, "notes.md", "# h\n\nbody")
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("body"))
	}, teatest.WithDuration(mdRunWaitFor))

	tm.Send(tea.KeyPressMsg{Code: 'q'})

	fm := mdRunFinalModel(t, tm)
	if !fm.quitting {
		t.Error("program should be quitting after q")
	}
}

func TestRunMdViewerQuitsOnEsc(t *testing.T) {
	tm := startMdProgramTeatest(t, "notes.md", "# h\n\nbody")
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("body"))
	}, teatest.WithDuration(mdRunWaitFor))

	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})

	fm := mdRunFinalModel(t, tm)
	if !fm.quitting {
		t.Error("program should be quitting after esc")
	}
}

func TestRunMdViewerQDuringSearchTypesIntoQuery(t *testing.T) {
	tm := startMdProgramTeatest(t, "notes.md", "# h\n\nquery target")
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("query target"))
	}, teatest.WithDuration(mdRunWaitFor))

	// Open search and type 'q' — q must route to the search input, not quit.
	tm.Type("/")
	tm.Type("q")

	// mdRunFinalModel sends Ctrl+C, which is the only key that quits
	// during search. The proof that 'q' was routed correctly is that
	// it landed in the search query and search mode is still active.
	fm := mdRunFinalModel(t, tm)
	if !fm.viewer.Searching() {
		t.Error("viewer should still be in search mode after typing q")
	}
	if got := fm.viewer.Query(); got != "q" {
		t.Errorf("search query = %q, want %q", got, "q")
	}
}

func TestMdViewerExtraHintsAppearInFooter(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# h\n\nbody")
	v.SetSize(80, 20)
	v.SetExtraHints([]string{"q quit"})

	footer := v.Footer(80)
	if !strings.Contains(footer, "q quit") {
		t.Errorf("footer missing extra hint, got %q", footer)
	}
}

func TestMdViewerExtraHintsAbsentDuringSearch(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# h\n\nbody")
	v.SetSize(80, 20)
	v.SetExtraHints([]string{"q quit"})

	// Enter search mode — Footer returns "" while search input is visible
	// in the body, so extra hints must not leak.
	v, _ = v.Update(tea.KeyPressMsg{Code: '/'})
	if got := v.Footer(80); got != "" {
		t.Errorf("footer should be empty during search, got %q", got)
	}
}
