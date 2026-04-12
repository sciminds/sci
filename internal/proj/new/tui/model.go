// Package tui implements the interactive Bubble Tea model for `sci new`.
//
// It presents a multi-select list of project templates, runs the selected
// scaffolding operations, and displays a summary when done.
package tui

import (
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	projnew "github.com/sciminds/cli/internal/proj/new"
	"github.com/sciminds/cli/internal/ui"
)

// ── Phase ────────────────────────────────────────────────────────────────────

type phase int

const (
	phaseSelecting phase = iota
	phaseApplying
	phaseDone
)

// ── Messages ─────────────────────────────────────────────────────────────────

type applyDoneMsg struct {
	err error
}

// ── File entry ───────────────────────────────────────────────────────────────

type fileEntry struct {
	file    projnew.ConfigFile
	applied bool
}

// SelectTitle implements ui.SelectItem.
func (f fileEntry) SelectTitle() string { return f.file.Path }

func (f fileEntry) statusLabel() string {
	if f.file.Changed && f.file.Exists {
		return "overwrite"
	}
	if f.file.Changed {
		return "create"
	}
	return "up to date"
}

// ── Key maps ─────────────────────────────────────────────────────────────────

type doneKeys struct{}

func (k doneKeys) ShortHelp() []key.Binding {
	return []key.Binding{ui.BindEnter, ui.BindQuit}
}
func (k doneKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// ── Model ────────────────────────────────────────────────────────────────────

// Model is the Bubble Tea model for the proj config TUI.
type Model struct {
	phase   phase
	width   int
	height  int
	help    help.Model
	spinner spinner.Model

	selectList ui.SelectList
	dir        string
	files      []fileEntry

	Result projnew.SyncResult
	Err    error
}

// Options configures the proj config TUI.
type Options struct {
	Dir   string
	Files []projnew.ConfigFile
}

// New creates a new proj config TUI model.
func New(opts Options) Model {
	files := make([]fileEntry, len(opts.Files))
	items := make([]ui.SelectItem, len(opts.Files))
	selected := make([]bool, len(opts.Files))
	for i, f := range opts.Files {
		files[i] = fileEntry{file: f}
		items[i] = files[i]
		selected[i] = f.Changed
	}

	sl := ui.NewSelectList(items,
		ui.WithHeading("Select config files to apply"),
		ui.WithSelected(selected),
		ui.WithRenderItem(renderFileItem),
	)

	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(ui.TUI.SpinnerDot()),
	)

	return Model{
		phase:      phaseSelecting,
		help:       ui.NewHelp(),
		spinner:    s,
		selectList: sl,
		dir:        opts.Dir,
		files:      files,
	}
}

func renderFileItem(item ui.SelectItem, selected, isCursor bool) string {
	f := item.(fileEntry)
	line := ui.RenderSelectItemLine(f.file.Path, selected, isCursor)
	statusStr := ui.TUI.Dim().Render("(" + f.statusLabel() + ")")
	return line + "  " + statusStr
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return nil
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(ui.ContentWidth(msg.Width))
		m.selectList.SetWidth(ui.ContentWidth(msg.Width))
		return m, nil

	case applyDoneMsg:
		if msg.err != nil {
			m.Err = msg.err
		}
		// Build result from applied files
		m.Result = projnew.SyncResult{Dir: m.dir}
		for _, f := range m.files {
			if f.applied {
				m.Result.Changed = append(m.Result.Changed, projnew.SyncChange{
					Path:    f.file.Path,
					Changed: true,
					Exists:  f.file.Exists,
				})
			}
		}
		m.phase = phaseDone
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	if m.phase == phaseApplying {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case ui.KeyQ, ui.KeyCtrlC, ui.KeyEsc:
		return m, tea.Quit
	}

	switch m.phase {
	case phaseSelecting:
		var cmd tea.Cmd
		m.selectList, cmd = m.selectList.Update(msg)
		if m.selectList.IsConfirmed() {
			m.phase = phaseApplying
			return m, tea.Batch(m.spinner.Tick, m.applySelected())
		}
		return m, cmd
	case phaseDone:
		if msg.String() == ui.KeyEnter {
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *Model) applySelected() tea.Cmd {
	var selected []projnew.ConfigFile
	for i, f := range m.files {
		if m.selectList.IsSelected(i) {
			selected = append(selected, f.file)
			m.files[i].applied = true
		}
	}

	dir := m.dir
	return func() tea.Msg {
		err := projnew.ApplyConfigFiles(dir, selected)
		return applyDoneMsg{err: err}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (m Model) footerLeft() string {
	switch m.phase {
	case phaseSelecting:
		return fmt.Sprintf("%d selected", m.selectList.SelectedCount())
	case phaseDone:
		if m.Err != nil {
			return "Error"
		}
		return "Done"
	}
	return ""
}

func (m Model) footerRight() string {
	var km help.KeyMap
	switch m.phase {
	case phaseSelecting:
		km = ui.NewSelectListKeys()
	case phaseDone:
		km = doneKeys{}
	}
	if km == nil {
		return ""
	}
	return m.help.View(km)
}
