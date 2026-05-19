//go:build !race

// Transfer-progress tests are excluded from the -race build because
// charm.land/bubbles/v2/progress's nextFrame() closure captures the
// progress.Model pointer and reads m.id / m.tag concurrently with
// SetPercent's write to m.tag. That race lives in upstream bubbles
// (v2.1.0, the latest stable), not in our test code. Until upstream
// snapshots those fields before scheduling the Tick, gate these tests
// out of `just test-race` while still running them on `just test`.

package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/lab"
)

func TestTeatest_TransferCompletesQueue(t *testing.T) {
	b := sampleBackend()
	b.progressFrames = []lab.Progress{
		{Bytes: 100, Percent: 20, Rate: "1MB/s", ETA: "0:00:04"},
		{Bytes: 512, Percent: 100, Rate: "1MB/s", ETA: "0:00:00"},
	}
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
	waitOutput(t, tm, "Done")
	sendKey(tm, "q")
	final := tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
	fm := final.(*Model)
	if fm.transferred != 1 {
		t.Errorf("transferred = %d, want 1", fm.transferred)
	}
	if got := b.TransferCalls(); len(got) != 1 || got[0] != "/labs/sciminds/data/results.csv" {
		t.Errorf("transferCalls = %v, want 1 call to results.csv", got)
	}
}

func TestTeatest_TransferWritesManifestStartAndDone(t *testing.T) {
	b := sampleBackend()
	b.progressFrames = []lab.Progress{
		{Bytes: 512, Percent: 100, Rate: "1MB/s", ETA: "0:00:00"},
	}
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
	waitOutput(t, tm, "Done")

	pending, err := lab.PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty after successful transfer", pending)
	}
	sendKey(tm, "q")
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
}

func TestTeatest_PressR_ResumesPending(t *testing.T) {
	dir := t.TempDir()
	hermeticTransferLog(t)
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
	waitOutput(t, tm, "1 interrupted")
	sendKey(tm, "r")
	waitOutput(t, tm, "Done")
	if got := b.TransferCalls(); len(got) != 1 || got[0] != "/labs/sciminds/data/results.csv" {
		t.Errorf("transferCalls = %v, want 1 call to results.csv", got)
	}
	sendKey(tm, "q")
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(testFinal))
}

func TestTeatest_TransferQuitWithQ_LeavesPending(t *testing.T) {
	hermeticTransferLog(t)
	b := sampleBackend()
	for i := 0; i < 50; i++ {
		b.progressFrames = append(b.progressFrames, lab.Progress{Bytes: int64(i), Percent: i, Rate: "1MB/s", ETA: "0:01:00"})
	}
	// Hold the transfer in-flight until ctx cancellation so `q` reliably
	// hits while screen=screenTransfer. Frame-count alone is unreliable: the
	// buffered progress channel lets the producer race to completion.
	b.holdUntilCancel = true
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitOutput(t, tm, "Total:")
	// Pre-create a short local file so the entry counts as resumable later.
	if err := os.WriteFile("./results.csv", []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove("./results.csv") })
	sendKey(tm, "y")
	waitOutput(t, tm, "Downloading")
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
	hermeticTransferLog(t)
	b := sampleBackend()
	for i := 0; i < 50; i++ {
		b.progressFrames = append(b.progressFrames, lab.Progress{Bytes: int64(i), Percent: i, Rate: "1MB/s", ETA: "0:01:00"})
	}
	// Hold the transfer in-flight until ctx cancellation so the test can
	// reliably observe screen=screenTransfer before sending ctrl+c. Without
	// this, the fake races through frames → completion between polling ticks
	// and the test would "pass" via natural completion rather than verifying
	// that ctrl+c actually drops the partial.
	b.holdUntilCancel = true
	m := NewModel(&lab.Config{User: "alice"}, b)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	waitOutput(t, tm, "sciminds")
	sendSpecial(tm, tea.KeyEnter)
	waitOutput(t, tm, "results.csv")
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeyDown)
	sendSpecial(tm, tea.KeySpace)
	sendKey(tm, "d")
	waitOutput(t, tm, "Total:")
	if err := os.WriteFile("./results.csv", []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove("./results.csv") })
	sendKey(tm, "y")
	waitOutput(t, tm, "Downloading")
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
