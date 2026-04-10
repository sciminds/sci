package ui

// spinner.go — bubbletea inline spinner for long-running operations.
// Replaces the old manual ANSI tick-renderer with a proper bubbletea program.

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Messages ────────────────────────────────────────────────────────────────

type statusMsg string
type doneMsg struct{ err error }

// ── Model ───────────────────────────────────────────────────────────────────

type spinnerModel struct {
	spinner spinner.Model
	title   string
	status  string
	done    bool
	err     error
}

func newSpinnerModel(title string) spinnerModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#56B4E9"))),
	)
	return spinnerModel{spinner: s, title: title}
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		m.status = string(msg)
		return m, nil
	case doneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.done = true
			m.err = fmt.Errorf("interrupted")
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m spinnerModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	line := m.spinner.View() + TUI.FgAccent().Render(m.title)
	if m.status != "" {
		line += TUI.Dim().Render("  " + m.status)
	}
	return tea.NewView(line + "\n")
}

// ── Public API ──────────────────────────────────────────────────────────────

// RunWithSpinner shows an inline spinner while fn runs. Returns fn's error.
// In quiet mode, prints the title to stderr and skips the TUI.
func RunWithSpinner(title string, fn func() error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn()
	}

	m := newSpinnerModel(title)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	go func() {
		err := fn()
		p.Send(doneMsg{err: err})
	}()

	result, runErr := p.Run()
	if runErr != nil {
		return runErr
	}
	return result.(spinnerModel).err
}

// RunWithSpinnerStatus shows an inline spinner while fn runs, with a
// status callback for updating detail text. Returns fn's error.
// In quiet mode, prints the title to stderr and skips the TUI.
func RunWithSpinnerStatus(title string, fn func(setStatus func(string)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(string) {})
	}

	m := newSpinnerModel(title)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	go func() {
		err := fn(func(s string) { p.Send(statusMsg(s)) })
		p.Send(doneMsg{err: err})
	}()

	result, runErr := p.Run()
	if runErr != nil {
		return runErr
	}
	return result.(spinnerModel).err
}
