package uikit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/lo"
)

// ── Asciicast v2 types ──────────────────────────────────────────────────────

// CastHeader is the first line of an asciicast v2 recording.
type CastHeader struct {
	Version int `json:"version"`
	Width   int `json:"width"`
	Height  int `json:"height"`
}

// CastEvent is a single output event: [time, "o", data].
type CastEvent struct {
	Time float64
	Data string
}

// Cast holds a parsed asciicast v2 recording.
type Cast struct {
	Header CastHeader
	Events []CastEvent
}

// ParseCast parses asciicast v2 format (JSON-lines: header object + event arrays).
func ParseCast(data []byte) (Cast, error) {
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) == 0 {
		return Cast{}, errors.New("empty cast data")
	}

	var headerLine []byte
	var rest [][]byte
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) > 0 {
			headerLine = line
			rest = lines[i+1:]
			break
		}
	}
	if headerLine == nil {
		return Cast{}, errors.New("empty cast data")
	}

	var h CastHeader
	if err := json.Unmarshal(headerLine, &h); err != nil {
		return Cast{}, fmt.Errorf("parse header: %w", err)
	}

	var events []CastEvent
	for _, line := range rest {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		var raw []json.RawMessage
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			continue
		}
		if len(raw) < 3 {
			continue
		}

		var t float64
		if err := json.Unmarshal(raw[0], &t); err != nil {
			continue
		}

		var typ string
		if err := json.Unmarshal(raw[1], &typ); err != nil {
			continue
		}
		if typ != "o" {
			continue
		}

		var d string
		if err := json.Unmarshal(raw[2], &d); err != nil {
			continue
		}

		events = append(events, CastEvent{Time: t, Data: d})
	}

	if len(events) == 0 {
		return Cast{}, errors.New("no output events found")
	}

	return Cast{Header: h, Events: events}, nil
}

// ── CastPlayer model ────────────────────────────────────────────────────────

const castMaxDelay = 2 * time.Second

// CastTickMsg advances playback to the given event index.
type CastTickMsg struct{ Index int }

// CastPlayer is a bubbletea sub-model that plays back an asciicast recording.
type CastPlayer struct {
	cast     Cast
	current  int
	output   string
	paused   bool
	finished bool
	height   int
	width    int
}

// NewCastPlayer creates a player for the given cast recording.
func NewCastPlayer(c Cast, visibleHeight int) *CastPlayer {
	return &CastPlayer{
		cast:   c,
		height: visibleHeight,
	}
}

// Cast returns the underlying cast recording (for layout callers that need
// the recorded width).
func (p *CastPlayer) Cast() Cast { return p.cast }

// Init starts playback by scheduling the first event.
func (p *CastPlayer) Init() tea.Cmd {
	return p.scheduleNext()
}

// Update handles ticks and key controls.
func (p *CastPlayer) Update(msg tea.Msg) (*CastPlayer, tea.Cmd) {
	switch msg := msg.(type) {
	case CastTickMsg:
		if p.paused || p.finished || msg.Index != p.current {
			return p, nil
		}
		p.output += p.cast.Events[p.current].Data
		p.current++
		if p.current >= len(p.cast.Events) {
			p.finished = true
			return p, nil
		}
		return p, p.scheduleNext()

	case tea.KeyPressMsg:
		switch msg.String() {
		case KeySpace:
			p.paused = !p.paused
			if !p.paused && !p.finished {
				return p, p.scheduleNext()
			}
			return p, nil
		case "r":
			p.current = 0
			p.output = ""
			p.finished = false
			p.paused = false
			return p, p.scheduleNext()
		}
	}

	return p, nil
}

// View renders the current playback output, tail-capped and padded to visible height.
func (p *CastPlayer) View() string {
	lines := strings.Split(p.output, "\n")
	if p.height > 0 && len(lines) > p.height {
		lines = lines[len(lines)-p.height:]
	}
	for len(lines) < p.height {
		lines = append(lines, "")
	}
	// Strip carriage returns: they survive ansi.Truncate (width 0) but the
	// terminal interprets them as cursor-rewind at render time, clobbering
	// the overlay's left border and shifting the bottom down by a row.
	lines = lo.Map(lines, func(line string, _ int) string {
		return strings.ReplaceAll(line, "\r", "")
	})
	if p.width > 0 {
		lines = lo.Map(lines, func(line string, _ int) string {
			return ansi.Truncate(line, p.width, "")
		})
	}

	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\n")

	progress := fmt.Sprintf("%d/%d", p.current, len(p.cast.Events))
	var status string
	switch {
	case p.finished:
		status = TUI.TextGreen().Render("done")
	case p.paused:
		status = TUI.TextOrange().Render("paused")
	default:
		status = TUI.TextBlue().Render("playing")
	}
	b.WriteString(status + "  " + TUI.Dim().Render(progress))

	return b.String()
}

// SetHeight updates the visible line cap (body rows, not counting the
// built-in status line).
func (p *CastPlayer) SetHeight(h int) { p.height = h }

// SetWidth updates the visible column cap. Lines exceeding this width are
// truncated in View() to prevent overflow into adjacent panels.
func (p *CastPlayer) SetWidth(w int) { p.width = w }

// SetSize implements ScrollPanel: h is the total rows available; the player
// reserves 2 rows for its internal status line (blank + "playing N/M").
func (p *CastPlayer) SetSize(w, h int) {
	body := h - 2
	if body < 1 {
		body = 1
	}
	p.height = body
	p.width = w
}

// Handle implements ScrollPanel.
func (p *CastPlayer) Handle(msg tea.Msg) tea.Cmd {
	_, cmd := p.Update(msg)
	return cmd
}

// Footer implements ScrollPanel. Renders playback-control hints, trimming
// to fit the given width.
func (p *CastPlayer) Footer(width int) string {
	const sep = "  "

	escPart := TUI.HeaderHint().Render("q/esc close")
	escW := lipgloss.Width(escPart)

	hints := []string{"space pause/play", "r restart"}

	budget := width - escW
	var parts []string
	for _, h := range hints {
		rendered := TUI.HeaderHint().Render(h)
		w := lipgloss.Width(rendered) + len(sep)
		if budget-w < 0 {
			break
		}
		parts = append(parts, rendered)
		budget -= w
	}
	parts = append(parts, escPart)
	return strings.Join(parts, sep)
}

// CapturesInput implements ScrollPanel: CastPlayer never captures input.
func (p *CastPlayer) CapturesInput() bool { return false }

// OwnsKey implements ScrollPanel: player claims space (pause/play) and r (restart).
func (p *CastPlayer) OwnsKey(key string) bool { return key == KeySpace || key == "r" }

// PreferredWidth implements ScrollPanel: the recorded cast width.
func (p *CastPlayer) PreferredWidth() int { return p.cast.Header.Width }

func (p *CastPlayer) scheduleNext() tea.Cmd {
	if p.current >= len(p.cast.Events) {
		return nil
	}
	var delay time.Duration
	e := p.cast.Events[p.current]
	if p.current == 0 {
		delay = time.Duration(e.Time * float64(time.Second))
	} else {
		delay = time.Duration((e.Time - p.cast.Events[p.current-1].Time) * float64(time.Second))
	}
	if delay > castMaxDelay {
		delay = castMaxDelay
	}
	if delay < 0 {
		delay = 0
	}
	idx := p.current
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return CastTickMsg{Index: idx}
	})
}
