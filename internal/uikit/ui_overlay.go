package uikit

// ui_overlay.go — scrollable modal panel (help, detail views) rendered on top
// of other content. The parent model composites it via [Compose].

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── overlaySearch — shared search state for scrollable overlays ─────────

// overlaySearch holds live-search state for [Overlay] and [MarkdownOverlay].
// It mirrors the searchState in mdview but is embedded inside the overlay
// types so consumers get in-overlay search for free.
type overlaySearch struct {
	searching  bool
	input      textinput.Model
	query      string
	matchCount int
	matchLines []int
	matchIdx   int
}

func newOverlaySearch() overlaySearch {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 128
	return overlaySearch{input: ti}
}

// focus enters search mode, clearing the input.
func (s *overlaySearch) focus() tea.Cmd {
	s.searching = true
	s.input.SetValue("")
	return s.input.Focus()
}

// clear exits search mode, resets matches, and restores the viewport content.
func (s *overlaySearch) clear(vp *viewport.Model, rendered string) {
	s.searching = false
	s.query = ""
	s.matchCount = 0
	s.matchLines = nil
	s.matchIdx = 0
	vp.SetContent(rendered)
}

// confirm exits search mode and locks the current query as the highlight.
func (s *overlaySearch) confirm(vp *viewport.Model, rendered string) {
	s.searching = false
	s.query = s.input.Value()
	s.matchLines, s.matchCount, s.matchIdx = overlayApplySearch(s.query, rendered, vp)
}

// liveUpdate updates the search input and re-highlights on change.
func (s *overlaySearch) liveUpdate(msg tea.Msg, vp *viewport.Model, rendered string) tea.Cmd {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if q := s.input.Value(); q != s.query {
		s.query = q
		s.matchLines, s.matchCount, s.matchIdx = overlayApplySearch(s.query, rendered, vp)
	}
	return cmd
}

// nextMatch cycles to the next match and scrolls the viewport.
func (s *overlaySearch) nextMatch(vp *viewport.Model) {
	if len(s.matchLines) > 0 {
		s.matchIdx = (s.matchIdx + 1) % len(s.matchLines)
		vp.SetYOffset(s.matchLines[s.matchIdx])
	}
}

// prevMatch cycles to the previous match and scrolls the viewport.
func (s *overlaySearch) prevMatch(vp *viewport.Model) {
	if len(s.matchLines) > 0 {
		s.matchIdx = (s.matchIdx - 1 + len(s.matchLines)) % len(s.matchLines)
		vp.SetYOffset(s.matchLines[s.matchIdx])
	}
}

// overlaySearchAction is the result of handleKey — tells the caller what
// happened so it can short-circuit or fall through to the viewport.
type overlaySearchAction int

const (
	searchPassthrough overlaySearchAction = iota // not a search key — let viewport handle it
	searchHandled                                // consumed, no cmd
	searchCmd                                    // consumed, cmd returned via out param
)

// handleKey processes a key press and returns what the overlay should do.
// When the action is searchCmd, cmd holds the tea.Cmd to return.
func (s *overlaySearch) handleKey(msg tea.KeyPressMsg, vp *viewport.Model, rendered string) (overlaySearchAction, tea.Cmd) {
	if s.searching {
		switch msg.String() {
		case KeyEnter:
			s.confirm(vp, rendered)
			return searchHandled, nil
		case KeyEsc:
			s.clear(vp, rendered)
			return searchHandled, nil
		}
		return searchCmd, s.liveUpdate(msg, vp, rendered)
	}
	switch msg.String() {
	case "/":
		return searchCmd, s.focus()
	case KeyN:
		s.nextMatch(vp)
		return searchHandled, nil
	case "N":
		s.prevMatch(vp)
		return searchHandled, nil
	case "g", "t":
		vp.SetYOffset(0)
		return searchHandled, nil
	case "G", "b":
		vp.SetYOffset(vp.TotalLineCount())
		return searchHandled, nil
	}
	return searchPassthrough, nil
}

