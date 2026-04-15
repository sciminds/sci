package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/lab"
)

const (
	testTermW = 80
	testTermH = 24
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

func startTeatest(t *testing.T, b Backend) (*teatest.TestModel, *Model) {
	t.Helper()
	// Each test gets its own transfer-log file so the manifest stays hermetic.
	t.Setenv("SCI_LAB_TRANSFER_LOG", t.TempDir()+"/transfers.jsonl")
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	return tm, m
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

// finalModel sends ctrl+c and returns the final *Model.
func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
	return final.(*Model)
}

// waitOutput blocks until output contains substr.
func waitOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(substr))
	}, teatest.WithDuration(testWait), teatest.WithCheckInterval(time.Millisecond))
}

// waitFor blocks until pred(model) returns true. Polls model state directly
// via a ticker (avoids relying on the cursed renderer flushing output for
// every state change, which it doesn't always do for rapid transitions).
func waitFor(t *testing.T, m *Model, what string, pred func(*Model) bool) {
	t.Helper()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(testWait)
	for {
		if pred(m) {
			return
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("waitFor %s: condition not met after %s (screen=%d cwd=%s entries=%d)",
				what, testWait, m.screen, m.cwd, len(m.entries))
		}
	}
}

func TestTeatest_BrowseLoadsRoot(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root listing", func(m *Model) bool {
		return len(m.entries) == 3
	})
	_ = finalModel(t, tm)
}

func TestTeatest_DescendIntoDir(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root listing", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir loaded", func(m *Model) bool {
		return m.cwd == "/labs/sciminds/data" && len(m.entries) == 3
	})
	_ = finalModel(t, tm)
}

func TestTeatest_AscendBackToParent(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" })
	sendSpecial(tm, tea.KeyBackspace)
	waitFor(t, m, "back at root", func(m *Model) bool { return m.cwd == lab.ReadRoot })
	_ = finalModel(t, tm)
}

func TestTeatest_ToggleSelectShowsCount(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeySpace)
	waitFor(t, m, "1 selected", func(m *Model) bool { return m.SelectedCount() == 1 })
	_ = finalModel(t, tm)
}

func TestTeatest_ConfirmShowsTotalSize(t *testing.T) {
	b := sampleBackend()
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter) // descend into data
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" && len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown) // cursor on results.csv
	sendSpecial(tm, tea.KeySpace)
	waitFor(t, m, "1 selected", func(m *Model) bool { return m.SelectedCount() == 1 })
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool {
		return m.screen == screenConfirm && !m.sizeProbing
	})
	if m.totalBytes != 512 {
		t.Errorf("totalBytes = %d, want 512", m.totalBytes)
	}
	_ = finalModel(t, tm)
}

func TestTeatest_ConfirmCancelReturnsToBrowse(t *testing.T) {
	b := sampleBackend()
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" && len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "confirm", func(m *Model) bool { return m.screen == screenConfirm })
	sendKey(tm, "n")
	waitFor(t, m, "back to browse", func(m *Model) bool { return m.screen == screenBrowse })
	_ = finalModel(t, tm)
}

func TestTeatest_TransferCompletesQueue(t *testing.T) {
	b := sampleBackend()
	b.progressFrames = []lab.Progress{
		{Bytes: 100, Percent: 20, Rate: "1MB/s", ETA: "0:00:04"},
		{Bytes: 512, Percent: 100, Rate: "1MB/s", ETA: "0:00:00"},
	}
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" && len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool {
		return m.screen == screenConfirm && !m.sizeProbing
	})
	sendKey(tm, "y")
	waitFor(t, m, "transfer done", func(m *Model) bool { return m.screen == screenDone })
	if m.transferred != 1 {
		t.Errorf("transferred = %d, want 1", m.transferred)
	}
	if got := b.transferCalls; len(got) != 1 || got[0] != "/labs/sciminds/data/results.csv" {
		t.Errorf("transferCalls = %v, want 1 call to results.csv", got)
	}
}

