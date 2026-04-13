// Package ui is the design system for dbtui's terminal interface.
//
// It provides a single, shared set of styles, colors, layout constants, and
// overlay primitives. All visual output in the TUI is composed from this
// package — no file should inline lipgloss.NewStyle() directly.
//
// # Styles singleton
//
// The package-level variable [TUI] is a pre-built [Styles] instance.
// Access styles through its methods:
//
//	ui.TUI.TextBlue().Render("highlighted text")
//	ui.TUI.Error().Render("something went wrong")
//	ui.TUI.OverlayBox().Width(40).Render(content)
//
// Never create ad-hoc lipgloss styles in application code. If you need a new
// style, add it to the [Styles] struct with an accessor method.
//
// # Color palette
//
// Colors are defined in palette.go as a [Palette] struct resolved once at init
// via [NewPalette]. The palette uses the Wong colorblind-safe scheme with
// light/dark variants selected by [lipgloss.LightDark]:
//
//   - Accent    — primary interactive elements (links, active tabs, focus rings)
//   - Secondary — secondary emphasis (sort arrows, warnings)
//   - Success   — confirmations, info messages, active column headers
//   - Danger    — errors, destructive actions
//   - Muted     — de-emphasized elements (filter marks, pinned indicators)
//   - TextBright / TextMid / TextDim — text hierarchy
//   - Surface   — background for selected rows
//   - OnAccent  — text on accent-colored backgrounds
//   - Border    — borders, table separators
//
// # Overlay system
//
// Modal overlays are rendered using [CenterOverlay] to composite a foreground
// panel over a dimmed background:
//
//	fg := ui.CancelFaint(overlayContent)
//	bg := ui.DimBackground(baseView)
//	result := ui.CenterOverlay(fg, bg)
//
// Use [OverlayWidth] to compute responsive overlay widths that respect minimum
// and maximum bounds relative to terminal width. Use [OverlayBodyHeight] to
// compute the available body height inside an overlay after accounting for
// chrome lines.
//
// # Layout constants
//
// layout.go exports constants and helpers for consistent sizing:
//
//   - [MinUsableWidth] / [MinUsableHeight] — below this, show "too small" message
//   - [OverlayWidth] — responsive width clamped between min and max
//   - [OverlayBodyHeight] — available height minus chrome
//   - [OverlayBoxPadding] — horizontal padding consumed by the overlay box border
//   - [OverlayMargin] — external margin around overlays
package ui
