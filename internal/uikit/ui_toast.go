package uikit

// ui_toast.go — auto-dismissing toast notifications for Bubbletea TUIs.
// Inspired by github.com/junhinhow/charm-toast but implemented as a proper
// tea.Model that uses project Palette/Styles and tick-based expiry.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/samber/lo"
)

// ── Toast level ────────────────────────────────────────────────────────────

// ToastLevel represents the severity of a toast notification.
type ToastLevel int

const (
	ToastInfo    ToastLevel = iota // informational
	ToastSuccess                   // operation succeeded
	ToastWarning                   // non-fatal warning
	ToastError                     // error
)

// String returns a human-readable label for the level.
func (l ToastLevel) String() string {
	switch l {
	case ToastInfo:
		return "info"
	case ToastSuccess:
		return "success"
	case ToastWarning:
		return "warning"
	case ToastError:
		return "error"
	default:
		return "unknown"
	}
}

// Icon returns the Unicode icon for the level (from color_symbols.go).
func (l ToastLevel) Icon() string {
	switch l {
	case ToastInfo:
		return IconArrow
	case ToastSuccess:
		return IconPass
	case ToastWarning:
		return IconWarn
	case ToastError:
		return IconFail
	default:
		return IconDot
	}
}

// style returns the lipgloss style for the level from the shared TUI singleton.
func (l ToastLevel) style() lipgloss.Style {
	switch l {
	case ToastInfo:
		return TUI.TextBlue()
	case ToastSuccess:
		return TUI.Pass()
	case ToastWarning:
		return TUI.Warn()
	case ToastError:
		return TUI.Fail()
	default:
		return TUI.Dim()
	}
}

// ── Toast value type ───────────────────────────────────────────────────────

// Toast is a single notification. It is a plain value — the ToastModel
// manages timing and rendering.
type Toast struct {
	Message  string
	Level    ToastLevel
	Duration time.Duration
}

// WithLevel returns a copy with the level changed.
func (t Toast) WithLevel(l ToastLevel) Toast { t.Level = l; return t }

// WithMessage returns a copy with the message changed.
func (t Toast) WithMessage(m string) Toast { t.Message = m; return t }

// WithDuration returns a copy with the duration changed.
func (t Toast) WithDuration(d time.Duration) Toast { t.Duration = d; return t }

// ── Internal entry (adds timing) ───────────────────────────────────────────

type toastEntry struct {
	Toast
	createdAt time.Time
}

func (e toastEntry) expired() bool {
	return time.Since(e.createdAt) >= e.Duration
}

// ── Messages ───────────────────────────────────────────────────────────────

// toastTickMsg is the internal tick fired to garbage-collect expired toasts.
type toastTickMsg struct{}

// toastTickInterval controls how often we check for expired toasts.
const toastTickInterval = 500 * time.Millisecond

// ── ToastModel ─────────────────────────────────────────────────────────────

// ToastModel manages a stack of auto-dismissing toast notifications.
// It is a Bubbletea model: embed it in your root model and forward
// Update/View. Push new toasts via Push().
type ToastModel struct {
	toasts     []toastEntry
	MaxVisible int
}

// NewToastModel returns an empty toast manager showing up to 5 toasts.
func NewToastModel() ToastModel {
	return ToastModel{MaxVisible: 5}
}

// Push adds a toast and returns a tick Cmd to start expiry polling.
func (m ToastModel) Push(t Toast) (ToastModel, tea.Cmd) {
	m.toasts = append(m.toasts, toastEntry{
		Toast:     t,
		createdAt: time.Now(),
	})
	return m, m.scheduleTick()
}

// Active returns true when at least one toast is visible.
func (m ToastModel) Active() bool {
	return lo.ContainsBy(m.toasts, func(e toastEntry) bool { return !e.expired() })
}

// Dismiss removes the newest (topmost) toast.
func (m ToastModel) Dismiss() ToastModel {
	if len(m.toasts) > 0 {
		m.toasts = m.toasts[:len(m.toasts)-1]
	}
	return m
}

// DismissAll removes all toasts.
func (m ToastModel) DismissAll() ToastModel {
	m.toasts = nil
	return m
}

// Update handles tick messages. Forward all tea.Msg here from your
// root model's Update. Returns the concrete ToastModel (not tea.Model)
// so callers don't need a type assertion.
func (m ToastModel) Update(msg tea.Msg) (ToastModel, tea.Cmd) {
	if _, ok := msg.(toastTickMsg); !ok {
		return m, nil
	}
	// Garbage-collect expired entries.
	m.toasts = lo.Filter(m.toasts, func(e toastEntry, _ int) bool {
		return !e.expired()
	})
	if len(m.toasts) == 0 {
		return m, nil
	}
	return m, m.scheduleTick()
}

// View renders active toasts stacked vertically (newest at bottom).
// The caller decides where to place the result (e.g. Chrome.Status,
// overlay compositor, or lipgloss.Place).
func (m ToastModel) View(width int) string {
	active := lo.Filter(m.toasts, func(e toastEntry, _ int) bool {
		return !e.expired()
	})
	if len(active) == 0 {
		return ""
	}
	// Show only the newest MaxVisible entries.
	if len(active) > m.MaxVisible {
		active = active[len(active)-m.MaxVisible:]
	}
	lines := lo.Map(active, func(e toastEntry, _ int) string {
		return m.renderOne(e, width)
	})
	return strings.Join(lines, "\n")
}

// ── Internal helpers ───────────────────────────────────────────────────────

func (m ToastModel) scheduleTick() tea.Cmd {
	return tea.Tick(toastTickInterval, func(_ time.Time) tea.Msg {
		return toastTickMsg{}
	})
}

func (m ToastModel) renderOne(e toastEntry, width int) string {
	sty := e.Level.style()
	icon := sty.Render(e.Level.Icon())
	msg := TUI.Dim().Render(e.Message)
	content := icon + " " + msg

	if width > 0 {
		content = lipgloss.PlaceHorizontal(width, lipgloss.Right, content)
	}
	return content
}
