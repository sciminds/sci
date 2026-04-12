package ui

// spinner.go — bubbletea inline runner for long-running operations.
// Provides both a simple spinner (RunWithSpinner / RunWithSpinnerStatus) and a
// progress-bar variant (RunWithProgress) backed by a single model.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// ── Messages ────────────────────────────────────────────────────────────────

type statusMsg string

type doneMsg struct{ err error }

type progressUpdateMsg struct {
	current   int
	total     int
	status    string
	lastEvent string
	counters  []counterEntry
}

// counterEntry is a single named counter for progress display.
type counterEntry struct {
	Key   string
	Count int
}

// ── ProgressTracker ─────────────────────────────────────────────────────────

// ProgressTracker is the handle passed to the callback in RunWithProgress.
// The caller calls SetTotal once and then Advance/Event for each item.
// Methods are goroutine-safe.
type ProgressTracker struct {
	p        *tea.Program
	mu       sync.Mutex
	current  int
	total    int
	counters []counterEntry
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
		t.bumpCounter(counter)
	}
	t.mu.Unlock()
	t.send(event, "")
}

// Status updates the status text without advancing the counter.
func (t *ProgressTracker) Status(s string) {
	t.send("", s)
}

// bumpCounter increments the named counter. Must be called with mu held.
func (t *ProgressTracker) bumpCounter(key string) {
	for i := range t.counters {
		if t.counters[i].Key == key {
			t.counters[i].Count++
			return
		}
	}
	t.counters = append(t.counters, counterEntry{Key: key, Count: 1})
}

func (t *ProgressTracker) send(event, status string) {
	t.mu.Lock()
	snap := make([]counterEntry, len(t.counters))
	copy(snap, t.counters)
	msg := progressUpdateMsg{
		current:  t.current,
		total:    t.total,
		status:   status,
		counters: snap,
	}
	if event != "" {
		msg.lastEvent = event
	}
	t.mu.Unlock()
	t.p.Send(msg)
}

// ── Model ───────────────────────────────────────────────────────────────────

type runnerModel struct {
	spinner   spinner.Model
	title     string
	status    string
	lastEvent string
	current   int
	total     int
	counters  []counterEntry
	progress  bool // true = show bar + counters
	done      bool
	err       error
	width     int
}

func newRunnerModel(title string, progress bool) runnerModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(TUI.SpinnerDot()),
	)
	return runnerModel{
		spinner:  s,
		title:    title,
		progress: progress,
		width:    60,
	}
}

func (m runnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m runnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		m.status = string(msg)
		return m, nil
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
	case doneMsg:
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

func (m runnerModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	var b strings.Builder

	// Line 1: spinner + title + fraction
	b.WriteString(m.spinner.View())
	b.WriteString(TUI.FgAccent().Render(m.title))
	if m.progress && m.total > 0 {
		b.WriteString(TUI.Dim().Render(fmt.Sprintf("  %d/%d", m.current, m.total)))
	}
	if !m.progress && m.status != "" {
		b.WriteString(TUI.Dim().Render("  " + m.status))
	}
	b.WriteByte('\n')

	if m.progress {
		m.viewProgress(&b)
	}

	return tea.NewView(b.String())
}

func (m runnerModel) viewProgress(b *strings.Builder) {
	// Progress bar
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
		b.WriteString("  ")
		b.WriteString(TUI.FgAccent().Render(strings.Repeat("█", filled)))
		b.WriteString(TUI.Dim().Render(strings.Repeat("░", barWidth-filled)))
		b.WriteString(TUI.Dim().Render(fmt.Sprintf(" %d%%", pct)))
		b.WriteByte('\n')
	}

	// Counters — sorted with well-known keys first for stable display
	if len(m.counters) > 0 {
		b.WriteString("  ")
		sorted := sortCounters(m.counters)
		for i, c := range sorted {
			if i > 0 {
				b.WriteString(TUI.Dim().Render(" | "))
			}
			label := c.Key + ":" + fmt.Sprintf("%d", c.Count)
			if c.Key == "failed" {
				b.WriteString(TUI.Fail().Render(label))
			} else {
				b.WriteString(TUI.Dim().Render(label))
			}
		}
		b.WriteByte('\n')
	}

	// Status / last event
	if m.status != "" {
		b.WriteString("  ")
		b.WriteString(TUI.Dim().Render(m.status))
		b.WriteByte('\n')
	} else if m.lastEvent != "" {
		b.WriteString("  ")
		b.WriteString(TUI.Dim().Render(m.lastEvent))
		b.WriteByte('\n')
	}
}

// counterOrder defines preferred display position for well-known counters.
// Keys not in this map sort alphabetically after the known ones.
var counterOrder = map[string]int{
	"created":  0,
	"replaced": 1,
	"cached":   2,
	"patched":  3,
	"skipped":  4,
	"failed":   5,
}

func sortCounters(cs []counterEntry) []counterEntry {
	out := make([]counterEntry, len(cs))
	copy(out, cs)
	sort.SliceStable(out, func(i, j int) bool {
		oi, oki := counterOrder[out[i].Key]
		oj, okj := counterOrder[out[j].Key]
		if oki && okj {
			return oi < oj
		}
		if oki != okj {
			return oki // known keys first
		}
		return out[i].Key < out[j].Key
	})
	return out
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
// In quiet mode, prints the title to stderr and skips the TUI.
func RunWithSpinnerStatus(title string, fn func(setStatus func(string)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(string) {})
	}

	m := newRunnerModel(title, false)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	go func() {
		err := fn(func(s string) { p.Send(statusMsg(s)) })
		p.Send(doneMsg{err: err})
	}()

	result, runErr := p.Run()
	DrainStdin()
	if runErr != nil {
		return runErr
	}
	return result.(runnerModel).err
}

// RunWithProgress shows an inline progress display while fn runs. The
// callback receives a ProgressTracker whose methods update the view in
// real-time. In quiet mode, prints the title to stderr and runs fn
// with a no-op tracker.
func RunWithProgress(title string, fn func(t *ProgressTracker) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		tracker := &ProgressTracker{}
		return fn(tracker)
	}

	m := newRunnerModel(title, true)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	tracker := &ProgressTracker{p: p}

	go func() {
		err := fn(tracker)
		p.Send(doneMsg{err: err})
	}()

	result, runErr := p.Run()
	DrainStdin()
	if runErr != nil {
		return runErr
	}
	return result.(runnerModel).err
}
