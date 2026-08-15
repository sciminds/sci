package uikit

// layout_dims.go — dimension constants and the helpers that derive usable
// content sizes from them.

// ── Dimension guards ────────────────────────────────────────────────────────
// Bubble Tea calls View() before any WindowSizeMsg, so width/height start at
// zero. These constants define the shared "too small to render" thresholds
// that TUI models check before drawing.

const (
	// MinUsableWidth is the minimum terminal width we try to render for.
	MinUsableWidth = 40

	// MinUsableHeight is the minimum usable body height.
	MinUsableHeight = 10
)

// ── Page chrome overhead ────────────────────────────────────────────────────

const (
	// PageChromeLines is the number of vertical lines consumed by
	// PageLayout's wrapper: Page() padding (top 1 + bottom 1) + title (1)
	// + blank after title (2) + footer (1) + blank before footer (2) +
	// visual breathing room (3).
	//
	// Subtract this from terminal height to get the usable body height.
	PageChromeLines = 10

	// PageSidePadding is the horizontal padding applied by Page() style
	// (Padding(1,2) → 2 per side = 4 columns).
	PageSidePadding = 4
)

// ── Spacing tokens ──────────────────────────────────────────────────────────

const (
	// DividerLeadingSpaces is the indent prefix prepended to every divider.
	DividerLeadingSpaces = "  "

	// DividerInset is the total horizontal inset for RenderDivider:
	// PageSidePadding (4) + DividerLeadingSpaces (2) = 6.
	DividerInset = 6

	// ItemDescIndent is the indent for the second line (description) of a
	// two-line select list item. It aligns under the name past the
	// cursor + marker columns.
	ItemDescIndent = "        " // 8 spaces
)

// ── Overlay defaults ────────────────────────────────────────────────────

const (
	// OverlayMargin is the horizontal margin from terminal edges for overlays.
	OverlayMargin = 12

	// OverlayChromeLines is the vertical overhead of the overlay frame:
	// border(2) + box-padding(2) + title(1) + blank(1) + blank(1) + footer(1).
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
	// FallbackDividerWidth is the divider width used when terminal width
	// is unknown or too narrow for arithmetic.
	FallbackDividerWidth = 50
)

// ── Helpers ─────────────────────────────────────────────────────────────────

// ContentWidth returns the usable inner width after subtracting
// PageSidePadding. This is what content inside PageLayout actually has.
func ContentWidth(termWidth int) int {
	w := termWidth - PageSidePadding
	if w < 0 {
		return 0
	}
	return w
}

// OverlayBodyHeight returns the maximum number of body lines available inside
// an overlay, given the terminal height and any extra chrome lines the caller
// adds beyond OverlayChromeLines (e.g. status row, hint rows). The result is
// clamped to at least OverlayMinH.
func OverlayBodyHeight(termH, extraChrome int) int {
	h := max(termH-OverlayChromeLines-extraChrome, OverlayMinH)
	return h
}
