package mdview

import (
	"strings"
	"testing"
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
