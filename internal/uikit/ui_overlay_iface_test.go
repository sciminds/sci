package uikit

import "testing"

// Compile-time checks: both overlay types satisfy ScrollableOverlay.
var (
	_ ScrollableOverlay = Overlay{}
	_ ScrollableOverlay = MarkdownOverlay{}
)

func TestScrollableOverlayPlain(t *testing.T) {
	t.Parallel()
	var o ScrollableOverlay = NewOverlay("Test", "content", 80, 40)
	if o.View() == "" {
		t.Error("plain Overlay via ScrollableOverlay should render")
	}
}

func TestScrollableOverlayMarkdown(t *testing.T) {
	t.Parallel()
	var o ScrollableOverlay = NewMarkdownOverlay("Test", "# Hello", 80, 40)
	if o.View() == "" {
		t.Error("MarkdownOverlay via ScrollableOverlay should render")
	}
}
