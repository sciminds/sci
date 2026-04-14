package uikit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNewMarkdownOverlay(t *testing.T) {
	t.Parallel()
	o := NewMarkdownOverlay("Test", "# Hello\n\nWorld.", 80, 40)
	view := o.View()
	if view == "" {
		t.Fatal("View() should not be empty")
	}
	if !strings.Contains(view, "Test") {
		t.Error("View() should contain the title")
	}
	if !strings.Contains(view, "esc close") {
		t.Error("View() should contain the close hint")
	}
}

func TestMarkdownOverlayRendersMarkdown(t *testing.T) {
	t.Parallel()
	o := NewMarkdownOverlay("Paper", "# Heading\n\n**bold text**", 80, 40)
	view := o.View()
	if !strings.Contains(view, "Heading") {
		t.Error("View() should contain rendered heading text")
	}
}

func TestMarkdownOverlayResize(t *testing.T) {
	t.Parallel()
	o := NewMarkdownOverlay("Test", "# Hello\n\nContent here.", 80, 40)
	o2 := o.Resize(120, 50)
	view := o2.View()
	if view == "" {
		t.Fatal("View() after Resize should not be empty")
	}
	if !strings.Contains(view, "Hello") {
		t.Error("View() after Resize should still contain content")
	}
}

func TestMarkdownOverlayUpdate(t *testing.T) {
	t.Parallel()
	o := NewMarkdownOverlay("Test", "# Hello\n\nContent.", 80, 40)
	o2, cmd := o.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	// Should not panic and should return the overlay.
	_ = cmd
	if o2.View() == "" {
		t.Error("View() after Update should not be empty")
	}
}

func TestMarkdownOverlayZeroWidth(t *testing.T) {
	t.Parallel()
	o := MarkdownOverlay{} // zero value
	if o.View() != "" {
		t.Error("zero-value MarkdownOverlay should render empty")
	}
}

func TestMarkdownOverlayFallsBackOnBadMarkdown(t *testing.T) {
	t.Parallel()
	// Plain text should still render without error.
	o := NewMarkdownOverlay("Plain", "just some plain text", 80, 40)
	view := o.View()
	stripped := ansi.Strip(view)
	if !strings.Contains(stripped, "plain text") {
		t.Errorf("plain text should pass through even without markdown formatting, got %q", stripped)
	}
}

func TestMarkdownOverlayRawContent(t *testing.T) {
	t.Parallel()
	md := "# Hello\n\nWorld."
	o := NewMarkdownOverlay("Test", md, 80, 40)
	if o.RawContent() != md {
		t.Errorf("RawContent() = %q, want %q", o.RawContent(), md)
	}
}
