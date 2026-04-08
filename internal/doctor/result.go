package doctor

// result.go — [DocResult] implements [cmdutil.Result] (JSON + Human output)
// for the `sci doctor` command.

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// DocResult is the top-level result returned by `sci doctor`.
// It implements [cmdutil.Result] via [JSON] and [Human].
type DocResult struct {
	Sections []CheckSection
	Tools    []ToolInfo
}

type jsonCheck struct {
	Label   string `json:"label"`
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type jsonSection struct {
	Name   string      `json:"name"`
	Checks []jsonCheck `json:"checks"`
}

type jsonSummary struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Warn int `json:"warn"`
}

type jsonTool struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type jsonDocResult struct {
	Sections []jsonSection `json:"sections"`
	Summary  jsonSummary   `json:"summary"`
	Tools    []jsonTool    `json:"tools,omitempty"`
}

// JSON returns the structured output for --json mode.
func (r DocResult) JSON() any {
	pass, fail, warn := 0, 0, 0
	sections := make([]jsonSection, 0, len(r.Sections))
	for _, sec := range r.Sections {
		js := jsonSection{Name: sec.Name}
		for _, c := range sec.Checks {
			js.Checks = append(js.Checks, jsonCheck(c))
			switch c.Status {
			case StatusFail:
				fail++
			case StatusWarn:
				warn++
			default:
				pass++
			}
		}
		sections = append(sections, js)
	}
	var tools []jsonTool
	for _, t := range r.Tools {
		tools = append(tools, jsonTool(t))
	}

	return jsonDocResult{
		Sections: sections,
		Summary:  jsonSummary{Pass: pass, Fail: fail, Warn: warn},
		Tools:    tools,
	}
}

// Human returns the styled terminal output.
func (r DocResult) Human() string {
	var b strings.Builder
	pass, fail, warn := 0, 0, 0

	for _, sec := range r.Sections {
		fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Bold().Render(sec.Name))
		for _, c := range sec.Checks {
			sym := ui.SymOK
			switch c.Status {
			case StatusFail:
				sym = ui.SymFail
				fail++
			case StatusWarn:
				sym = ui.SymWarn
				warn++
			default:
				pass++
			}
			fmt.Fprintf(&b, "    %s %-20s %s\n", sym, c.Label, ui.TUI.Dim().Render(c.Message))
		}
	}

	// Tools summary (one line instead of full list)
	if len(r.Tools) > 0 {
		installed := 0
		for _, t := range r.Tools {
			if t.Installed {
				installed++
			}
		}
		sym := ui.SymOK
		if installed < len(r.Tools) {
			sym = ui.SymFail
		}
		fmt.Fprintf(&b, "\n  %s\n", ui.TUI.Bold().Render("Tools"))
		fmt.Fprintf(&b, "    %s %-20s %s\n", sym, "installed",
			ui.TUI.Dim().Render(fmt.Sprintf("%d/%d", installed, len(r.Tools))))
	}

	// Summary line — only shown when there are problems.
	if fail > 0 || warn > 0 {
		var parts []string
		if fail > 0 {
			parts = append(parts, ui.TUI.Fail().Render(fmt.Sprintf("%d failed", fail)))
		}
		if warn > 0 {
			parts = append(parts, ui.TUI.Warn().Render(fmt.Sprintf("%d warnings", warn)))
		}
		fmt.Fprintf(&b, "\n  %s\n", strings.Join(parts, ui.TUI.Dim().Render(" · ")))
	}
	fmt.Fprintln(&b)

	return b.String()
}

// AllPassed returns true if no checks failed.
func (r DocResult) AllPassed() bool {
	for _, sec := range r.Sections {
		for _, c := range sec.Checks {
			if c.Status == StatusFail {
				return false
			}
		}
	}
	return true
}
