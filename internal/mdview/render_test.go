package mdview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderBasicMarkdown(t *testing.T) {
	md := "# Hello\n\nSome **bold** text."
	out, err := Render(md, 80)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "Hello") {
		t.Error("rendered output should contain heading text")
	}
}

func TestRenderCodeBlock(t *testing.T) {
	md := "```python\nprint('hello')\n```"
	out, err := Render(md, 80)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "print") {
		t.Error("rendered output should contain code block content")
	}
}

func TestRenderNarrowWidth(t *testing.T) {
	md := "This is a long line that should be wrapped when the width is very narrow."
	out, err := Render(md, 20)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out == "" {
		t.Error("rendered output should not be empty")
	}
}

// ── HighlightMatches tests ──────────────────────────────────────────────────

func TestHighlightMatchesPlainText(t *testing.T) {
	out := HighlightMatches("hello world hello", "hello")
	if !strings.Contains(out, hlOn) {
		t.Error("output should contain highlight-on escape")
	}
	if !strings.Contains(out, hlOff) {
		t.Error("output should contain highlight-off escape")
	}
	// Plain text should be preserved.
	if ansi.Strip(out) != "hello world hello" {
		t.Errorf("stripping ANSI should recover original text, got %q", ansi.Strip(out))
	}
}

func TestHighlightMatchesCaseInsensitive(t *testing.T) {
	out := HighlightMatches("Hello HELLO hello", "hello")
	// All three occurrences should be highlighted (3 on + 3 off).
	if strings.Count(out, hlOn) != 3 {
		t.Errorf("expected 3 highlight-on sequences, got %d", strings.Count(out, hlOn))
	}
}

func TestHighlightMatchesEmptyQuery(t *testing.T) {
	styled := "\x1b[1mhello\x1b[0m"
	out := HighlightMatches(styled, "")
	if out != styled {
		t.Error("empty query should return input unchanged")
	}
}

func TestHighlightMatchesNoMatch(t *testing.T) {
	styled := "\x1b[1mhello\x1b[0m"
	out := HighlightMatches(styled, "xyz")
	if out != styled {
		t.Error("no-match query should return input unchanged")
	}
}

func TestHighlightMatchesPreservesANSI(t *testing.T) {
	// Simulate glamour output: bold "hello" with reset.
	styled := "\x1b[1mhello\x1b[0m world"
	out := HighlightMatches(styled, "hello")
	// The original ANSI sequences should still be present.
	if !strings.Contains(out, "\x1b[1m") {
		t.Error("original bold sequence should be preserved")
	}
	if ansi.Strip(out) != "hello world" {
		t.Errorf("plain text should be preserved, got %q", ansi.Strip(out))
	}
}

func TestHighlightMatchesSpansAcrossANSI(t *testing.T) {
	// Match spans across an ANSI reset mid-word.
	styled := "\x1b[1mhel\x1b[0mlo world"
	out := HighlightMatches(styled, "hello")
	if !strings.Contains(out, hlOn) {
		t.Error("match spanning ANSI boundaries should still be highlighted")
	}
	// After the reset inside the match, highlight should be re-asserted.
	// Check: hlOn appears after the \x1b[0m
	resetIdx := strings.Index(out, "\x1b[0m")
	afterReset := out[resetIdx+len("\x1b[0m"):]
	if !strings.HasPrefix(afterReset, hlOn) {
		t.Error("highlight should be re-asserted after SGR reset within a match")
	}
}

func TestAnsiSeqLen(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"\x1b[1m", 4},           // CSI bold
		{"\x1b[0m", 4},           // CSI reset
		{"\x1b[38;5;196m", 11},   // CSI 256-color
		{"\x1b]0;title\x07", 10}, // OSC with BEL
		{"\x1b(B", 2},            // two-byte sequence
		{"hello", 0},             // not an escape
	}
	for _, tt := range tests {
		got := ansiSeqLen(tt.input)
		if got != tt.want {
			t.Errorf("ansiSeqLen(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
