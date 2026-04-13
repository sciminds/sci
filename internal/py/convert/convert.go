// Package convert transforms notebook files between three formats using
// jupytext as the conversion engine:
//
//   - Marimo notebooks (.py) — Python scripts with cell markers
//   - MyST Markdown (.md) — Jupyter-compatible Markdown
//   - Quarto documents (.qmd) — Quarto's extended Markdown
//
// After jupytext conversion, format-specific fixup functions clean up
// artifacts: merging import cells, rewriting callout syntax, stripping
// frontmatter, and adjusting return tuples.
//
// Use [InferFormat] to detect the format from a file extension, then
// call Convert to transform between any pair.
package convert

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// Format represents a notebook format.
type Format string

// Supported notebook formats.
const (
	Marimo Format = "marimo"
	MyST   Format = "myst"
	Quarto Format = "quarto"
)

// InferFormat returns the format based on file extension.
func InferFormat(path string) (Format, error) {
	switch filepath.Ext(path) {
	case ".py":
		return Marimo, nil
	case ".md":
		return MyST, nil
	case ".qmd":
		return Quarto, nil
	default:
		return "", fmt.Errorf("unsupported file extension: %s (expected .py, .md, or .qmd)", filepath.Ext(path))
	}
}

// ConvertResult holds the output of a conversion.
type ConvertResult struct { //nolint:revive // name is established in the API
	Input      string `json:"input"`
	Output     string `json:"output"`
	FromFormat string `json:"from"`
	ToFormat   string `json:"to"`
}

// JSON implements cmdutil.Result.
func (r ConvertResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ConvertResult) Human() string {
	return fmt.Sprintf("  %s %s → %s\n", ui.SymOK, r.Input, r.Output)
}

// Convert converts input to output, inferring formats from extensions.
func Convert(input, output string) (*ConvertResult, error) {
	from, err := InferFormat(input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	to, err := InferFormat(output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	if from == to {
		return nil, fmt.Errorf("input and output formats are the same (%s)", from)
	}

	// Ensure uvx is available
	if _, err := exec.LookPath("uvx"); err != nil {
		return nil, fmt.Errorf("uvx not found — run %s to install it", ui.TUI.TextBlue().Render("sci doctor check"))
	}

	// Build jupytext args
	fromArg, toArg := jupytextArgs(from, to)
	cmd := exec.Command("uvx", "jupytext", "--from", fromArg, "--to", toArg, input, "--output", output)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("jupytext: %w", err)
	}

	// Read output and apply fixups
	data, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	text := string(data)

	switch to {
	case MyST:
		text = FixupsAfterToMyst(text, from == Marimo)
	case Quarto:
		text = FixupsAfterToQuarto(text, from == Marimo)
	case Marimo:
		text = FixupsAfterToMarimo(text)
	}

	if err := os.WriteFile(output, []byte(text), 0o644); err != nil {
		return nil, err
	}

	return &ConvertResult{
		Input:      input,
		Output:     output,
		FromFormat: string(from),
		ToFormat:   string(to),
	}, nil
}

func jupytextArgs(from, to Format) (string, string) {
	fromMap := map[Format]string{
		Marimo: "py:marimo",
		MyST:   "md:myst",
		Quarto: "qmd",
	}
	toMap := map[Format]string{
		Marimo: "py:marimo",
		MyST:   "myst",
		Quarto: "qmd",
	}
	return fromMap[from], toMap[to]
}

// CollapseBlankLines collapses 3+ consecutive blank lines to 2.
func CollapseBlankLines(text string) string {
	// Replace 3+ newlines with 2
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text
}
