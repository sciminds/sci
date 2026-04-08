package convert

import (
	"strings"
	"testing"
)

func TestStripFrontmatterBody(t *testing.T) {
	input := "+++ {\"test\": true}\n---\ntitle: Test\nauthor: Test\ndate: 2024-01-01\n---\nContent here"
	got := stripFrontmatterBody(input)
	if strings.Contains(got, "title: Test") {
		t.Error("frontmatter body not stripped")
	}
	if !strings.Contains(got, "Content here") {
		t.Error("content should be preserved")
	}
}

func TestStripMarimoMetadata(t *testing.T) {
	input := "before\n+++ {\"marimo\": {\"disabled\": true}}\nafter"
	got := stripMarimoMetadata(input)
	if strings.Contains(got, "marimo") {
		t.Error("marimo metadata not stripped")
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Error("surrounding content should be preserved")
	}
}

func TestNormalizeCodeLang(t *testing.T) {
	input := "```{code-cell} ipython3\nprint('hello')\n```"
	got := normalizeCodeLang(input)
	if !strings.Contains(got, "{code-cell} python") {
		t.Error("ipython3 not normalized to python")
	}
}

func TestConvertCallouts(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "titled callout",
			input:  `:::{.callout-warning title="Be careful"}`,
			expect: ":::{admonition} Be careful\n:class: warning",
		},
		{
			name:   "titleless callout",
			input:  `:::{.callout-note}`,
			expect: `:::{note}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertCallouts(tt.input)
			if !strings.Contains(got, tt.expect) {
				t.Errorf("expected %q in output, got: %q", tt.expect, got)
			}
		})
	}
}

func TestTagImportsCell(t *testing.T) {
	input := "```{code-cell} python\nimport marimo as mo\nimport numpy as np\n```\n\n```{code-cell} python\nx = 1\n```"
	got := tagImportsCell(input)
	if !strings.Contains(got, ":tags: [remove-cell]") {
		t.Error("first cell should be tagged")
	}
	if strings.Contains(got, "import marimo as mo") {
		t.Error("import marimo as mo should be removed from first cell")
	}
	if !strings.Contains(got, "import numpy as np") {
		t.Error("other imports should be preserved")
	}
}

func TestFixupsAfterToMystFromMarimo(t *testing.T) {
	input := "+++ {\"marimo\": {}}\nsome content\n```{code-cell} ipython3\nimport marimo as mo\nimport numpy as np\n```"
	got := FixupsAfterToMyst(input, true)
	if strings.Contains(got, "ipython3") {
		t.Error("ipython3 should be normalized")
	}
	if strings.Contains(got, "+++ {") {
		t.Error("marimo metadata should be stripped")
	}
}

func TestFixupsAfterToMystNotFromMarimo(t *testing.T) {
	input := "```{code-cell} ipython3\nx = 1\n```\n:::{.callout-note}\nContent\n:::"
	got := FixupsAfterToMyst(input, false)
	if strings.Contains(got, "ipython3") {
		t.Error("ipython3 should be normalized")
	}
	if !strings.Contains(got, ":::{note}") {
		t.Error("callouts should be converted")
	}
}
