package app

import (
	"context"

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
func startTransferCmd(b Backend, remotePath, localDir string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
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

// waitProgressCmd reads the next progress event or transfer completion.
// Returns one msg; the model re-issues this Cmd to keep listening.
func waitProgressCmd(ch <-chan lab.Progress, done <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-ch:
			if !ok {
				// channel closed; wait for done error
				err := <-done
				return transferDoneMsg{err: err}
			}
			return progressMsg{p: p}
		case err := <-done:
			return transferDoneMsg{err: err}
		}
	}
}
