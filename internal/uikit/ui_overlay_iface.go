// ui_overlay_iface.go — common interface for scrollable overlay panels.

package uikit

import tea "charm.land/bubbletea/v2"

// ScrollableOverlay is the common interface satisfied by both [Overlay]
// (plain text) and [MarkdownOverlay] (glamour-rendered markdown). It
// allows consumer code to hold either type polymorphically.
//
// The Update and Resize methods return ScrollableOverlay (not the concrete
// type) so the interface is self-contained. Both [Overlay] and
// [MarkdownOverlay] implement these via wrapper methods that delegate to
// their concrete equivalents.
//
// Searching reports whether the overlay's /‑search input is active. Parents
// should check this before intercepting esc — when true, esc exits search
// rather than closing the overlay.
type ScrollableOverlay interface {
	View() string
	UpdateOverlay(msg tea.Msg) (ScrollableOverlay, tea.Cmd)
	ResizeOverlay(termW, termH int) ScrollableOverlay
	Searching() bool
}

// UpdateOverlay implements ScrollableOverlay for Overlay.
func (o Overlay) UpdateOverlay(msg tea.Msg) (ScrollableOverlay, tea.Cmd) {
	o2, cmd := o.Update(msg)
	return o2, cmd
}

// ResizeOverlay implements ScrollableOverlay for Overlay.
func (o Overlay) ResizeOverlay(termW, termH int) ScrollableOverlay {
	return o.Resize(termW, termH)
}

// UpdateOverlay implements ScrollableOverlay for MarkdownOverlay.
func (o MarkdownOverlay) UpdateOverlay(msg tea.Msg) (ScrollableOverlay, tea.Cmd) {
	o2, cmd := o.Update(msg)
	return o2, cmd
}

// ResizeOverlay implements ScrollableOverlay for MarkdownOverlay.
func (o MarkdownOverlay) ResizeOverlay(termW, termH int) ScrollableOverlay {
	return o.Resize(termW, termH)
}
