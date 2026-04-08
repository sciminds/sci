package convert

import (
	"strings"
	"testing"
)

func TestConvertAdmonitions(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "generic with title and class",
			input:  ":::{admonition} Watch out\n:class: warning",
			expect: `:::{.callout-warning title="Watch out"}`,
		},
		{
			name:   "native shorthand",
			input:  ":::{note}",
			expect: ":::{.callout-note}",
		},
		{
			name:   "unknown type preserved",
			input:  ":::{custom-thing}",
			expect: ":::{custom-thing}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertAdmonitions(tt.input)
			if !strings.Contains(got, tt.expect) {
				t.Errorf("expected %q in output, got: %q", tt.expect, got)
			}
		})
	}
}

func TestSafeHorizontalRules(t *testing.T) {
	// Frontmatter should be preserved
	input := "---\ntitle: test\n---\nContent\n---\nMore content"
	got := safeHorizontalRules(input)
	// The frontmatter --- should stay
	if !strings.HasPrefix(got, "---\ntitle: test\n---\n") {
		t.Error("frontmatter should be preserved")
	}
	// Body --- should become ***
	if !strings.Contains(got, "\n***\n") {
		t.Errorf("body --- should become ***, got: %q", got)
	}
}

func TestTagFirstCellQuarto(t *testing.T) {
	input := "```{python}\nimport numpy as np\n```\n\n```{python}\nx = 1\n```"
	got := tagFirstCellQuarto(input)
	if !strings.Contains(got, "#| include: false") {
		t.Error("first cell should have include: false")
	}
	// Only the first cell should be tagged
	parts := strings.SplitN(got, "```{python}", 3)
	if len(parts) < 3 {
		t.Fatal("expected at least 2 python cells")
	}
	if strings.Contains(parts[2], "#| include: false") {
		t.Error("second cell should NOT have include: false")
	}
}

func TestStripMarimoCellMetadataQmd(t *testing.T) {
	input := "#| marimo: {\"disabled\": true}\nprint('hello')"
	got := stripMarimoCellMetadataQmd(input)
	if strings.Contains(got, "marimo") {
		t.Error("marimo metadata should be stripped")
	}
	if !strings.Contains(got, "print('hello')") {
		t.Error("code should be preserved")
	}
}

func TestFixupsAfterToQuartoFromMarimo(t *testing.T) {
	input := "+++ {\"marimo\": {}}\n```{python}\nimport marimo as mo\n```\n\n---\n\n:::{note}\nHi\n:::"
	got := FixupsAfterToQuarto(input, true)
	if strings.Contains(got, "import marimo") {
		t.Error("marimo import should be stripped")
	}
}
