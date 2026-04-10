package ui

// spinner.go — tick-loop renderer for spinners and progress bars.
// Renders inline to stderr using ANSI escape codes. No bubbletea program,
// no raw mode, no terminal capability queries — child processes get clean
// uncontested access to stdin/stdout.

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
)

// Braille dot spinner frames (matches bubbles/spinner.Dot).
var spinnerFrames = []string{"⣾ ", "⣽ ", "⣻ ", "⢿ ", "⡿ ", "⣟ ", "⣯ ", "⣷ "}

const spinnerFPS = time.Second / 10

// ── tickRenderer — inline spinner / progress renderer ────────────────────────

// tickRenderer manages a ticker goroutine that renders a spinner or progress
// bar to an io.Writer (normally os.Stderr). All state updates are mutex-
// protected and safe to call from any goroutine.
//
// Rendering strategy: the spinner occupies line(s) ABOVE the cursor position.
// The cursor always sits on a blank line below, so external process output
// (e.g. sudo password prompts written to /dev/tty) appears there and is never
// overwritten by the spinner's tick loop.
type tickRenderer struct {
	mu        sync.Mutex
	out       io.Writer
	title     string
	status    string
	frameIdx  int
	suspended bool
	drawn     bool // true after the first frame has been rendered

	// Progress-bar mode (nil = spinner mode).
	bar         *progress.Model
	percent     float64
	barLabel    string
	lines       int // number of rendered lines (1 for spinner, 2-3 for progress)
	formatLabel func(current, total int64) string

	done chan struct{}
}

func newTickRenderer(out io.Writer, title string) *tickRenderer {
	return &tickRenderer{
		out:   out,
		title: title,
		done:  make(chan struct{}),
		lines: 1,
	}
}

func newProgressRenderer(out io.Writer, title string, formatLabel func(current, total int64) string) *tickRenderer {
	bar := progress.New(
		progress.WithColors(lipgloss.Color("#56B4E9"), lipgloss.Color("#009E73")),
		progress.WithWidth(ProgressBarWidth),
	)
	return &tickRenderer{
		out:         out,
		title:       title,
		bar:         &bar,
		done:        make(chan struct{}),
		lines:       2,
		formatLabel: formatLabel,
	}
}

// start launches the rendering goroutine.
func (r *tickRenderer) start() {
	go func() {
		ticker := time.NewTicker(spinnerFPS)
		defer ticker.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-ticker.C:
				r.render()
			}
		}
	}()
}

// stop halts the renderer and clears any output.
func (r *tickRenderer) stop() {
	close(r.done)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drawn {
		r.clearLines()
		r.drawn = false
	}
}

// setTitle updates the title and clears the status line.
func (r *tickRenderer) setTitle(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.title = s
	r.status = ""
}

// setStatus updates the status text.
func (r *tickRenderer) setStatus(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = s
}

// suspend hides the spinner so the terminal is clean for interactive prompts.
// Clears the spinner lines and moves the cursor back down to the line where
// external output (e.g. a password prompt) appears.
func (r *tickRenderer) suspend() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suspended = true
	if r.drawn {
		r.clearLines()
		// Move cursor back down past the cleared region and below any
		// external output (e.g. "Password:") on the cursor line.
		for i := 0; i <= r.lines; i++ {
			_, _ = fmt.Fprint(r.out, "\033[B")
		}
		_, _ = fmt.Fprint(r.out, "\r")
		r.drawn = false
	}
}

// resume restores rendering after a suspend. The next tick will redraw the
// spinner above the cursor.
func (r *tickRenderer) resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suspended = false
}

// setProgress updates progress bar state.
func (r *tickRenderer) setProgress(current, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if total > 0 {
		r.percent = float64(current) / float64(total)
	}
	if r.formatLabel != nil {
		r.barLabel = r.formatLabel(current, total)
	}
}

// setProgressStatus updates both progress and status in one call.
func (r *tickRenderer) setProgressStatus(current, total int64, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if total > 0 {
		r.percent = float64(current) / float64(total)
	}
	if r.formatLabel != nil {
		r.barLabel = r.formatLabel(current, total)
	}
	r.status = status
}

