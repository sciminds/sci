package app

import (
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/lab"
)

// Update implements tea.Model. Dispatch by screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		barW := m.width - 20
		if barW > 60 {
			barW = 60
		}
		if barW < 10 {
			barW = 10
		}
		m.progressBar.SetWidth(barW)
		return m, nil

	case listLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.cache[msg.path] = msg.entries
		if msg.path == m.cwd {
			m.entries = msg.entries
			if m.cursor >= len(m.entries) {
				m.cursor = 0
			}
		}
		return m, nil

	case pendingLoadedMsg:
		m.pending = msg.entries
		return m, nil

	case sizeProbedMsg:
		m.sizeProbing = false
		m.sizeErr = msg.err
		m.totalBytes = msg.bytes
		return m, nil

	case transferStartedMsg:
		m.activeCh = msg.ch
		m.activeDone = msg.done
		m.activeCancel = msg.cancel
		return m, waitProgressCmd(m.activeCh, m.activeDone)

	case progressMsg:
		m.progress = msg.p
		setCmd := m.progressBar.SetPercent(float64(msg.p.Percent) / 100.0)
		return m, tea.Batch(setCmd, waitProgressCmd(m.activeCh, m.activeDone))

	case progress.FrameMsg:
		pm, cmd := m.progressBar.Update(msg)
		m.progressBar = pm
		return m, cmd

	case transferDoneMsg:
		m.activeCh = nil
		m.activeDone = nil
		m.activeCancel = nil
		if msg.err != nil {
			m.transferErr = msg.err
			m.screen = screenError
			return m, nil
		}
		// Manifest: mark the just-finished item complete so the next browse
		// session won't offer to resume it.
		_ = lab.LogTransferDone(m.queue[m.queueIdx])
		m.transferred++
		m.queueIdx++
		if m.queueIdx >= len(m.queue) {
			m.screen = screenDone
			// Reload pending so an immediate jump back to browse doesn't
			// show a stale banner for the items we just finished.
			return m, loadPendingCmd()
		}
		m.progress = lab.Progress{}
		resetCmd := m.progressBar.SetPercent(0)
		return m, tea.Batch(resetCmd, startTransferCmd(m.backend, m.queue[m.queueIdx], "."))

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == 'c' && msg.Mod == tea.ModCtrl {
		// Hard abort: if a transfer is in flight, mark it done in the manifest
		// so the next browse session won't offer to resume the partial. (Use
		// `q` on the transfer screen instead to keep the partial resumable.)
		if m.activeCancel != nil {
			m.activeCancel()
		}
		if m.screen == screenTransfer && m.queueIdx < len(m.queue) {
			_ = lab.LogTransferDone(m.queue[m.queueIdx])
		}
		return m, tea.Quit
	}
	switch m.screen {
	case screenBrowse:
		return m.keyBrowse(msg)
	case screenConfirm:
		return m.keyConfirm(msg)
	case screenTransfer:
		return m.keyTransfer(msg)
	case screenError:
		return m.keyError(msg)
	case screenDone:
		return m.keyDone(msg)
	}
	return m, nil
}

func (m *Model) keyBrowse(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Code {
	case tea.KeyDown:
		if m.cursor+1 < len(m.entries) {
			m.cursor++
		}
		return m, nil
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case tea.KeyEnter, tea.KeyRight:
		if cmd, ok := m.descendIfDir(); ok {
			return m, cmd
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyLeft:
		m.ascend()
		return m, m.reloadCmd()
	case tea.KeySpace:
		m.toggleSelectAtCursor()
		return m, nil
	}
	switch msg.Text {
	case "j":
		if m.cursor+1 < len(m.entries) {
			m.cursor++
		}
	case "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "h":
		m.ascend()
		return m, m.reloadCmd()
	case "l":
		if cmd, ok := m.descendIfDir(); ok {
			return m, cmd
		}
	case "r":
		// Resume any pending transfers from prior sessions. No-op when the
		// banner is hidden (no entries) so the keypress is silently absorbed.
		if len(m.pending) == 0 {
			return m, nil
		}
		m.queue = make([]string, len(m.pending))
		for i, e := range m.pending {
			m.queue[i] = e.Remote
		}
		m.queueIdx = 0
		m.transferred = 0
		m.transferErr = nil
		m.screen = screenTransfer
		m.progress = lab.Progress{}
		return m, startTransferCmd(m.backend, m.queue[0], ".")
	case "R":
		// Refresh the current directory listing.
		delete(m.cache, m.cwd)
		return m, m.reloadCmd()
	case "c":
		// Clear all resumable downloads. No-op when the banner is hidden.
		if len(m.pending) == 0 {
			return m, nil
		}
		_ = lab.ClearTransferLog()
		return m, loadPendingCmd()
	case "q":
		return m, tea.Quit
	case "d":
		if len(m.selected) == 0 {
			if m.cursor < len(m.entries) {
				p := m.pathFor(m.entries[m.cursor])
				m.selectPath(p)
				m.implicitSel = p
			}
		}
		if len(m.selected) == 0 {
			return m, nil
		}
		m.screen = screenConfirm
		m.sizeProbing = true
		m.totalBytes = 0
		m.sizeErr = nil
		return m, probeSizeCmd(m.backend, m.SelectedPaths())
	}
	return m, nil
}

func (m *Model) keyConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Text {
	case "y":
		if m.sizeProbing {
			return m, nil
		}
		// User confirmed — implicit pick is now committed; forget it so a
		// later cancel-from-confirm doesn't try to remove an already-acted path.
		m.implicitSel = ""
		m.queue = m.SelectedPaths()
		m.queueIdx = 0
		m.transferred = 0
		m.transferErr = nil
		m.screen = screenTransfer
		m.progress = lab.Progress{}
		return m, startTransferCmd(m.backend, m.queue[0], ".")
	case "n":
		m.cancelConfirm()
		return m, nil
	}
	if msg.Code == tea.KeyEsc {
		m.cancelConfirm()
		return m, nil
	}
	return m, nil
}

// cancelConfirm returns to browse and discards an implicit cursor selection.
// Explicit (space-toggled) picks survive so the user doesn't re-select after
// peeking at the size estimate and choosing not to download yet.
func (m *Model) cancelConfirm() {
	if m.implicitSel != "" {
		delete(m.selected, m.implicitSel)
		m.implicitSel = ""
	}
	m.screen = screenBrowse
}

func (m *Model) keyTransfer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Soft quit: cancel the in-flight rsync but leave the manifest entry in
	// place so the next browse session offers to resume it. Use ctrl+c
	// (handled in handleKey) to abort *and* drop the partial from the
	// resume list.
	if msg.Text == "q" {
		if m.activeCancel != nil {
			m.activeCancel()
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) keyError(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Text {
	case "r":
		m.transferErr = nil
		m.screen = screenTransfer
		m.progress = lab.Progress{}
		return m, startTransferCmd(m.backend, m.queue[m.queueIdx], ".")
	case "s":
		m.transferErr = nil
		m.queueIdx++
		if m.queueIdx >= len(m.queue) {
			m.screen = screenDone
			return m, nil
		}
		m.screen = screenTransfer
		m.progress = lab.Progress{}
		return m, startTransferCmd(m.backend, m.queue[m.queueIdx], ".")
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) keyDone(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyEnter || msg.Code == tea.KeyEsc {
		return m, tea.Quit
	}
	if msg.Text == "q" {
		return m, tea.Quit
	}
	return m, nil
}