// overlayApplySearch finds all case-insensitive matches of query tokens in
// rendered, updates the viewport content with highlights, and returns the
// match state. The query is phrase-aware ([TokenizeQuery]) so `/foo bar`
// highlights foo and bar independently while `/"foo bar"` highlights only
// contiguous phrases — same tokenizer semantics as the row-level phrase
// search in dbtui.
func overlayApplySearch(query, rendered string, vp *viewport.Model) (matchLines []int, matchCount, matchIdx int) {
	tokens := TokenizeQuery(query)
	if len(tokens) == 0 {
		vp.SetContent(rendered)
		return nil, 0, 0
	}

	plain := ansi.Strip(rendered)
	lowerPlain := strings.ToLower(plain)

	// Dedupe match line numbers across tokens (so n/N cycles each region once).
	lineSet := map[int]bool{}
	for _, tok := range tokens {
		lowerTok := strings.ToLower(tok)
		if lowerTok == "" {
			continue
		}
		start := 0
		for {
			idx := strings.Index(lowerPlain[start:], lowerTok)
			if idx < 0 {
				break
			}
			begin := start + idx
			line := strings.Count(lowerPlain[:begin], "\n")
			lineSet[line] = true
			start = begin + len(lowerTok)
		}
	}

	if len(lineSet) > 0 {
		matchLines = slices.Sorted(maps.Keys(lineSet))
	}

	matchCount = len(matchLines)
	if matchCount > 0 {
		vp.SetContent(HighlightMatchesTokens(rendered, tokens))
		vp.SetYOffset(matchLines[0])
	} else {
		vp.SetContent(rendered)
	}
	return matchLines, matchCount, 0
}

// overlayDims computes the overlay box width, inner content width, and
// viewport body height from terminal dimensions and rendered content.
func overlayDims(rendered string, termW, termH int) (boxW, innerW, bodyH int) {
	boxW = OverlayWidth(termW, OverlayMinW, OverlayMaxW)
	innerW = boxW - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}
	maxBodyH := OverlayBodyHeight(termH, 0)
	contentLines := strings.Count(rendered, "\n") + 1
	bodyH = contentLines
	if bodyH > maxBodyH {
		bodyH = maxBodyH
	}
	if bodyH < OverlayMinH {
		bodyH = OverlayMinH
	}
	return boxW, innerW, bodyH
}

// overlayApplyResize sets viewport dimensions, content, and re-applies any
// active search highlights. Shared by Overlay.Resize and MarkdownOverlay.Resize.
func overlayApplyResize(vp *viewport.Model, s *overlaySearch, rendered string, innerW, bodyH int) {
	vp.SetWidth(innerW)
	vp.SetHeight(bodyH)
	vp.SetContent(rendered)
	if s.query != "" {
		s.matchLines, s.matchCount, s.matchIdx = overlayApplySearch(s.query, rendered, vp)
	}
}

// Overlay is a scrollable content panel rendered as a modal over other content.
// The parent model is responsible for compositing via [Compose].
type Overlay struct {
	title    string
	content  string // raw content (pre-wrap) — retained for resize
	rendered string // word-wrapped — retained for search restore
	vp       viewport.Model
	width    int // 0 until sized
	search   overlaySearch
}

// OverlayOption configures an [Overlay] or [MarkdownOverlay] at construction.
type OverlayOption func(*overlayConfig)

type overlayConfig struct {
	initialQuery string
}

// WithInitialQuery seeds the overlay's /-search with the given query so the
// first rendered frame already shows token highlights — used when the caller
// has an active row-search and wants the preview's highlights to align.
func WithInitialQuery(q string) OverlayOption {
	return func(c *overlayConfig) { c.initialQuery = q }
}

