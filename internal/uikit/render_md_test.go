package uikit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderMarkdownBasic(t *testing.T) {
	t.Parallel()
	md := "# Hello\n\nSome **bold** text."
	out, err := RenderMarkdown(md, 80)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(out, "Hello") {
		t.Error("rendered output should contain heading text")
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	t.Parallel()
	md := "```python\nprint('hello')\n```"
	out, err := RenderMarkdown(md, 80)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(out, "print") {
		t.Error("rendered output should contain code block content")
	}
}

func TestRenderMarkdownNarrowWidth(t *testing.T) {
	t.Parallel()
	md := "This is a long line that should be wrapped when the width is very narrow."
	out, err := RenderMarkdown(md, 20)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if out == "" {
		t.Error("rendered output should not be empty")
	}
}

func TestRenderMarkdownCachesResults(t *testing.T) {
	t.Parallel()
	md := "# Cached\n\nContent here."
	out1, err := RenderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := RenderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
	if out1 != out2 {
		t.Error("repeated calls with same input should return identical output")
	}
}

func TestPreRenderMarkdown(t *testing.T) {
	t.Parallel()
	docs := []string{"# One", "# Two", "# Three"}
	PreRenderMarkdown(docs, 80)
	// After pre-render, each should be cached and return without error.
	for _, md := range docs {
		out, err := RenderMarkdown(md, 80)
		if err != nil {
			t.Fatalf("RenderMarkdown after PreRenderMarkdown: %v", err)
		}
		if out == "" {
			t.Errorf("pre-rendered doc %q should not be empty", md)
		}
	}
}

// ── HighlightMatches tests ──────────────────────────────────────────────────

func TestHighlightMatchesPlainText(t *testing.T) {
	t.Parallel()
	out := HighlightMatches("hello world hello", "hello")
	if !strings.Contains(out, hlOn) {
		t.Error("output should contain highlight-on escape")
	}
	if !strings.Contains(out, hlOff) {
		t.Error("output should contain highlight-off escape")
	}
	if ansi.Strip(out) != "hello world hello" {
		t.Errorf("stripping ANSI should recover original text, got %q", ansi.Strip(out))
	}
}

func TestHighlightMatchesCaseInsensitive(t *testing.T) {
	t.Parallel()
	out := HighlightMatches("Hello HELLO hello", "hello")
	if strings.Count(out, hlOn) != 3 {
		t.Errorf("expected 3 highlight-on sequences, got %d", strings.Count(out, hlOn))
	}
}

func TestHighlightMatchesEmptyQuery(t *testing.T) {
	t.Parallel()
	styled := "\x1b[1mhello\x1b[0m"
	out := HighlightMatches(styled, "")
	if out != styled {
		t.Error("empty query should return input unchanged")
	}
}

func TestHighlightMatchesNoMatch(t *testing.T) {
	t.Parallel()
	styled := "\x1b[1mhello\x1b[0m"
	out := HighlightMatches(styled, "xyz")
	if out != styled {
		t.Error("no-match query should return input unchanged")
	}
}

func TestHighlightMatchesPreservesANSI(t *testing.T) {
	t.Parallel()
	styled := "\x1b[1mhello\x1b[0m world"
	out := HighlightMatches(styled, "hello")
	if !strings.Contains(out, "\x1b[1m") {
		t.Error("original bold sequence should be preserved")
	}
	if ansi.Strip(out) != "hello world" {
		t.Errorf("plain text should be preserved, got %q", ansi.Strip(out))
	}
}

func TestHighlightMatchesSpansAcrossANSI(t *testing.T) {
	t.Parallel()
	styled := "\x1b[1mhel\x1b[0mlo world"
	out := HighlightMatches(styled, "hello")
	if !strings.Contains(out, hlOn) {
		t.Error("match spanning ANSI boundaries should still be highlighted")
	}
	resetIdx := strings.Index(out, "\x1b[0m")
	afterReset := out[resetIdx+len("\x1b[0m"):]
	if !strings.HasPrefix(afterReset, hlOn) {
		t.Error("highlight should be re-asserted after SGR reset within a match")
	}
}

func TestAnsiSeqLen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"\x1b[1m", 4},
		{"\x1b[0m", 4},
		{"\x1b[38;5;196m", 11},
		{"\x1b]0;title\x07", 10},
		{"\x1b(B", 2},
		{"hello", 0},
	}
	for _, tt := range tests {
		got := ansiSeqLen(tt.input)
		if got != tt.want {
			t.Errorf("ansiSeqLen(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDetectTermStyle(t *testing.T) {
	t.Parallel()
	// Just verify it doesn't panic and is callable.
	DetectTermStyle()
}
