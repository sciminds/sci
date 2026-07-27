package app

// status.go — the status line's lifecycle. Info messages ("Copied cell",
// "Pinned: …") are notifications: they retire themselves after
// [statusInfoTTL] so the default hint line comes back without the user
// pressing esc. Errors are not on a timer — they stay until the user acts
// or another message replaces them.
//
// The mechanism mirrors the debounced FTS tick in search.go: every message
// carries a sequence number, and a tick only clears the message it was
// scheduled for. Without that guard, copying twice in quick succession
// would let the first tick retire the second message early.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// statusInfoTTL is how long an info message stays on screen. Long enough to
// read a short notification, short enough that the hint line isn't hidden
// while the user thinks about the next key.
const statusInfoTTL = 4 * time.Second

// statusExpiredMsg asks the model to retire the status message identified by
// Seq. Stale ticks (Seq behind the model's) are ignored.
type statusExpiredMsg struct {
	Seq int
}

func (m *Model) setStatusInfo(text string) {
	m.status = statusMsg{Text: text, Kind: statusInfo}
	m.statusSeq++
}

func (m *Model) setStatusError(text string) {
	m.status = statusMsg{Text: text, Kind: statusError}
	m.statusSeq++
}

// clearStatus removes the status message immediately (esc, and the expiry
// tick). It bumps the sequence too, so a tick still in flight for the
// message being cleared can't retire whatever is posted next.
func (m *Model) clearStatus() {
	m.status = statusMsg{}
	m.statusSeq++
}

// scheduleStatusExpiry returns a command that retires message seq after d.
func scheduleStatusExpiry(seq int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return statusExpiredMsg{Seq: seq}
	})
}

// handleStatusExpired clears the status line if the tick is for the message
// currently on screen. Errors are left alone.
func (m *Model) handleStatusExpired(msg statusExpiredMsg) {
	if msg.Seq != m.statusSeq || m.status.Kind != statusInfo {
		return
	}
	m.clearStatus()
}

// statusExpiryCmd returns the tick that retires the message posted during
// this update, or nil when nothing expirable was posted. prevSeq is the
// sequence from before the update ran.
func (m *Model) statusExpiryCmd(prevSeq int) tea.Cmd {
	if m.statusSeq == prevSeq || m.status.Text == "" || m.status.Kind != statusInfo {
		return nil
	}
	return scheduleStatusExpiry(m.statusSeq, statusInfoTTL)
}
