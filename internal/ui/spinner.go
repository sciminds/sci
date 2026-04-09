package ui

// spinner.go — Bubble Tea model that wraps a blocking operation with a spinner
// and optional progress bar, reporting status updates as they arrive.

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Spinner TUI — wraps a blocking operation with visual feedback ───────────

type spinnerDoneMsg struct{ err error }
type spinnerTitleMsg string
type spinnerStatusMsg string
type spinnerSuspendMsg struct{}
type spinnerResumeMsg struct{}

// spinnerModel is a lightweight bubbletea model that shows a spinner
// while a blocking function runs in a goroutine.
type spinnerModel struct {
	spinner   spinner.Model
	title     string
	status    string
	err       error
	done      bool
	suspended bool
}

func newSpinnerModel(title string) spinnerModel {
	s := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(TUI.FgAccent()))
	return spinnerModel{spinner: s, title: title}
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == KeyCtrlC {
			return m, tea.Quit
		}
	case spinnerDoneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case spinnerTitleMsg:
		m.title = string(msg)
		m.status = ""
		return m, nil
	case spinnerStatusMsg:
		m.status = string(msg)
		return m, nil
	case spinnerSuspendMsg:
		m.suspended = true
		return m, nil
	case spinnerResumeMsg:
		m.suspended = false
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() tea.View {
	if m.done || m.suspended {
		return tea.NewView("")
	}
	line := m.spinner.View() + " " + TUI.FgAccent().Render(m.title)
	if m.status != "" {
		line += TUI.Dim().Render("  " + m.status)
	}
	return tea.NewView(line + "\n")
}

// RunWithSpinner shows a spinner while fn runs. The fn receives two callbacks:
// setTitle updates the spinner's main label, setStatus updates the dim detail text.
// Returns fn's error. In quiet mode, skips the TUI and prints the title to stderr.
func RunWithSpinner(title string, fn func(setTitle, setStatus func(string)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(string) {}, func(string) {})
	}

	m := newSpinnerModel(title)
	p := tea.NewProgram(m)

	go func() {
		err := fn(
			func(s string) { p.Send(spinnerTitleMsg(s)) },
			func(s string) { p.Send(spinnerStatusMsg(s)) },
		)
		p.Send(spinnerDoneMsg{err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("spinner TUI: %w", err)
	}
	return finalModel.(spinnerModel).err
}

// SpinnerControls provides suspend/resume in addition to title/status updates.
type SpinnerControls struct {
	SetTitle  func(string)
	SetStatus func(string)
	Suspend   func()
	Resume    func()
}

// RunWithInteractiveSpinner is like RunWithSpinner but provides suspend/resume
// controls for commands that may prompt for user input (e.g. sudo password).
// In quiet mode, skips the TUI and provides no-op callbacks.
func RunWithInteractiveSpinner(title string, fn func(SpinnerControls) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(SpinnerControls{
			SetTitle:  func(string) {},
			SetStatus: func(string) {},
			Suspend:   func() {},
			Resume:    func() {},
		})
	}

	m := newSpinnerModel(title)
	p := tea.NewProgram(m)

	go func() {
		err := fn(SpinnerControls{
			SetTitle:  func(s string) { p.Send(spinnerTitleMsg(s)) },
			SetStatus: func(s string) { p.Send(spinnerStatusMsg(s)) },
			Suspend:   func() { p.Send(spinnerSuspendMsg{}) },
			Resume:    func() { p.Send(spinnerResumeMsg{}) },
		})
		p.Send(spinnerDoneMsg{err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("spinner TUI: %w", err)
	}
	return finalModel.(spinnerModel).err
}

// ── Progress bar TUI — for operations with known total ──────────────────────

type progressTickMsg struct {
	current int64
	total   int64
}
type progressDoneMsg struct{ err error }
type progressStatusMsg string

type progressModel struct {
	bar         progress.Model
	title       string
	status      string
	current     int64
	total       int64
	err         error
	done        bool
	formatLabel func(current, total int64) string
}

func newProgressModel(title string) progressModel {
	// Use the dark-mode accent/success hex values for the gradient.
	bar := progress.New(
		progress.WithColors(lipgloss.Color("#56B4E9"), lipgloss.Color("#009E73")),
		progress.WithWidth(ProgressBarWidth),
	)
	return progressModel{bar: bar, title: title}
}

func (m progressModel) Init() tea.Cmd {
	return nil
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == KeyCtrlC {
			return m, tea.Quit
		}
	case progressDoneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case progressStatusMsg:
		m.status = string(msg)
		return m, nil
	case progressTickMsg:
		m.current = msg.current
		m.total = msg.total
		if m.total > 0 {
			return m, m.bar.SetPercent(float64(m.current) / float64(m.total))
		}
		return m, nil
	case progress.FrameMsg:
		var cmd tea.Cmd
		m.bar, cmd = m.bar.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m progressModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	var b strings.Builder
	b.WriteString(TUI.FgAccent().Render(m.title))
	b.WriteString("\n")
	b.WriteString(m.bar.View())
	if m.total > 0 {
		label := fmt.Sprintf("  %s / %s", formatBytes(m.current), formatBytes(m.total))
		if m.formatLabel != nil {
			label = m.formatLabel(m.current, m.total)
		}
		b.WriteString(TUI.Dim().Render(label))
	}
	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(TUI.Dim().Render("  " + m.status))
		b.WriteString("\n")
	}
	return tea.NewView(b.String())
}

// RunWithItemProgress shows a progress bar for operations with a known item count.
// The fn receives a callback to report (current, total) progress as item counts.
// In quiet mode, skips the TUI and provides a no-op callback.
func RunWithItemProgress(title string, fn func(update func(current, total int)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(int, int) {})
	}

	m := newProgressModel(title)
	m.formatLabel = func(cur, tot int64) string {
		return fmt.Sprintf("  %d / %d", cur, tot)
	}
	p := tea.NewProgram(m)

	go func() {
		err := fn(func(current, total int) {
			p.Send(progressTickMsg{current: int64(current), total: int64(total)})
		})
		p.Send(progressDoneMsg{err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("progress TUI: %w", err)
	}
	return finalModel.(progressModel).err
}

// RunWithItemProgressStatus shows a progress bar with a per-item status line.
// The fn receives a callback to report (current, total, status) after each item.
// In quiet mode, skips the TUI and provides a no-op callback.
func RunWithItemProgressStatus(title string, fn func(update func(current, total int, status string)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(int, int, string) {})
	}

	m := newProgressModel(title)
	m.formatLabel = func(cur, tot int64) string {
		return fmt.Sprintf("  %d / %d", cur, tot)
	}
	p := tea.NewProgram(m)

	go func() {
		err := fn(func(current, total int, status string) {
			p.Send(progressTickMsg{current: int64(current), total: int64(total)})
			p.Send(progressStatusMsg(status))
		})
		p.Send(progressDoneMsg{err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("progress TUI: %w", err)
	}
	return finalModel.(progressModel).err
}

// RunWithProgress shows a progress bar for operations with known total bytes.
// The fn receives a callback to report (current, total) progress.
// In quiet mode, skips the TUI and provides a no-op callback.
func RunWithProgress(title string, fn func(update func(current, total int64)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(int64, int64) {})
	}

	m := newProgressModel(title)
	p := tea.NewProgram(m)

	go func() {
		err := fn(func(current, total int64) {
			p.Send(progressTickMsg{current: current, total: total})
		})
		p.Send(progressDoneMsg{err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("progress TUI: %w", err)
	}
	return finalModel.(progressModel).err
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
