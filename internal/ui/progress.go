package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Messages ────────────────────────────────────────────────────────────────

type progressUpdateMsg struct {
	current   int
	total     int
	status    string
	lastEvent string
	counters  map[string]int
}

type progressDoneMsg struct{ err error }

// ── ProgressTracker ─────────────────────────────────────────────────────────

// ProgressTracker is the handle passed to the callback in RunWithProgress.
// The caller calls SetTotal once and then Advance/Event for each item.
// Methods are goroutine-safe.
type ProgressTracker struct {
	p        *tea.Program
	mu       sync.Mutex
	current  int
	total    int
	counters map[string]int
}

// SetTotal sets the expected number of items.
func (t *ProgressTracker) SetTotal(n int) {
	t.mu.Lock()
	t.total = n
	t.mu.Unlock()
	t.send("", "")
}

// Advance increments the current count by 1 and sends a status update.
// counter is a named bucket (e.g. "created", "skipped", "failed") — its
// total is shown in the progress view.
func (t *ProgressTracker) Advance(counter, event string) {
	t.mu.Lock()
	t.current++
	if counter != "" {
		t.counters[counter]++
	}
	t.mu.Unlock()
	t.send(event, "")
}

// Status updates the status text without advancing the counter.
func (t *ProgressTracker) Status(s string) {
	t.send("", s)
}

func (t *ProgressTracker) send(event, status string) {
	t.mu.Lock()
	msg := progressUpdateMsg{
		current:  t.current,
		total:    t.total,
		status:   status,
		counters: make(map[string]int, len(t.counters)),
	}
	for k, v := range t.counters {
		msg.counters[k] = v
	}
	if event != "" {
		msg.lastEvent = event
	}
	t.mu.Unlock()
	t.p.Send(msg)
}

// ── Model ───────────────────────────────────────────────────────────────────

type progressModel struct {
	spinner   spinner.Model
	title     string
	status    string
	lastEvent string
	current   int
	total     int
	counters  map[string]int
	done      bool
	err       error
	width     int
}

func newProgressModel(title string) progressModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#56B4E9"))),
	)
	return progressModel{
		spinner:  s,
		title:    title,
		counters: map[string]int{},
		width:    60,
	}
}

func (m progressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressUpdateMsg:
		m.current = msg.current
		m.total = msg.total
		m.counters = msg.counters
		if msg.status != "" {
			m.status = msg.status
		}
		if msg.lastEvent != "" {
			m.lastEvent = msg.lastEvent
		}
		return m, nil
	case progressDoneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
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

func (m progressModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	var b strings.Builder

	// Line 1: spinner + title + fraction
	b.WriteString(m.spinner.View())
	b.WriteString(TUI.FgAccent().Render(m.title))
	if m.total > 0 {
		b.WriteString(TUI.Dim().Render(fmt.Sprintf("  %d/%d", m.current, m.total)))
	}
	b.WriteByte('\n')

	// Line 2: progress bar
	if m.total > 0 {
		barWidth := m.width - 12
		if barWidth < 10 {
			barWidth = 10
		}
		if barWidth > 60 {
			barWidth = 60
		}
		filled := barWidth * m.current / m.total
		if filled > barWidth {
			filled = barWidth
		}
		pct := 100 * m.current / m.total
		bar := "  " +
			TUI.FgAccent().Render(strings.Repeat("█", filled)) +
			TUI.Dim().Render(strings.Repeat("░", barWidth-filled)) +
			TUI.Dim().Render(fmt.Sprintf(" %d%%", pct))
		b.WriteString(bar)
		b.WriteByte('\n')
	}

	// Line 3: counters
	if len(m.counters) > 0 {
		b.WriteString("  ")
		first := true
		for _, key := range []string{"created", "replaced", "cached", "skipped", "failed"} {
			v, ok := m.counters[key]
			if !ok || v == 0 {
				continue
			}
			if !first {
				b.WriteString(TUI.Dim().Render(" | "))
			}
			first = false
			label := key + ":" + fmt.Sprintf("%d", v)
			if key == "failed" {
				b.WriteString(TUI.Fail().Render(label))
			} else {
				b.WriteString(TUI.Dim().Render(label))
			}
		}
		b.WriteByte('\n')
	}

	// Line 4: last event or status
	if m.status != "" {
		b.WriteString("  ")
		b.WriteString(TUI.Dim().Render(m.status))
		b.WriteByte('\n')
	} else if m.lastEvent != "" {
		b.WriteString("  ")
		b.WriteString(TUI.Dim().Render(m.lastEvent))
		b.WriteByte('\n')
	}

	return tea.NewView(b.String())
}

// ── Public API ──────────────────────────────────────────────────────────────

// RunWithProgress shows an inline progress display while fn runs. The
// callback receives a ProgressTracker whose methods update the view in
// real-time. In quiet mode, prints the title to stderr and runs fn
// with a no-op tracker.
func RunWithProgress(title string, fn func(t *ProgressTracker) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		tracker := &ProgressTracker{counters: make(map[string]int)}
		return fn(tracker)
	}

	m := newProgressModel(title)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	tracker := &ProgressTracker{
		p:        p,
		counters: make(map[string]int),
	}

	go func() {
		err := fn(tracker)
		p.Send(progressDoneMsg{err: err})
	}()

	result, runErr := p.Run()
	drainStdin()
	if runErr != nil {
		return runErr
	}
	return result.(progressModel).err
}
