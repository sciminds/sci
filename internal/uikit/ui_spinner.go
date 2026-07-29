package uikit

// spinner.go — bubbletea inline runner for long-running operations.
// Provides both a simple spinner (RunWithSpinner / RunWithSpinnerStatus) and a
// progress-bar variant (RunWithProgress) backed by a single model.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// ErrInterrupted is returned by the spinner/progress runners when the user
// aborts with ctrl+c. Callers that can distinguish "the work failed" from
// "the user stopped it" should errors.Is against this.
var ErrInterrupted = errors.New("interrupted")

// ── Messages ────────────────────────────────────────────────────────────────

type statusMsg string

type titleMsg string

type doneMsg struct{ err error }

type progressUpdateMsg struct {
	current   int
	total     int
	status    string
	lastEvent string
	counters  []counterEntry
}

type progressResetMsg struct {
	total int
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

// SetTitle updates the progress bar title text.
func (t *ProgressTracker) SetTitle(s string) {
	if t.p != nil {
		t.p.Send(titleMsg(s))
	}
}

// SetTotal sets the expected number of items.
func (t *ProgressTracker) SetTotal(n int) {
	t.mu.Lock()
	t.total = n
	t.mu.Unlock()
	t.send("", "")
}

// Reset clears the current count, counters, and status text so the
// tracker can be reused for a new phase within the same progress view.
func (t *ProgressTracker) Reset(title string, total int) {
	t.mu.Lock()
	t.current = 0
	t.total = total
	t.counters = nil
	t.mu.Unlock()
	if t.p != nil {
		t.p.Send(titleMsg(title))
		t.p.Send(progressResetMsg{total: total})
	}
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
	if t.p != nil {
		t.p.Send(msg)
	}
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

	// cancelWork, when non-nil, makes ctrl+c cancel the work's context and
	// keep the display alive until the work goroutine reports done —
	// quitting immediately would exit the process while the (canceled but
	// still winding down) work's subprocesses get reaped. Nil keeps the
	// legacy behavior: quit the display, work keeps running.
	cancelWork  func()
	interrupted bool
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
	case progressResetMsg:
		m.current = 0
		m.total = msg.total
		m.counters = nil
		m.status = ""
		m.lastEvent = ""
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
			if m.cancelWork != nil && !m.interrupted {
				// First ctrl+c in cancellable mode: cancel the work and
				// wait for its doneMsg instead of quitting. Context
				// cancellation is goroutine-safe and non-blocking, so
				// calling it inline beats a Cmd — the kill must not wait
				// on the event loop's scheduling.
				m.interrupted = true
				m.cancelWork()
				return m, nil
			}
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

	// Line 1: spinner + title + fraction
	b.WriteString(m.spinner.View())
	b.WriteString(TUI.TextBlue().Render(m.title))
	if m.progress && m.total > 0 {
		b.WriteString(TUI.Dim().Render(fmt.Sprintf("  %d/%d", m.current, m.total)))
	}
	if !m.progress && m.status != "" {
		b.WriteString(TUI.Dim().Render("  " + m.status))
	}
	b.WriteByte('\n')

	if m.interrupted {
		b.WriteString("  ")
		b.WriteString(TUI.Fail().Render("interrupting — waiting for the current job to stop (ctrl+c again to abandon)"))
		b.WriteByte('\n')
	}

	if m.progress {
		m.viewProgress(&b)
	}

	return tea.NewView(b.String())
}

func (m runnerModel) viewProgress(b *strings.Builder) {
	// Progress bar
	if m.total > 0 {
		barWidth := min(max(m.width-12, 10), 60)
		filled := min(barWidth*m.current/m.total, barWidth)
		pct := 100 * m.current / m.total
		b.WriteString("  ")
		b.WriteString(TUI.TextBlue().Render(strings.Repeat("█", filled)))
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
	out := slices.Clone(cs)
	slices.SortStableFunc(out, func(a, b counterEntry) int {
		oi, oki := counterOrder[a.Key]
		oj, okj := counterOrder[b.Key]
		if oki && okj {
			return cmp.Compare(oi, oj)
		}
		if oki != okj {
			if oki {
				return -1 // known keys first
			}
			return 1
		}
		return cmp.Compare(a.Key, b.Key)
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
// In quiet mode or without a terminal on stderr (see [interactive]),
// prints the title to stderr and skips the TUI.
func RunWithSpinnerStatus(title string, fn func(setStatus func(string)) error) error {
	if !interactive() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(string) {})
	}

	m := newRunnerModel(title, false)
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

// RunWithProgress shows an inline progress display while fn runs. The
// callback receives a ProgressTracker whose methods update the view in
// real-time. In quiet mode or without a terminal on stderr (see
// [interactive]), prints the title to stderr and runs fn with a no-op
// tracker.
//
// ctrl+c quits the *display* only — the work keeps running to completion.
// When the work can honor cancellation, use [RunWithProgressCtx] instead
// so ctrl+c actually stops it.
func RunWithProgress(title string, fn func(t *ProgressTracker) error) error {
	return runProgress(context.Background(), title, func(_ context.Context, t *ProgressTracker) error {
		return fn(t)
	}, false)
}

// RunWithProgressCtx is [RunWithProgress] for cancellable work: fn receives
// a context derived from ctx, and ctrl+c cancels it. The display then stays
// up ("interrupting…") until fn returns — so any subprocess teardown wired
// to the context (e.g. docling's process-group kill) completes before the
// command exits; a second ctrl+c abandons the wait. Returns
// [ErrInterrupted] when the user aborted, whatever fn returned otherwise.
func RunWithProgressCtx(ctx context.Context, title string, fn func(ctx context.Context, t *ProgressTracker) error) error {
	return runProgress(ctx, title, fn, true)
}

// runProgress is the shared body of RunWithProgress (cancellable=false:
// ctrl+c quits the display, work keeps running) and RunWithProgressCtx
// (cancellable=true: ctrl+c cancels the derived context and waits for fn).
func runProgress(ctx context.Context, title string, fn func(ctx context.Context, t *ProgressTracker) error, cancellable bool) error {
	var cancel context.CancelFunc
	if cancellable {
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	if !interactive() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(ctx, &ProgressTracker{})
	}

	m := newRunnerModel(title, true)
	m.cancelWork = cancel // nil under RunWithProgress
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	tracker := &ProgressTracker{p: p}

	workErr := make(chan error, 1)
	go func() {
		err := fn(ctx, tracker)
		workErr <- err
		p.Send(doneMsg{err: err})
	}()

	result, runErr := p.Run()
	DrainStdin()
	if runErr != nil {
		return reportDisplayFailure(title, runErr, workErr)
	}
	rm := result.(runnerModel)
	if rm.interrupted {
		// fn's own error (if any) is cancellation fallout — the user's
		// abort is the story worth reporting.
		return ErrInterrupted
	}
	return rm.err
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
	fmt.Fprintf(os.Stderr, "%s\n(progress display unavailable: %v)\n", title, runErr)
	return <-workErr
}