func TestTeatest_TransferErrorPath(t *testing.T) {
	b := sampleBackend()
	b.transferErr = errFake
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" && len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool {
		return m.screen == screenConfirm && !m.sizeProbing
	})
	sendKey(tm, "y")
	waitFor(t, m, "error screen", func(m *Model) bool { return m.screen == screenError })
	if m.transferErr == nil {
		t.Error("expected transferErr to be set")
	}
	sendKey(tm, "q")
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
}

// (silence unused waitOutput when not needed; keep available for future use)
var _ = waitOutput

func TestTeatest_TransferWritesManifestStartAndDone(t *testing.T) {
	b := sampleBackend()
	b.progressFrames = []lab.Progress{
		{Bytes: 512, Percent: 100, Rate: "1MB/s", ETA: "0:00:00"},
	}
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool {
		return m.screen == screenConfirm && !m.sizeProbing
	})
	sendKey(tm, "y")
	waitFor(t, m, "transfer done", func(m *Model) bool { return m.screen == screenDone })

	pending, err := lab.PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty after successful transfer", pending)
	}
}

func TestTeatest_BannerHiddenWhenNoPending(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	waitFor(t, m, "pending loaded", func(m *Model) bool { return m.pending != nil })
	out := m.View().Content
	if bytes.Contains([]byte(out), []byte("interrupted")) {
		t.Errorf("banner shown but no pending; output:\n%s", out)
	}
	_ = finalModel(t, tm)
}

func TestTeatest_BannerShownWithCount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SCI_LAB_TRANSFER_LOG", filepath.Join(dir, "transfers.jsonl"))
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
	waitFor(t, m, "pending loaded", func(m *Model) bool { return len(m.pending) == 2 })
	out := m.View().Content
	if !bytes.Contains([]byte(out), []byte("2 interrupted")) {
		t.Errorf("expected banner with '2 interrupted'; output:\n%s", out)
	}
	_ = finalModel(t, tm)
}

