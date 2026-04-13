package uikit

import (
	"strings"
	"testing"
)

// ── Render ────────────────────────────────────────────────────────────

func TestOverlayBoxRenderContainsTitle(t *testing.T) {
	o := OverlayBox{
		Title: "My Modal",
		Body:  "Hello world",
		Hints: []string{"esc close", "space play"},
	}
	out := o.Render(80)
	if !strings.Contains(out, "My Modal") {
		t.Error("output should contain the title")
	}
}

func TestOverlayBoxRenderContainsBody(t *testing.T) {
	o := OverlayBox{
		Title: "Test",
		Body:  "unique body content",
		Hints: []string{"q quit"},
	}
	out := o.Render(80)
	if !strings.Contains(out, "unique body content") {
		t.Error("output should contain the body")
	}
}

func TestOverlayBoxRenderContainsHints(t *testing.T) {
	o := OverlayBox{
		Title: "Test",
		Body:  "x",
		Hints: []string{"esc close", "space play"},
	}
	out := o.Render(80)
	if !strings.Contains(out, "esc close") {
		t.Error("output should contain hint 'esc close'")
	}
	if !strings.Contains(out, "space play") {
		t.Error("output should contain hint 'space play'")
	}
}

func TestOverlayBoxNoHints(t *testing.T) {
	o := OverlayBox{
		Title: "Minimal",
		Body:  "content",
	}
	out := o.Render(80)
	if out == "" {
		t.Fatal("Render should not be empty")
	}
	if !strings.Contains(out, "Minimal") {
		t.Error("output should contain the title")
	}
}

func TestOverlayBoxNarrowTerminal(t *testing.T) {
	o := OverlayBox{
		Title: "Test",
		Body:  "x",
		Hints: []string{"esc close"},
	}
	// Should not panic on very narrow terminal.
	out := o.Render(20)
	if out == "" {
		t.Fatal("Render should not be empty even for narrow terminal")
	}
}

func TestOverlayBoxNonEmpty(t *testing.T) {
	o := OverlayBox{
		Title: "T",
		Body:  "B",
	}
	if out := o.Render(100); out == "" {
		t.Error("Render should not be empty")
	}
}
