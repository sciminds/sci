package app

import (
	"context"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/lab"
)

// ── Messages ───────────────────────────────────────────────────────────────

type listLoadedMsg struct {
	path    string
	entries []lab.Entry
	err     error
}

type sizeProbedMsg struct {
	bytes int64
	err   error
}

type transferStartedMsg struct {
	ch     <-chan lab.Progress
	done   <-chan error
	cancel context.CancelFunc
}

type progressMsg struct{ p lab.Progress }

type transferDoneMsg struct{ err error }

type pendingLoadedMsg struct{ entries []lab.TransferEntry }

// ── Cmd factories ──────────────────────────────────────────────────────────

func loadDirCmd(b Backend, p string) tea.Cmd {
	return func() tea.Msg {
		entries, err := b.List(context.Background(), p)
		return listLoadedMsg{path: p, entries: entries, err: err}
	}
}

func probeSizeCmd(b Backend, paths []string) tea.Cmd {
	return func() tea.Msg {
		n, err := b.Size(context.Background(), paths)
		return sizeProbedMsg{bytes: n, err: err}
	}
}

// startTransferCmd kicks off rsync for one remotePath. The returned message
// carries channels the model polls via waitProgressCmd. Cancel() the
// context on screen exit to terminate rsync.
//
// Before launching rsync we probe the remote item's size and append a
// "started" record to the transfer log. On success the model appends a
// matching "done" record (see Update transferDoneMsg). Failed/cancelled
// transfers leave the started record in place so the next browse session
// can offer to resume.
func startTransferCmd(b Backend, remotePath, localDir string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())

		// Per-item size probe (one ssh roundtrip; cheap because the master
		// connection is already warm). Best-effort — if it fails we still
		// log the entry, just without ExpectedBytes.
		var expected int64
		if n, err := b.Size(ctx, []string{remotePath}); err == nil {
			expected = n
		}
		_ = lab.LogTransferStarted(lab.TransferEntry{
			Remote:        remotePath,
			Local:         localDestFor(localDir, remotePath),
			ExpectedBytes: expected,
		})

		ch := make(chan lab.Progress, 8)
		done := make(chan error, 1)
		go func() {
			defer close(ch)
			done <- b.Transfer(ctx, remotePath, localDir, ch)
			close(done)
		}()
		return transferStartedMsg{ch: ch, done: done, cancel: cancel}
	}
}

// localDestFor mirrors what rsync writes when given `alias:remote dest/`:
// the basename of remote, joined onto dest.
func localDestFor(localDir, remotePath string) string {
	return filepath.Join(localDir, filepath.Base(remotePath))
}

// loadPendingCmd reads the transfer manifest off the main goroutine.
// On error we surface an empty slice — the banner is a nice-to-have, not
// load-bearing, so silent failure is better than blocking the TUI.
func loadPendingCmd() tea.Cmd {
	return func() tea.Msg {
		entries, err := lab.PendingTransfers()
		if err != nil {
			entries = []lab.TransferEntry{}
		}
		if entries == nil {
			entries = []lab.TransferEntry{}
		}
		return pendingLoadedMsg{entries: entries}
	}
}

// waitProgressCmd reads the next progress event or transfer completion.
// Returns one msg; the model re-issues this Cmd to keep listening.
//
// Only treats the *closure* of ch as completion. The producer goroutine
// always closes ch after Transfer returns (and after pushing final progress
// frames), so this drains every emitted frame before surfacing
// transferDoneMsg. Selecting on done directly would race against buffered
// progress frames and let the UI miss the final 100% tick.
func waitProgressCmd(ch <-chan lab.Progress, done <-chan error) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if ok {
			return progressMsg{p: p}
		}
		return transferDoneMsg{err: <-done}
	}
}
