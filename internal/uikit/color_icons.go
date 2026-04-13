package uikit

// color_icons.go — Unicode icon constants and pre-rendered symbols shared
// across all TUI and CLI output.

// Icon constants — raw strings so callers can apply their own styles.
const (
	IconPass    = "✓"
	IconFail    = "✗"
	IconWarn    = "⚠"
	IconPending = "○"
	IconArrow   = "→"
	IconCursor  = "❯"
	IconDot     = "●"
	IconSkip    = "–"
)

// Pre-rendered symbols for non-TUI CLI output.
var (
	SymOK    = TUI.Pass().Render(IconPass)
	SymFail  = TUI.Fail().Render(IconFail)
	SymWarn  = TUI.Warn().Render(IconWarn)
	SymArrow = TUI.TextBlue().Render(IconArrow)
)
