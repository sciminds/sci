package ui

// overlay.go — scrollable modal panel (help, detail views) rendered on top of
// other content. The parent model composites it via [Compose].

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay is a scrollable content panel rendered as a modal over other content.
// The parent model is responsible for compositing via [Compose].
type Overlay struct {
	title   string
	content string // raw content (pre-wrap) — retained for resize
	vp      viewport.Model
	width   int // 0 until sized
}

// NewOverlay creates an auto-sized overlay. The viewport height shrinks to
// fit short content so there is no empty space.
func NewOverlay(title, content string, termW, termH int) Overlay {
	w := OverlayWidth(termW, OverlayMinW, OverlayMaxW)
	innerW := w - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}
	wrapped := WordWrap(content, innerW)

	maxBodyH := OverlayBodyHeight(termH, 0)
	contentLines := strings.Count(wrapped, "\n") + 1
	bodyH := contentLines
	if bodyH > maxBodyH {
		bodyH = maxBodyH
	}
	if bodyH < OverlayMinH {
		bodyH = OverlayMinH
	}

	vp := viewport.New(viewport.WithWidth(innerW), viewport.WithHeight(bodyH))
	vp.SetContent(wrapped)

	return Overlay{title: title, content: content, vp: vp, width: w}
}

// Resize recalculates the overlay dimensions for the given terminal size,
// re-wrapping content and adjusting the viewport height.
func (o Overlay) Resize(termW, termH int) Overlay {
	w := OverlayWidth(termW, OverlayMinW, OverlayMaxW)
	innerW := w - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}
	wrapped := WordWrap(o.content, innerW)

	maxBodyH := OverlayBodyHeight(termH, 0)
	contentLines := strings.Count(wrapped, "\n") + 1
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
	o.vp.SetContent(wrapped)

	return o
}

// Update delegates key/mouse messages to the viewport for scrolling.
func (o Overlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	var cmd tea.Cmd
	o.vp, cmd = o.vp.Update(msg)
	return o, cmd
}

// View renders the overlay box. The parent composites it over the background
// using [Compose] or [CenterOverlay].
func (o Overlay) View() string {
	if o.width == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(TUI.HeaderSection().Render(" " + o.title + " "))
	b.WriteString("\n\n")
	b.WriteString(o.vp.View())
	b.WriteString("\n\n")

	footer := TUI.HeaderHint().Render("esc close")
	if o.vp.TotalLineCount() > o.vp.VisibleLineCount() {
		pct := o.vp.ScrollPercent() * 100
		var pos string
		switch {
		case pct < 1:
			pos = "top"
		case pct > 99:
			pos = "end"
		default:
			pos = fmt.Sprintf("%d%%", int(pct))
		}
		footer = TUI.HeaderHint().Render("↑↓ scroll") + "  " +
			TUI.HeaderHint().Render(pos) + "  " + footer
	}
	b.WriteString(footer)

	return TUI.OverlayBox().
		Width(o.width).
		Render(b.String())
}

// ── Compositing helpers ─────────────────────────────────────────────────
// These are general-purpose and used by both the ui package and db/tui.

// CenterOverlay composites fg centered over bg. Both are newline-delimited
// strings of rendered terminal output.
func CenterOverlay(fg, bg string) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	bgH := len(bgLines)
	fgH := len(fgLines)

	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}
	bgW := 0
	for _, l := range bgLines {
		if w := lipgloss.Width(l); w > bgW {
			bgW = w
		}
	}

	startRow := (bgH - fgH) / 2
	startCol := (bgW - fgW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	result := make([]string, len(bgLines))
	copy(result, bgLines)

	for i, fgLine := range fgLines {
		row := startRow + i
		if row >= len(result) {
			break
		}
		bgLine := result[row]
		bgLineW := lipgloss.Width(bgLine)

		var b strings.Builder
		if startCol > 0 {
			if bgLineW >= startCol {
				b.WriteString(ansi.Truncate(bgLine, startCol, ""))
			} else {
				b.WriteString(bgLine)
				b.WriteString(strings.Repeat(" ", startCol-bgLineW))
			}
		}
		b.WriteString(fgLine)

		endCol := startCol + fgW
		if bgLineW > endCol {
			b.WriteString(ansi.TruncateLeft(bgLine, endCol, ""))
		}
		result[row] = b.String()
	}

	return strings.Join(result, "\n")
}

// DimBackground applies faint (SGR 2) to every line of s.
func DimBackground(s string) string {
	s = strings.ReplaceAll(s, "\033[0m", "\033[0;2m")
	s = strings.ReplaceAll(s, "\033[22m", "\033[2m")
	return prependLines(s, "\033[2m", "\033[0m")
}

// CancelFaint wraps each line with SGR 22 (cancel faint) so overlay text
// renders at normal intensity even when composited over a dimmed background.
func CancelFaint(s string) string {
	n := strings.Count(s, "\n")
	const pre = "\033[22m"
	const suf = "\033[2m"
	var b strings.Builder
	b.Grow(len(s) + (n+1)*(len(pre)+len(suf)))
	b.WriteString(pre)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		b.WriteString(suf)
		b.WriteByte('\n')
		b.WriteString(pre)
		s = s[i+1:]
	}
	b.WriteString(suf)
	return b.String()
}

// OverlayWidth computes the overlay content width given terminal width and
// constraints. It applies [OverlayMargin] of margin, then clamps to [minW, maxW].
func OverlayWidth(termW, minW, maxW int) int {
	w := termW - OverlayMargin
	if w < minW {
		w = minW
	}
	if w > maxW {
		w = maxW
	}
	return w
}

// Compose is a convenience for CenterOverlay(CancelFaint(fg), DimBackground(bg)).
func Compose(fg, bg string) string {
	return CenterOverlay(CancelFaint(fg), DimBackground(bg))
}

// WordWrap wraps text at maxW, preserving paragraph breaks (newlines).
func WordWrap(text string, maxW int) string {
	if maxW <= 0 || text == "" {
		return text
	}
	var result strings.Builder
	for _, paragraph := range strings.Split(text, "\n") {
		if result.Len() > 0 {
			result.WriteByte('\n')
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			continue
		}
		lineW := 0
		for i, word := range words {
			ww := lipgloss.Width(word)
			if i == 0 {
				result.WriteString(word)
				lineW = ww
				continue
			}
			if lineW+1+ww > maxW {
				result.WriteByte('\n')
				result.WriteString(word)
				lineW = ww
			} else {
				result.WriteByte(' ')
				result.WriteString(word)
				lineW += 1 + ww
			}
		}
	}
	return result.String()
}

// prependLines wraps every line of s with prefix/suffix.
func prependLines(s, prefix, suffix string) string {
	n := strings.Count(s, "\n")
	var b strings.Builder
	b.Grow(len(s) + (n+1)*len(prefix) + len(suffix))
	b.WriteString(prefix)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		b.WriteByte('\n')
		b.WriteString(prefix)
		s = s[i+1:]
	}
	b.WriteString(suffix)
	return b.String()
}
