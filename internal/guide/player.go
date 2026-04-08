package guide

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/ui"
)

// ── Asciicast v2 types ──────────────────────────────────────────────────────

// Header is the first line of an asciicast v2 recording.
type Header struct {
	Version int `json:"version"`
	Width   int `json:"width"`
	Height  int `json:"height"`
}

// Event is a single output event: [time, "o", data].
type Event struct {
	Time float64
	Data string
}

// Cast holds a parsed asciicast v2 recording.
type Cast struct {
	Header Header
	Events []Event
}

// ParseCast parses asciicast v2 format (JSON-lines: header object + event arrays).
func ParseCast(data []byte) (Cast, error) {
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) == 0 {
		return Cast{}, errors.New("empty cast data")
	}

	// Parse header (first non-blank line).
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

	var h Header
	if err := json.Unmarshal(headerLine, &h); err != nil {
		return Cast{}, fmt.Errorf("parse header: %w", err)
	}

	// Parse events.
	var events []Event
	for _, line := range rest {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		var raw []json.RawMessage
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			continue // skip malformed lines
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
			continue // only output events
		}

		var d string
		if err := json.Unmarshal(raw[2], &d); err != nil {
			continue
		}

		events = append(events, Event{Time: t, Data: d})
	}

	if len(events) == 0 {
		return Cast{}, errors.New("no output events found")
	}

	return Cast{Header: h, Events: events}, nil
}

// ── Player model ────────────────────────────────────────────────────────────

const maxDelay = 2 * time.Second

// tickMsg advances playback to the given event index.
type tickMsg struct{ index int }

// Player is a bubbletea sub-model that plays back an asciicast recording.
type Player struct {
	cast     Cast
	current  int    // next event index to apply
	output   string // accumulated output
	paused   bool
	finished bool
	height   int // max visible lines
}

// NewPlayer creates a player for the given cast recording.
func NewPlayer(c Cast, visibleHeight int) *Player {
	return &Player{
		cast:   c,
		height: visibleHeight,
	}
}

// Init starts playback by scheduling the first event.
func (p *Player) Init() tea.Cmd {
	return p.scheduleNext()
}

// Update handles ticks and key controls.
func (p *Player) Update(msg tea.Msg) (*Player, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if p.paused || p.finished || msg.index != p.current {
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
		case "space":
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
func (p *Player) View() string {
	lines := strings.Split(p.output, "\n")
	if p.height > 0 && len(lines) > p.height {
		lines = lines[len(lines)-p.height:]
	}
	// Pad to fixed height so the overlay doesn't resize during playback.
	for len(lines) < p.height {
		lines = append(lines, "")
	}

	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\n")

	// Status line
	progress := fmt.Sprintf("%d/%d", p.current, len(p.cast.Events))
	var status string
	switch {
	case p.finished:
		status = ui.TUI.FgSuccess().Render("done")
	case p.paused:
		status = ui.TUI.FgSecondary().Render("paused")
	default:
		status = ui.TUI.FgAccent().Render("playing")
	}
	b.WriteString(status + "  " + ui.TUI.Dim().Render(progress))

	return b.String()
}

// SetHeight updates the visible line cap.
func (p *Player) SetHeight(h int) {
	p.height = h
}

func (p *Player) scheduleNext() tea.Cmd {
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
	if delay > maxDelay {
		delay = maxDelay
	}
	if delay < 0 {
		delay = 0
	}
	idx := p.current
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return tickMsg{index: idx}
	})
}