// render writes one frame to the output. Must not be called with mu held.
//
// Layout: the spinner occupies N lines above the cursor. The cursor always
// rests on a blank line below so that external /dev/tty output (e.g. sudo
// "Password:" prompt) is never overwritten.
//
//	Line -N   ⣾  Removing quarto…        ← spinner (rewritten each tick)
//	Line -N+1 [progress bar, if any]
//	Line 0    _                           ← cursor; external output safe here
func (r *tickRenderer) render() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.suspended {
		return
	}

	if r.drawn {
		// Move cursor up to the first spinner line.
		for i := 0; i < r.lines; i++ {
			_, _ = fmt.Fprint(r.out, "\033[A")
		}
	}

	if r.bar == nil {
		// Spinner mode: single line + cursor line below.
		frame := TUI.FgAccent().Render(spinnerFrames[r.frameIdx])
		r.frameIdx = (r.frameIdx + 1) % len(spinnerFrames)
		line := frame + " " + TUI.FgAccent().Render(r.title)
		if r.status != "" {
			line += TUI.Dim().Render("  " + r.status)
		}
		_, _ = fmt.Fprint(r.out, "\r\033[K"+line+"\n")
	} else {
		// Progress mode: title + bar + optional status, then cursor line.
		r.lines = 2
		_, _ = fmt.Fprint(r.out, "\r\033[K"+TUI.FgAccent().Render(r.title)+"\n")
		barLine := r.bar.ViewAs(r.percent)
		if r.barLabel != "" {
			barLine += TUI.Dim().Render(r.barLabel)
		}
		_, _ = fmt.Fprint(r.out, "\033[K"+barLine+"\n")
		if r.status != "" {
			r.lines = 3
			_, _ = fmt.Fprint(r.out, "\033[K"+TUI.Dim().Render("  "+r.status)+"\n")
		}
	}

	r.drawn = true
}

// clearLines erases the spinner output above the cursor. Caller must hold mu.
// After clearing, the cursor is on the line where the spinner's first line was.
func (r *tickRenderer) clearLines() {
	// Move up into the spinner region.
	for i := 0; i < r.lines; i++ {
		_, _ = fmt.Fprint(r.out, "\033[A\033[2K")
	}
	_, _ = fmt.Fprint(r.out, "\r")
}

// ── Public API ───────────────────────────────────────────────────────────────

// SpinnerControls provides title/status updates and suspend/resume controls.
// Suspend hides the spinner so interactive prompts (e.g. sudo password) are
// visible; Resume restores it.
type SpinnerControls struct {
	SetTitle  func(string)
	SetStatus func(string)
	Suspend   func()
	Resume    func()
}

// RunWithSpinner shows a spinner while fn runs. The fn receives SpinnerControls
// with title/status updates and suspend/resume for interactive prompts.
// Returns fn's error. In quiet mode, skips the TUI and prints the title to stderr.
func RunWithSpinner(title string, fn func(SpinnerControls) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(SpinnerControls{
			SetTitle:  func(string) {},
			SetStatus: func(string) {},
			Suspend:   func() {},
			Resume:    func() {},
		})
	}

	r := newTickRenderer(os.Stderr, title)
	r.start()

	err := fn(SpinnerControls{
		SetTitle:  r.setTitle,
		SetStatus: r.setStatus,
		Suspend:   r.suspend,
		Resume:    r.resume,
	})

	r.stop()
	return err
}

// ── Progress bar variants ────────────────────────────────────────────────────

// RunWithItemProgress shows a progress bar for operations with a known item count.
// The fn receives a callback to report (current, total) progress as item counts.
// In quiet mode, skips the TUI and provides a no-op callback.
func RunWithItemProgress(title string, fn func(update func(current, total int)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(int, int) {})
	}

	r := newProgressRenderer(os.Stderr, title, func(cur, tot int64) string {
		return fmt.Sprintf("  %d / %d", cur, tot)
	})
	r.start()

	err := fn(func(current, total int) {
		r.setProgress(int64(current), int64(total))
	})

	r.stop()
	return err
}

// RunWithItemProgressStatus shows a progress bar with a per-item status line.
// The fn receives a callback to report (current, total, status) after each item.
// In quiet mode, skips the TUI and provides a no-op callback.
func RunWithItemProgressStatus(title string, fn func(update func(current, total int, status string)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(int, int, string) {})
	}

	r := newProgressRenderer(os.Stderr, title, func(cur, tot int64) string {
		return fmt.Sprintf("  %d / %d", cur, tot)
	})
	r.start()

	err := fn(func(current, total int, status string) {
		r.setProgressStatus(int64(current), int64(total), status)
	})

	r.stop()
	return err
}

// RunWithProgress shows a progress bar for operations with known total bytes.
// The fn receives a callback to report (current, total) progress.
// In quiet mode, skips the TUI and provides a no-op callback.
func RunWithProgress(title string, fn func(update func(current, total int64)) error) error {
	if IsQuiet() {
		fmt.Fprintf(os.Stderr, "%s\n", title)
		return fn(func(int64, int64) {})
	}

	r := newProgressRenderer(os.Stderr, title, func(cur, tot int64) string {
		return fmt.Sprintf("  %s / %s", formatBytes(cur), formatBytes(tot))
	})
	r.start()

	err := fn(func(current, total int64) {
		r.setProgress(current, total)
	})

	r.stop()
	return err
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
