package fspicker

// fspicker_test.go — full-loop teatest coverage for the picker.
//
// The app/ unit tests cover the Provider, Entry rendering, and each
// Action's Run in isolation. Here we drive the real tea.Model returned
// by newModel through the key → Update → View loop and assert on what
// Pick() would return: model.Result(). The browser primitive's own
// navigation/confirm mechanics live in internal/uikit/browser.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/sciminds/cli/internal/tui/fspicker/app"
	"github.com/sciminds/cli/internal/tuitest"
)

const (
	testTermW = 80
	testTermH = 24
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

// startPicker builds a picker rooted at dir and starts a teatest program.
func startPicker(t *testing.T, dir string) *teatest.TestModel {
	t.Helper()
	state := &app.State{}
	m := newModel(app.NewProvider(dir, nil, state), state)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	return tm
}

// Thin aliases over the shared tuitest helpers.
func sendKey(tm *teatest.TestModel, s string)      { tuitest.SendKey(tm, s) }
func sendSpecial(tm *teatest.TestModel, code rune) { tuitest.SendSpecial(tm, code) }
func waitOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	tuitest.WaitFor(t, tm, substr, testWait)
}

// awaitFinal blocks until the picker exits on its own — an action
// returned tea.Quit, or the user pressed a quit key — and returns the
// final model. Unlike tuitest.Final it sends no key, so it never races
// the confirm-modal completion cascade by injecting ctrl+c mid-flight.
func awaitFinal(t *testing.T, tm *teatest.TestModel) model {
	t.Helper()
	return tm.FinalModel(t, teatest.WithFinalTimeout(testFinal)).(model)
}

// seedFile returns a fresh temp dir containing a single named file, so
// the list cursor lands on it deterministically at index 0.
func seedFile(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, name))
	return dir
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// confirmYes answers the open confirm modal affirmatively. Default focus
// is the negative button (safer), so toggle to Yes with "l" — matching
// uikit.HuhKeyMap.Confirm.Toggle — then submit. The Completed cascade is
// driven by the teatest runtime, so no manual draining is needed.
func confirmYes(tm *teatest.TestModel) {
	sendKey(tm, "l")
	sendSpecial(tm, tea.KeyEnter)
}

func TestTeatest_PickFile_ReturnsChosenPath(t *testing.T) {
	dir := seedFile(t, "pick-me.csv")
	tm := startPicker(t, dir)

	waitOutput(t, tm, "pick-me.csv")
	sendKey(tm, "u") // upload action → confirm modal
	waitOutput(t, tm, "Are you sure you want to upload")
	confirmYes(tm) // Run records the path and returns tea.Quit

	fm := awaitFinal(t, tm)
	want := filepath.Join(dir, "pick-me.csv")
	if got := fm.Result(); got.Path != want {
		t.Errorf("Result().Path = %q, want %q", got.Path, want)
	}
	if fm.Result().Force {
		t.Error("Result().Force = true after plain upload; want false")
	}
}

func TestTeatest_ForcePickFile_SetsForce(t *testing.T) {
	dir := seedFile(t, "pick-me.csv")
	tm := startPicker(t, dir)

	waitOutput(t, tm, "pick-me.csv")
	sendKey(tm, "U") // force-upload → confirm modal mentioning overwrite
	waitOutput(t, tm, "force-upload")
	confirmYes(tm)

	fm := awaitFinal(t, tm)
	want := filepath.Join(dir, "pick-me.csv")
	if got := fm.Result(); got.Path != want {
		t.Errorf("Result().Path = %q, want %q", got.Path, want)
	}
	if !fm.Result().Force {
		t.Error("Result().Force = false after force-upload; want true")
	}
}

func TestTeatest_Quit_ReturnsEmptyPath(t *testing.T) {
	dir := seedFile(t, "pick-me.csv")
	tm := startPicker(t, dir)

	waitOutput(t, tm, "pick-me.csv")
	sendKey(tm, "q") // quit key → program exits without picking

	fm := awaitFinal(t, tm)
	if got := fm.Result().Path; got != "" {
		t.Errorf("Result().Path = %q after quit; want empty (ErrCancelled)", got)
	}
}

func TestTeatest_ToggleHidden_RevealsDotfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "visible.txt"))
	writeFile(t, filepath.Join(dir, ".secret"))
	tm := startPicker(t, dir)

	waitOutput(t, tm, "visible.txt")
	// .secret is filtered by default; "." flips ShowHidden and refreshes.
	sendKey(tm, ".")
	waitOutput(t, tm, ".secret")

	sendKey(tm, "q")
	_ = awaitFinal(t, tm)
}
