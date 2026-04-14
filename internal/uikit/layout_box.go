// layout_box.go — border-box rendering: set outer size, get inner dimensions automatically.
//
// Box eliminates the manual frame arithmetic (width - 4, height - 2, etc.)
// that appears throughout TUI view code. Like CSS box-sizing: border-box,
// you specify the outer dimensions and the style's border/padding, and the
// callback receives the usable inner dimensions.
//
//	// Before: manual arithmetic, easy to get wrong
//	innerW := width - 4  // 2 border + 2 padding... probably?
//	innerH := height - 4
//	content := renderStuff(innerW, innerH)
//	return frame.Width(width).Height(height).Render(content)
//
//	// After: Box computes insets from the style
//	uikit.Box(width, height, frame, func(innerW, innerH int) string {
//	    return renderStuff(innerW, innerH)
//	})

package uikit

import "charm.land/lipgloss/v2"

// Box renders content inside a styled frame, automatically computing inner
// dimensions by subtracting the style's border and padding overhead.
//
// The style's Width and Height are set to the outer dimensions. The callback
// receives the remaining inner dimensions after frame overhead is subtracted.
// Inner dimensions are clamped to at least 1.
//
// Returns empty string if width or height is <= 0.
func Box(width, height int, style lipgloss.Style, fn func(innerW, innerH int) string) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	// Total frame overhead (border + padding) for computing inner content area.
	frameW := style.GetHorizontalFrameSize()
	frameH := style.GetVerticalFrameSize()

	innerW := width - frameW
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - frameH
	if innerH < 1 {
		innerH = 1
	}

	content := fn(innerW, innerH)

	// In lipgloss v2, Width/Height set the total visual size (including
	// border + padding). Pass outer dimensions directly.
	return style.
		Width(width).
		Height(height).
		Render(content)
}
