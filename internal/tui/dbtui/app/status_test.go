package app

// status_test.go — status-line lifecycle: info messages retire themselves
// after a delay so the default hint line comes back; errors stick around.

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// 1. A tick carrying the current sequence retires an info message.
func TestStatusInfoExpiresOnMatchingTick(t *testing.T) {
	m := makeYankModel([][]string{{"1", "alice"}})
	m.setStatusInfo("Copied cell (5 chars)")

	m.Update(statusExpiredMsg{Seq: m.statusSeq})

	if m.status.Text != "" {
		t.Errorf("status = %q, want it cleared", m.status.Text)
	}
}

//  2. A tick from a superseded message must not retire the current one —
//     otherwise a fast second copy would flash away early.
func TestStatusStaleTickIgnored(t *testing.T) {
	m := makeYankModel([][]string{{"1", "alice"}})
	m.setStatusInfo("first")
	stale := m.statusSeq
	m.setStatusInfo("second")

	m.Update(statusExpiredMsg{Seq: stale})

	if m.status.Text != "second" {
		t.Errorf("status = %q, want %q (stale tick must not clear it)", m.status.Text, "second")
	}
}

// 3. Errors persist — the user dismisses them, not a timer.
func TestStatusErrorDoesNotExpire(t *testing.T) {
	m := makeYankModel([][]string{{"1", "alice"}})
	m.setStatusError("Copy failed: no clipboard tool")

	m.Update(statusExpiredMsg{Seq: m.statusSeq})

	if m.status.Text == "" {
		t.Error("error status was cleared by the expiry tick; want it to persist")
	}
}

//  4. Clearing bumps the sequence, so a timer still in flight for the cleared
//     message can't retire whatever replaces it.
func TestClearStatusBumpsSequence(t *testing.T) {
	m := makeYankModel([][]string{{"1", "alice"}})
	m.setStatusInfo("pinned")
	before := m.statusSeq
	m.clearStatus()

	if m.statusSeq == before {
		t.Error("clearStatus did not bump statusSeq")
	}
}

// 5. The scheduled tick carries the sequence it was created with.
func TestScheduleStatusExpiryPayload(t *testing.T) {
	msg := scheduleStatusExpiry(7, time.Millisecond)()

	expired, ok := msg.(statusExpiredMsg)
	if !ok {
		t.Fatalf("tick produced %T, want statusExpiredMsg", msg)
	}
	if expired.Seq != 7 {
		t.Errorf("Seq = %d, want 7", expired.Seq)
	}
}

//  6. Posting an info message through the real key loop schedules the retiring
//     tick; a key that posts nothing schedules nothing.
func TestUpdateSchedulesStatusExpiry(t *testing.T) {
	_ = captureClipboard(t)

	m := makeYankModel([][]string{{"1", "alice"}})
	m.tabs[0].ColCursor = 1

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y posted a status message but scheduled no expiry tick")
	}

	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); cmd != nil {
		t.Error("j posts no status message; want no expiry tick scheduled")
	}
}
