package app

import (
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
		return m, waitProgressCmd(m.activeCh, m.activeDone)

	case transferDoneMsg:
		m.activeCh = nil
		m.activeDone = nil
		m.activeCancel = nil
		if msg.err != nil {
			m.transferErr = msg.err
			m.screen = screenError
			return m, nil
		}
		m.transferred++
		m.queueIdx++
		if m.queueIdx >= len(m.queue) {
			m.screen = screenDone
			return m, nil
		}
		m.progress = lab.Progress{}
		return m, startTransferCmd(m.backend, m.queue[m.queueIdx], ".")

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == 'c' && msg.Mod == tea.ModCtrl {
		if m.activeCancel != nil {
			m.activeCancel()
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
		delete(m.cache, m.cwd)
		return m, m.reloadCmd()
	case "q":
		return m, tea.Quit
	case "d":
		if len(m.selected) == 0 {
			if m.cursor < len(m.entries) {
				m.selectPath(m.pathFor(m.entries[m.cursor]))
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
		m.queue = m.SelectedPaths()
		m.queueIdx = 0
		m.transferred = 0
		m.transferErr = nil
		m.screen = screenTransfer
		m.progress = lab.Progress{}
		return m, startTransferCmd(m.backend, m.queue[0], ".")
	case "n":
		m.screen = screenBrowse
		return m, nil
	}
	if msg.Code == tea.KeyEsc {
		m.screen = screenBrowse
		return m, nil
	}
	return m, nil
}

func (m *Model) keyTransfer(_ tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Transfer screen is locked down — only ctrl+c (handled above) cancels.
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
