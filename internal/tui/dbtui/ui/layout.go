package ui

// layout.go — dimension constants and clamping helpers for safe rendering when
// Bubble Tea's initial WindowSizeMsg has not yet arrived (width/height = 0).

// ── Dimension guards ────────────────────────────────────────────────────────
// Bubble Tea calls View() before any WindowSizeMsg, so width/height start at
// zero. These constants define the shared safety net used across all TUI
// models.

const (
	// MinUsableWidth is the minimum terminal width we try to render for.
	// Below this we snap to FallbackWidth.
	MinUsableWidth = 40

	// FallbackWidth is the default width assumed when the real width is
	// unknown or too narrow.
	FallbackWidth = 80

	// MinUsableHeight is the minimum usable body height. Below this we
	// snap to FallbackHeight.
	MinUsableHeight = 10

	// FallbackHeight is the default list/table height assumed when the
	// real height is unknown or too short.
	FallbackHeight = 14
)

// ── Page chrome overhead ────────────────────────────────────────────────────

const (
	// PageChromeLines is the number of vertical lines consumed by
	// PageLayout's wrapper.
	PageChromeLines = 10

	// PageSidePadding is the horizontal padding applied by Page() style
	// (Padding(1,2) → 2 per side = 4 columns).
	PageSidePadding = 4
)

// ── Spacing tokens ──────────────────────────────────────────────────────────

const (
	DividerLeadingSpaces = "  "
	DividerInset         = 6
	ItemDescIndent       = "        " // 8 spaces
)

// ── Overlay defaults ────────────────────────────────────────────────────

const (
	// OverlayMargin is the horizontal margin from terminal edges for overlays.
	OverlayMargin = 12

	// OverlayBoxPadding is the total horizontal padding of OverlayBox
	// (Padding(1,2) → 2 per side = 4 columns).
	OverlayBoxPadding = 4

	// OverlayChromeLines is the vertical overhead of the overlay frame.
	OverlayChromeLines = 8

	// OverlayMinH is the minimum viewport body height.
	OverlayMinH = 3

	// OverlayMinW is the minimum overlay width.
	OverlayMinW = 30

	// OverlayMaxW is the maximum overlay width.
	OverlayMaxW = 80
)

// ── Component defaults ──────────────────────────────────────────────────────

const (
	ProgressBarWidth     = 40
	FallbackDividerWidth = 50
)

// ── Helpers ─────────────────────────────────────────────────────────────────

// ContentWidth returns the usable inner width after subtracting
// PageSidePadding.
func ContentWidth(termWidth int) int {
	w := termWidth - PageSidePadding
	if w < 0 {
		return 0
	}
	return w
}

// ClampWidth returns ContentWidth(width) if the result is at least
// MinUsableWidth, otherwise FallbackWidth.
func ClampWidth(width int) int {
	cw := ContentWidth(width)
	if cw < MinUsableWidth {
		return FallbackWidth
	}
	return cw
}

// OverlayBodyHeight returns the maximum number of body lines available inside
// an overlay, given the terminal height and any extra chrome lines.
func OverlayBodyHeight(termH, extraChrome int) int {
	h := termH - OverlayChromeLines - extraChrome
	if h < OverlayMinH {
		h = OverlayMinH
	}
	return h
}

// ClampHeight returns height if it is at least MinUsableHeight, otherwise
// FallbackHeight.
func ClampHeight(height int) int {
	if height < MinUsableHeight {
		return FallbackHeight
	}
	return height
}