func TestTeatest_PressR_ResumesPending(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SCI_LAB_TRANSFER_LOG", filepath.Join(dir, "transfers.jsonl"))
	short := filepath.Join(dir, "results.csv")
	if err := os.WriteFile(short, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = lab.LogTransferStarted(lab.TransferEntry{
		Remote: "/labs/sciminds/data/results.csv", Local: short, ExpectedBytes: 512,
	})
	b := sampleBackend()
	b.progressFrames = []lab.Progress{{Bytes: 512, Percent: 100, Rate: "1MB/s", ETA: "0:00:00"}}
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitFor(t, m, "pending loaded", func(m *Model) bool { return len(m.pending) == 1 })
	sendKey(tm, "r")
	waitFor(t, m, "transfer done", func(m *Model) bool { return m.screen == screenDone })
	if got := b.transferCalls; len(got) != 1 || got[0] != "/labs/sciminds/data/results.csv" {
		t.Errorf("transferCalls = %v, want 1 call to results.csv", got)
	}
}

func TestTeatest_PressC_ClearsBanner(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SCI_LAB_TRANSFER_LOG", filepath.Join(dir, "transfers.jsonl"))
	short := filepath.Join(dir, "x")
	if err := os.WriteFile(short, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = lab.LogTransferStarted(lab.TransferEntry{Remote: "/r/x", Local: short, ExpectedBytes: 100})

	b := sampleBackend()
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitFor(t, m, "pending loaded", func(m *Model) bool { return len(m.pending) == 1 })
	sendKey(tm, "c")
	waitFor(t, m, "pending cleared", func(m *Model) bool { return len(m.pending) == 0 })
	if pending, _ := lab.PendingTransfers(); len(pending) != 0 {
		t.Errorf("manifest still has entries after clear: %+v", pending)
	}
	out := m.View().Content
	if bytes.Contains([]byte(out), []byte("interrupted")) {
		t.Errorf("banner still shown after clear; output:\n%s", out)
	}
	_ = finalModel(t, tm)
}

func TestTeatest_PressC_NoOpWhenNoPending(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	waitFor(t, m, "pending loaded", func(m *Model) bool { return m.pending != nil })
	sendKey(tm, "c")
	waitFor(t, m, "still browse", func(m *Model) bool { return m.screen == screenBrowse })
	_ = finalModel(t, tm)
}

func TestTeatest_PressR_NoOpWhenNoPending(t *testing.T) {
	tm, m := startTeatest(t, sampleBackend())
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	waitFor(t, m, "pending loaded", func(m *Model) bool { return m.pending != nil })
	sendKey(tm, "r")
	// Give the model a beat to react; screen should remain on browse.
	waitFor(t, m, "still browse", func(m *Model) bool { return m.screen == screenBrowse })
	if m.screen != screenBrowse {
		t.Errorf("screen = %v, want screenBrowse", m.screen)
	}
	_ = finalModel(t, tm)
}

func TestTeatest_PressShiftR_RefreshesListing(t *testing.T) {
	b := sampleBackend()
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	beforeCalls := len(b.listCalls)
	sendKey(tm, "R")
	waitFor(t, m, "refresh issued", func(m *Model) bool { return len(b.listCalls) > beforeCalls })
	_ = finalModel(t, tm)
}

func TestTeatest_TransferQuitWithQ_LeavesPending(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SCI_LAB_TRANSFER_LOG", filepath.Join(dir, "transfers.jsonl"))
	b := sampleBackend()
	// Many frames so the transfer doesn't auto-complete before we send `q`.
	for i := 0; i < 50; i++ {
		b.progressFrames = append(b.progressFrames, lab.Progress{Bytes: int64(i), Percent: i, Rate: "1MB/s", ETA: "0:01:00"})
	}
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool { return m.screen == screenConfirm && !m.sizeProbing })
	// Pre-create a short local file so the entry counts as resumable later.
	if err := os.WriteFile("./results.csv", []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove("./results.csv") })
	sendKey(tm, "y")
	waitFor(t, m, "transfer started", func(m *Model) bool { return m.screen == screenTransfer && m.activeCancel != nil })
	sendKey(tm, "q")
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))

	pending, err := lab.PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 1 || pending[0].Remote != "/labs/sciminds/data/results.csv" {
		t.Errorf("Pending = %+v, want one entry for results.csv (q should leave it pending)", pending)
	}
}

func TestTeatest_TransferQuitWithCtrlC_DropsPending(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SCI_LAB_TRANSFER_LOG", filepath.Join(dir, "transfers.jsonl"))
	b := sampleBackend()
	for i := 0; i < 50; i++ {
		b.progressFrames = append(b.progressFrames, lab.Progress{Bytes: int64(i), Percent: i, Rate: "1MB/s", ETA: "0:01:00"})
	}
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool { return m.screen == screenConfirm && !m.sizeProbing })
	if err := os.WriteFile("./results.csv", []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove("./results.csv") })
	sendKey(tm, "y")
	waitFor(t, m, "transfer started", func(m *Model) bool { return m.screen == screenTransfer && m.activeCancel != nil })
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))

	pending, err := lab.PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty (ctrl-c should drop the partial)", pending)
	}
}

func TestTeatest_TransferLeavesPendingOnError(t *testing.T) {
	b := sampleBackend()
	b.transferErr = errFake
	tm, m := startTeatest(t, b)
	waitFor(t, m, "root", func(m *Model) bool { return len(m.entries) == 3 })
	sendSpecial(tm, tea.KeyEnter)
	waitFor(t, m, "data dir", func(m *Model) bool { return m.cwd == "/labs/sciminds/data" })
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitFor(t, m, "size probed", func(m *Model) bool {
		return m.screen == screenConfirm && !m.sizeProbing
	})
	// Pre-create a short local file so PendingTransfers thinks it's in flight.
	// (Without this, the "local missing" filter would drop it.)
	localFile := "./results.csv"
	if err := os.WriteFile(localFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(localFile) })

	sendKey(tm, "y")
	waitFor(t, m, "error screen", func(m *Model) bool { return m.screen == screenError })

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
