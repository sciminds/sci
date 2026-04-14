// ui_mdoverlay.go — scrollable modal panel that renders markdown via glamour.
// Same compositing contract as [Overlay] but uses [RenderMarkdown] instead of
// plain word-wrap, producing syntax-highlighted headings, bold, lists, etc.

package uikit

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// MarkdownOverlay is a scrollable content panel that renders markdown via
// glamour. The parent model composites it via [Compose] or [CenterOverlay],
// exactly like [Overlay].
type MarkdownOverlay struct {
	title    string
	markdown string // raw markdown — retained for resize and RawContent
	vp       viewport.Model
	width    int // 0 until sized
}

// NewMarkdownOverlay creates an auto-sized markdown overlay. The content is
// rendered via glamour at the appropriate width; the viewport height shrinks
// to fit short content so there is no empty space.
func NewMarkdownOverlay(title, markdown string, termW, termH int) MarkdownOverlay {
	w := OverlayWidth(termW, OverlayMinW, OverlayMaxW)
	innerW := w - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}

	rendered := renderMarkdownForOverlay(markdown, innerW)

	maxBodyH := OverlayBodyHeight(termH, 0)
	contentLines := strings.Count(rendered, "\n") + 1
	bodyH := contentLines
	if bodyH > maxBodyH {
		bodyH = maxBodyH
	}
	if bodyH < OverlayMinH {
		bodyH = OverlayMinH
	}

	vp := viewport.New(viewport.WithWidth(innerW), viewport.WithHeight(bodyH))
	vp.SetContent(rendered)

	return MarkdownOverlay{title: title, markdown: markdown, vp: vp, width: w}
}

// Resize recalculates the overlay dimensions for the given terminal size,
// re-rendering content at the new width.
func (o MarkdownOverlay) Resize(termW, termH int) MarkdownOverlay {
	w := OverlayWidth(termW, OverlayMinW, OverlayMaxW)
	innerW := w - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}

	rendered := renderMarkdownForOverlay(o.markdown, innerW)

	maxBodyH := OverlayBodyHeight(termH, 0)
	contentLines := strings.Count(rendered, "\n") + 1
	bodyH := contentLines
	if bodyH > maxBodyH {
		bodyH = maxBodyH
	}
	if bodyH < OverlayMinH {
		bodyH = OverlayMinH
	}

	o.width = w
	o.vp.SetWidth(innerW)
	o.vp.SetHeight(bodyH)
	o.vp.SetContent(rendered)

	return o
}

// Update delegates key/mouse messages to the viewport for scrolling.
func (o MarkdownOverlay) Update(msg tea.Msg) (MarkdownOverlay, tea.Cmd) {
	var cmd tea.Cmd
	o.vp, cmd = o.vp.Update(msg)
	return o, cmd
}

// View renders the overlay box. The parent composites it over the background
// using [Compose] or [CenterOverlay].
func (o MarkdownOverlay) View() string {
	return renderOverlayView(o.title, &o.vp, o.width)
}

// RawContent returns the original markdown source.
func (o MarkdownOverlay) RawContent() string { return o.markdown }

// renderMarkdownForOverlay renders markdown via glamour, falling back to
// plain word-wrap if glamour fails.
func renderMarkdownForOverlay(markdown string, width int) string {
	rendered, err := RenderMarkdown(markdown, width)
	if err != nil {
		return WordWrap(markdown, width)
	}
	return strings.TrimRight(rendered, "\n")
}