func applyOverlayOptions(opts []OverlayOption) overlayConfig {
	var c overlayConfig
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// seedInitialQuery commits the initial query into the overlay's search state
// and applies highlights, so the first View() already shows them without
// waiting for a / keystroke.
func seedInitialQuery(s *overlaySearch, vp *viewport.Model, rendered, q string) {
	if q == "" {
		return
	}
	s.query = q
	s.matchLines, s.matchCount, s.matchIdx = overlayApplySearch(q, rendered, vp)
}

// NewOverlay creates an auto-sized overlay. The viewport height shrinks to
// fit short content so there is no empty space.
func NewOverlay(title, content string, termW, termH int, opts ...OverlayOption) Overlay {
	cfg := applyOverlayOptions(opts)
	innerW := OverlayWidth(termW, OverlayMinW, OverlayMaxW) - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}
	wrapped := WordWrap(content, innerW)
	boxW, _, bodyH := overlayDims(wrapped, termW, termH)

	vp := viewport.New(viewport.WithWidth(innerW), viewport.WithHeight(bodyH))
	vp.SetContent(wrapped)

	o := Overlay{title: title, content: content, rendered: wrapped, vp: vp, width: boxW, search: newOverlaySearch()}
	seedInitialQuery(&o.search, &o.vp, wrapped, cfg.initialQuery)
	return o
}

// Resize recalculates the overlay dimensions for the given terminal size,
// re-wrapping content and adjusting the viewport height.
func (o Overlay) Resize(termW, termH int) Overlay {
	innerW := OverlayWidth(termW, OverlayMinW, OverlayMaxW) - OverlayBoxPadding
	if innerW < 1 {
		innerW = 1
	}
	wrapped := WordWrap(o.content, innerW)
	boxW, _, bodyH := overlayDims(wrapped, termW, termH)

	o.width = boxW
	o.rendered = wrapped
	overlayApplyResize(&o.vp, &o.search, wrapped, innerW, bodyH)
	return o
}

// Searching returns true when the overlay's search input is focused.
func (o Overlay) Searching() bool { return o.search.searching }

// Update delegates key/mouse messages to the viewport for scrolling, and
// handles /‑search keys when the search bar is active.
func (o Overlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		if act, cmd := o.search.handleKey(km, &o.vp, o.rendered); act != searchPassthrough {
			return o, cmd
		}
	}
	var cmd tea.Cmd
	o.vp, cmd = o.vp.Update(msg)
	return o, cmd
}

// View renders the overlay box. The parent composites it over the background
// using [Compose] or [CenterOverlay].
func (o Overlay) View() string {
	return renderOverlayView(o.title, &o.vp, o.width, &o.search)
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

// renderOverlayView is the shared rendering logic for scrollable overlay panels.
// Both [Overlay] and [MarkdownOverlay] delegate their View() to this function.
func renderOverlayView(title string, vp *viewport.Model, width int, s *overlaySearch) string {
	if width == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(TUI.HeaderSection().Render(" " + title + " "))
	b.WriteString("\n\n")
	b.WriteString(vp.View())
	b.WriteString("\n\n")

	if s != nil && s.searching {
		b.WriteString(s.input.View())
		if s.query != "" {
			if s.matchCount > 0 {
				b.WriteString("  ")
				b.WriteString(TUI.HeaderHint().Render(
					fmt.Sprintf("%d of %d", s.matchIdx+1, s.matchCount)))
			} else {
				b.WriteString("  ")
				b.WriteString(TUI.HeaderHint().Render("no matches"))
			}
		}
	} else {
		footer := overlayFooter(vp, s)
		b.WriteString(footer)
	}

	return TUI.OverlayBox().
		Width(width).
		Render(b.String())
}

// overlayFooter renders the status line for scrollable overlays.
func overlayFooter(vp *viewport.Model, s *overlaySearch) string {
	var parts []string

	if vp.TotalLineCount() > vp.VisibleLineCount() {
		pct := vp.ScrollPercent() * 100
		var pos string
		switch {
		case pct < 1:
			pos = "top"
		case pct > 99:
			pos = "end"
		default:
			pos = fmt.Sprintf("%d%%", int(pct))
		}
		parts = append(parts, TUI.HeaderHint().Render("↑↓ scroll"), TUI.HeaderHint().Render(pos))
	}

	if s != nil && s.query != "" && s.matchCount > 0 {
		parts = append(parts, TUI.HeaderHint().Render(
			fmt.Sprintf("n/N next/prev (%d of %d)", s.matchIdx+1, s.matchCount)))
	}

	parts = append(parts, TUI.HeaderHint().Render("t/b top/end"), TUI.HeaderHint().Render("/ search"), TUI.HeaderHint().Render("esc close"))
	return strings.Join(parts, "  ")
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
