package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/adrg/xdg"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/lab"
)

// hermeticTransferLog points xdg.StateHome at a fresh temp dir so the
// transfer-log manifest stays per-test.
func hermeticTransferLog(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
}

const (
	testTermW = 80
	testTermH = 24
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

func startTeatest(t *testing.T, b Backend) *teatest.TestModel {
	t.Helper()
	hermeticTransferLog(t)
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	return tm
}

func sendKey(tm *teatest.TestModel, text string) {
	runes := []rune(text)
	if len(runes) == 1 {
		tm.Send(tea.KeyPressMsg{Code: runes[0], Text: text})
		return
	}
	tm.Send(tea.KeyPressMsg{Text: text})
}

func sendSpecial(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

// finalModel sends ctrl+c and returns the final *Model. Reads from the
// returned model are race-free: FinalModel blocks until the program exits
// and its Update goroutine has drained.
func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
	return final.(*Model)
}

// waitOutput blocks until output contains substr. The renderer drains the
// output buffer as it goes, so a single substring may only be matched once
// per test — re-entering the same view (e.g. ascending back to root) needs
// a FinalModel assertion instead.
func waitOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(substr))
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))
}

func TestTeatest_BrowseLoadsRoot(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	_ = finalModel(t, tm)
}

func TestTeatest_DescendIntoDir(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	_ = finalModel(t, tm)
}

func TestTeatest_AscendBackToParent(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyBackspace)
	// Root and data breadcrumbs share the "sciminds" substring; the earlier
	// match already drained those bytes, so assert post-Quit instead.
	fm := finalModel(t, tm)
	if fm.cwd != lab.ReadRoot {
		t.Errorf("cwd = %q, want %q", fm.cwd, lab.ReadRoot)
	}
}

func TestTeatest_ToggleSelectShowsCount(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeySpace)
	waitOutput(t, tm, "[1 selected]")
	_ = finalModel(t, tm)
}

func TestTeatest_ConfirmShowsTotalSize(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter) // descend into data
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown) // cursor on results.csv
	sendSpecial(tm, tea.KeySpace)
	waitOutput(t, tm, "[1 selected]")
	sendKey(tm, "d")
	waitOutput(t, tm, "Total:")
	fm := finalModel(t, tm)
	if fm.totalBytes != 512 {
		t.Errorf("totalBytes = %d, want 512", fm.totalBytes)
	}
}

func TestTeatest_ConfirmCancelReturnsToBrowse(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitOutput(t, tm, "Confirm download")
	sendKey(tm, "n")
	waitOutput(t, tm, "space select")
	_ = finalModel(t, tm)
}

func TestTeatest_TransferErrorPath(t *testing.T) {
	b := sampleBackend()
	b.transferErr = errFake
	tm := startTeatest(t, b)
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitOutput(t, tm, "Total:")
	sendKey(tm, "y")
	waitOutput(t, tm, "Transfer failed")
	sendKey(tm, "q")
	final := tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
	fm := final.(*Model)
	if fm.transferErr == nil {
		t.Error("expected transferErr to be set")
	}
}

func TestTeatest_BannerHiddenWhenNoPending(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	// FinalModel drains the Update goroutine, so the pendingLoadedMsg has
	// been processed by the time we read fm.View().
	fm := finalModel(t, tm)
	out := fm.View().Content
	if bytes.Contains([]byte(out), []byte("interrupted")) {
		t.Errorf("banner shown but no pending; output:\n%s", out)
	}
}

func TestTeatest_BannerShownWithCount(t *testing.T) {
	dir := t.TempDir()
	hermeticTransferLog(t)
	// Two short local files → two pending entries.
	for _, name := range []string{"a", "b"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		_ = lab.LogTransferStarted(lab.TransferEntry{Remote: "/r/" + name, Local: p, ExpectedBytes: 100})
	}
	b := sampleBackend()
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitOutput(t, tm, "2 interrupted")
	_ = finalModel(t, tm)
}

func TestTeatest_PressC_ClearsBanner(t *testing.T) {
	dir := t.TempDir()
	hermeticTransferLog(t)
	short := filepath.Join(dir, "x")
	if err := os.WriteFile(short, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = lab.LogTransferStarted(lab.TransferEntry{Remote: "/r/x", Local: short, ExpectedBytes: 100})

	b := sampleBackend()
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitOutput(t, tm, "1 interrupted")
	sendKey(tm, "c")
	// Wait for the post-clear re-render: loadPendingCmd dispatches a
	// pendingLoadedMsg that drops m.pending to []; the diff rewrites the
	// hint line without the "r resume · c clear" prefix, so "space select"
	// appears again in fresh bytes. This sync-point guarantees the cleared
	// state is in the model by the time we send ctrl+c below.
	waitOutput(t, tm, "space select")
	fm := finalModel(t, tm)
	if pending, _ := lab.PendingTransfers(); len(pending) != 0 {
		t.Errorf("manifest still has entries after clear: %+v", pending)
	}
	out := fm.View().Content
	if bytes.Contains([]byte(out), []byte("interrupted")) {
		t.Errorf("banner still shown after clear; output:\n%s", out)
	}
}

func TestTeatest_PressC_NoOpWhenNoPending(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendKey(tm, "c")
	fm := finalModel(t, tm)
	if fm.screen != screenBrowse {
		t.Errorf("screen = %v, want screenBrowse", fm.screen)
	}
}

func TestTeatest_PressR_NoOpWhenNoPending(t *testing.T) {
	tm := startTeatest(t, sampleBackend())
	waitOutput(t, tm, "sciminds")
	sendKey(tm, "r")
	fm := finalModel(t, tm)
	if fm.screen != screenBrowse {
		t.Errorf("screen = %v, want screenBrowse", fm.screen)
	}
}

func TestTeatest_PressShiftR_RefreshesListing(t *testing.T) {
	b := sampleBackend()
	tm := startTeatest(t, b)
	waitOutput(t, tm, "sciminds")
	beforeCalls := b.ListCallsCount()
	sendKey(tm, "R")
	teatest.WaitFor(t, tm.Output(), func([]byte) bool {
		return b.ListCallsCount() > beforeCalls
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))
	_ = finalModel(t, tm)
}

func TestTeatest_TransferLeavesPendingOnError(t *testing.T) {
	b := sampleBackend()
	b.transferErr = errFake
	tm := startTeatest(t, b)
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitOutput(t, tm, "Total:")
	// Pre-create a short local file so PendingTransfers thinks it's in flight.
	// (Without this, the "local missing" filter would drop it.)
	localFile := "./results.csv"
	if err := os.WriteFile(localFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(localFile) })

	sendKey(tm, "y")
	waitOutput(t, tm, "Transfer failed")

	pending, err := lab.PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 1 || pending[0].Remote != "/labs/sciminds/data/results.csv" {
		t.Errorf("Pending = %+v, want one entry for results.csv", pending)
	}
	sendKey(tm, "q")
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
}
