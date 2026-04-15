package uikit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ── ParseCast tests ─────────────────────────────────────────────────────────

var sampleCast = []byte(`{"version": 2, "width": 80, "height": 24}
[0.5, "o", "$ "]
[1.0, "o", "ls\r\n"]
[1.5, "o", "file1.txt  file2.txt\r\n"]
`)

func TestParseCast(t *testing.T) {
	t.Parallel()
	c, err := ParseCast(sampleCast)
	if err != nil {
		t.Fatalf("ParseCast: %v", err)
	}

	if c.Header.Version != 2 {
		t.Errorf("version = %d, want 2", c.Header.Version)
	}
	if c.Header.Width != 80 {
		t.Errorf("width = %d, want 80", c.Header.Width)
	}
	if c.Header.Height != 24 {
		t.Errorf("height = %d, want 24", c.Header.Height)
	}

	if len(c.Events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(c.Events))
	}

	if c.Events[0].Time != 0.5 {
		t.Errorf("events[0].Time = %f, want 0.5", c.Events[0].Time)
	}
	if c.Events[0].Data != "$ " {
		t.Errorf("events[0].Data = %q, want %q", c.Events[0].Data, "$ ")
	}

	if c.Events[2].Time != 1.5 {
		t.Errorf("events[2].Time = %f, want 1.5", c.Events[2].Time)
	}
	if c.Events[2].Data != "file1.txt  file2.txt\r\n" {
		t.Errorf("events[2].Data = %q", c.Events[2].Data)
	}
}

func TestParseCastFiltersNonOutput(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version": 2, "width": 80, "height": 24}
[0.5, "o", "$ "]
[0.8, "i", "l"]
[1.0, "o", "ls\r\n"]
`)
	c, err := ParseCast(data)
	if err != nil {
		t.Fatalf("ParseCast: %v", err)
	}
	if len(c.Events) != 2 {
		t.Errorf("len(events) = %d, want 2 (input events should be filtered)", len(c.Events))
	}
}

func TestParseCastErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte("")},
		{"no header", []byte(`[0.5, "o", "hello"]`)},
		{"malformed header", []byte(`not json`)},
		{"header only", []byte(`{"version": 2, "width": 80, "height": 24}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCast(tt.input)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParseCastSkipsBlankLines(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version": 2, "width": 80, "height": 24}

[0.5, "o", "hello"]

[1.0, "o", "world"]
`)
	c, err := ParseCast(data)
	if err != nil {
		t.Fatalf("ParseCast: %v", err)
	}
	if len(c.Events) != 2 {
		t.Errorf("len(events) = %d, want 2", len(c.Events))
	}
}

// ── CastPlayer model tests ──────────────────────────────────────────────────

func testCast() Cast {
	c, _ := ParseCast(sampleCast)
	return c
}

func TestCastPlayerInit(t *testing.T) {
	t.Parallel()
	p := NewCastPlayer(testCast(), 20)

	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init should return a tick cmd")
	}
	if p.output != "" {
		t.Errorf("output should be empty initially, got %q", p.output)
	}
	if p.current != 0 {
		t.Errorf("current = %d, want 0", p.current)
	}
}

func TestCastPlayerAdvance(t *testing.T) {
	t.Parallel()
	p := NewCastPlayer(testCast(), 20)

	p, cmd := p.Update(CastTickMsg{Index: 0})
	if p.output != "$ " {
		t.Errorf("output after tick 0 = %q, want %q", p.output, "$ ")
	}
	if p.current != 1 {
		t.Errorf("current = %d, want 1", p.current)
	}
	if cmd == nil {
		t.Error("should schedule next tick")
	}
	if p.finished {
		t.Error("should not be finished after first event")
	}
}

func TestCastPlayerPause(t *testing.T) {
	t.Parallel()
	p := NewCastPlayer(testCast(), 20)

	p, _ = p.Update(CastTickMsg{Index: 0})

	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if !p.paused {
		t.Fatal("should be paused after space")
	}

	outputBefore := p.output
	p, cmd := p.Update(CastTickMsg{Index: 1})
	if p.output != outputBefore {
		t.Error("output should not change while paused")
	}
	if cmd != nil {
		t.Error("should not schedule tick while paused")
	}

	p, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if p.paused {
		t.Error("should be unpaused after second space")
	}
	if cmd == nil {
		t.Error("should schedule next tick on unpause")
	}
}

func TestCastPlayerRestart(t *testing.T) {
	t.Parallel()
	p := NewCastPlayer(testCast(), 20)

	p, _ = p.Update(CastTickMsg{Index: 0})
	p, _ = p.Update(CastTickMsg{Index: 1})
	if p.current != 2 {
		t.Fatalf("current = %d, want 2", p.current)
	}

	p, cmd := p.Update(tea.KeyPressMsg{Text: "r"})
	if p.current != 0 {
		t.Errorf("current = %d, want 0 after restart", p.current)
	}
	if p.output != "" {
		t.Errorf("output = %q, want empty after restart", p.output)
	}
	if p.finished {
		t.Error("should not be finished after restart")
	}
	if p.paused {
		t.Error("should not be paused after restart")
	}
	if cmd == nil {
		t.Error("should schedule first tick after restart")
	}
}

func TestCastPlayerFinished(t *testing.T) {
	t.Parallel()
	p := NewCastPlayer(testCast(), 20)

	for i := range p.cast.Events {
		p, _ = p.Update(CastTickMsg{Index: i})
	}

	if !p.finished {
		t.Error("should be finished after all events")
	}
	if p.current != len(p.cast.Events) {
		t.Errorf("current = %d, want %d", p.current, len(p.cast.Events))
	}

	want := "$ " + "ls\r\n" + "file1.txt  file2.txt\r\n"
	if p.output != want {
		t.Errorf("output = %q, want %q", p.output, want)
	}
}

func TestCastPlayerViewContainsOutput(t *testing.T) {
	t.Parallel()
	p := NewCastPlayer(testCast(), 20)
	p, _ = p.Update(CastTickMsg{Index: 0})
	p, _ = p.Update(CastTickMsg{Index: 1})

	view := p.View()
	if !strings.Contains(view, "ls") {
		t.Error("view should contain 'ls'")
	}
	if !strings.Contains(view, "playing") {
		t.Error("view should show 'playing' status")
	}
	if !strings.Contains(view, "2/3") {
		t.Errorf("view should show progress '2/3', got:\n%s", view)
	}
}

func TestCastPlayerViewTailCap(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version": 2, "width": 80, "height": 24}
[0.1, "o", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"]
`)
	c, _ := ParseCast(data)
	p := NewCastPlayer(c, 3)

	p, _ = p.Update(CastTickMsg{Index: 0})
	view := p.View()

	if strings.Contains(view, "line1\n") {
		t.Error("line1 should be scrolled off with height=3")
	}
	if !strings.Contains(view, "line10") {
		t.Error("line10 should be visible")
	}
}
