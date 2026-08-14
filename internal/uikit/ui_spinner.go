package uikit

// ui_spinner.go — bubbletea inline runner for long-running operations.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// ErrInterrupted is returned by the spinner runner when the user aborts with
// ctrl+c. Callers that can distinguish "the work failed" from "the user
// stopped it" should errors.Is against this.
var ErrInterrupted = errors.New("interrupted")

// ── Messages ────────────────────────────────────────────────────────────────

type statusMsg string

type titleMsg string

type doneMsg struct{ err error }

// ── Model ───────────────────────────────────────────────────────────────────

type runnerModel struct {
	spinner spinner.Model
	title   string
	status  string
	done    bool
	err     error
}

func newRunnerModel(title string) runnerModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(TUI.SpinnerDot()),
	)
	return runnerModel{spinner: s, title: title}
}

// Init implements tea.Model.
func (m runnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update implements tea.Model.
func (m runnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case titleMsg:
		m.title = string(msg)
		return m, nil
	case statusMsg:
		m.status = string(msg)
		return m, nil
	case doneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			// Quits the display only; the work goroutine keeps running to
			// completion. RunWithSpinnerStatus still returns through the
			// model, so the abort is reported as ErrInterrupted rather than
			// as whatever the abandoned work eventually says.
			m.done = true
			m.err = ErrInterrupted
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m runnerModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	var b strings.Builder
	b.WriteString(m.spinner.View())
	b.WriteString(TUI.TextBlue().Render(m.title))
	if m.status != "" {
		b.WriteString(TUI.Dim().Render("  " + m.status))
	}
	b.WriteByte('\n')

	return tea.NewView(b.String())
}

// ── Public API ──────────────────────────────────────────────────────────────

// RunWithSpinner shows an inline spinner while fn runs. Returns fn's error.
// In quiet mode, prints the title to stderr and skips the TUI.
func RunWithSpinner(title string, fn func() error) error {
	return RunWithSpinnerStatus(title, func(_ func(string)) error {
		return fn()
	})
}

// RunWithSpinnerStatus shows an inline spinner while fn runs, with a
// status callback for updating detail text. Returns fn's error.
// In quiet mode or without a terminal on stderr (see [interactive]),
// prints the title to stderr and skips the TUI.
func RunWithSpinnerStatus(title string, fn func(setStatus func(string)) error) error {
	if !interactive() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(string) {})
	}

	m := newRunnerModel(title)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	workErr := make(chan error, 1)
	go func() {
		err := fn(func(s string) { p.Send(statusMsg(s)) })
		workErr <- err
		p.Send(doneMsg{err: err})
	}()

	result, runErr := p.Run()
	DrainStdin()
	if runErr != nil {
		return reportDisplayFailure(title, runErr, workErr)
	}
	return result.(runnerModel).err
}

// reportDisplayFailure handles a bubbletea program that failed to run. The work
// goroutine is unaffected by that failure and keeps going, so its outcome — not
// the display error — is what the caller must report; returning runErr here is
// what made a successful --apply look like a failure. The TUI error is demoted
// to a stderr diagnostic and the work's own error is returned.
//
// Waiting on workErr cannot deadlock: [tea.Program.Run] cancels the program
// context before returning, which unblocks any [tea.Program.Send] the work
// goroutine is sitting in.
func reportDisplayFailure(title string, runErr error, workErr <-chan error) error {
	fmt.Fprintf(os.Stderr, "%s\n(spinner display unavailable: %v)\n", title, runErr)
	return <-workErr
}
