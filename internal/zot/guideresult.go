package zot

import (
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// GuideEntry is one task-oriented "I want to do X → run Y" cheat-sheet line.
type GuideEntry struct {
	// Goal is the agent-facing intent ("Find papers in my library on X").
	Goal string `json:"goal"`
	// Cmd is a runnable example invocation. Tests verify every Cmd's first
	// `sci zot …` token resolves to a real command.
	Cmd string `json:"cmd"`
	// Note is a short hint about applicability, tradeoffs, or follow-ups.
	Note string `json:"note,omitempty"`
}

// GuideSection groups related entries (e.g. "discovery", "extraction").
type GuideSection struct {
	Title   string       `json:"title"`
	Entries []GuideEntry `json:"entries"`
}

// GuideResult is returned by `sci zot guide`. Token-budgeted to ~50 lines
// of human output / ~2KB JSON so an agent can pull it once at session
// start without bloating context.
type GuideResult struct {
	Sections []GuideSection `json:"sections"`
	Tip      string         `json:"tip,omitempty"`
}

// JSON implements cmdutil.Result.
func (r GuideResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r GuideResult) Human() string {
	var b strings.Builder
	b.WriteByte('\n')
	for _, sec := range r.Sections {
		b.WriteString("  ")
		b.WriteString(uikit.TUI.Bold().Render(sec.Title))
		b.WriteString("\n\n")
		for _, e := range sec.Entries {
			b.WriteString("    ")
			b.WriteString(uikit.TUI.Dim().Render("# "))
			b.WriteString(uikit.TUI.Dim().Render(e.Goal))
			b.WriteByte('\n')
			b.WriteString("    ")
			b.WriteString(uikit.TUI.TextBlue().Render(e.Cmd))
			b.WriteByte('\n')
			if e.Note != "" {
				b.WriteString("      ")
				b.WriteString(uikit.TUI.Dim().Render(e.Note))
				b.WriteByte('\n')
			}
			b.WriteByte('\n')
		}
	}
	if r.Tip != "" {
		b.WriteString("  ")
		b.WriteString(uikit.SymArrow)
		b.WriteByte(' ')
		b.WriteString(r.Tip)
		b.WriteByte('\n')
	}
	return b.String()
}
