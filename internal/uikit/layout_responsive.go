// layout_responsive.go — breakpoint-driven layout switching.
//
// Responsive selects a layout based on terminal width, like Tailwind CSS
// breakpoints (sm: / md: / lg:). The highest matching breakpoint wins.
//
//	uikit.Responsive(width, height).
//	    When(80, func(w, h int) string {  // >= 80 cols: side by side
//	        return uikit.HStack(w, h).
//	            Flex(0.5, renderLeft).
//	            Flex(0.5, renderRight).
//	            Render()
//	    }).
//	    Default(func(w, h int) string {   // < 80 cols: stacked
//	        return uikit.VStack(w, h).
//	            Flex(1, renderTop).
//	            Flex(1, renderBottom).
//	            Render()
//	    }).
//	    Render()

package uikit

import "slices"

// breakpoint is a width threshold paired with a render function.
type breakpoint struct {
	minWidth int
	fn       func(w, h int) string
}

// ResponsiveLayout is a builder for breakpoint-driven layout switching.
// Use [Responsive] to create one.
type ResponsiveLayout struct {
	width       int
	height      int
	breakpoints []breakpoint
	defaultFn   func(w, h int) string
}

// Responsive creates a breakpoint-driven layout builder. Use [ResponsiveLayout.When]
// to add breakpoints and [ResponsiveLayout.Default] for the fallback.
func Responsive(width, height int) *ResponsiveLayout {
	return &ResponsiveLayout{width: width, height: height}
}

// When adds a breakpoint: if width >= minWidth, use this render function.
// Multiple breakpoints are evaluated highest-first; the first match wins.
// Breakpoints can be added in any order — they are sorted internally.
func (r *ResponsiveLayout) When(minWidth int, fn func(w, h int) string) *ResponsiveLayout {
	r.breakpoints = append(r.breakpoints, breakpoint{minWidth: minWidth, fn: fn})
	return r
}

// Default sets the fallback render function when no breakpoint matches.
func (r *ResponsiveLayout) Default(fn func(w, h int) string) *ResponsiveLayout {
	r.defaultFn = fn
	return r
}

// Render evaluates breakpoints from highest to lowest. The first breakpoint
// where width >= minWidth wins. If none match, the Default is used.
// Returns empty string if no breakpoint matches and no Default is set.
func (r *ResponsiveLayout) Render() string {
	// Sort breakpoints descending by minWidth so highest match wins.
	slices.SortFunc(r.breakpoints, func(a, b breakpoint) int {
		return b.minWidth - a.minWidth // descending
	})

	for _, bp := range r.breakpoints {
		if r.width >= bp.minWidth {
			return bp.fn(r.width, r.height)
		}
	}

	if r.defaultFn != nil {
		return r.defaultFn(r.width, r.height)
	}
	return ""
}
