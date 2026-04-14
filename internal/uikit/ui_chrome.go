package uikit

// Chrome renders a three-part vertical layout: title bar, body, and
// status bar — the standard TUI "chrome" that wraps every screen.
//
// The body callback receives the remaining height after title and status
// are measured, so callers never need to compute chrome offsets manually.
// The result is always exactly height lines tall.
type Chrome struct {
	// Title renders the title bar. Receives available width.
	Title func(width int) string
	// Status renders the status bar. Receives available width.
	Status func(width int) string
	// Body renders the main content area. Receives available width and
	// the remaining height after title and status are measured.
	Body func(width, height int) string
}

// Render composes the three sections vertically, padding or truncating
// the body so the total output is exactly height lines tall.
//
// Internally delegates to [VStack] with Fixed title, Flex body, Fixed status.
func (c Chrome) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return VStack(width, height).
		Fixed(c.Title).
		Flex(1, c.Body).
		Fixed(c.Status).
		Render()
}
