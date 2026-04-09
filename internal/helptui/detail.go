package helptui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/ui"
)

// RenderDetail renders the detail pane for a subcommand.
func RenderDetail(sub SubCommand, width, height int) string {
	var b strings.Builder

	// Description
	b.WriteString(ui.TUI.TextBright().Bold(true).Render(sub.Usage))
	b.WriteString("\n\n")

	// Usage line
	b.WriteString(ui.TUI.HelpSection().Render("Usage"))
	b.WriteString("\n")
	usage := sub.FullName
	if sub.ArgsUsage != "" {
		usage += " " + sub.ArgsUsage
	}
	b.WriteString("  " + ui.TUI.HelpUsage().Render(usage))
	b.WriteString("\n")

	// Flags
	if len(sub.Flags) > 0 {
		b.WriteString("\n")
		b.WriteString(ui.TUI.HelpSection().Render("Flags"))
		b.WriteString("\n")
		pad := maxFlagWidth(sub.Flags) + 2
		for _, f := range sub.Flags {
			name := ui.TUI.TextBright().Render(rpad(f.Names, pad))
			desc := ui.TUI.TextMid().Render(f.Usage)
			b.WriteString("  " + name + desc + "\n")
		}
	}

	// Examples
	if sub.Examples != "" {
		b.WriteString("\n")
		b.WriteString(ui.TUI.HelpSection().Render("Examples"))
		b.WriteString("\n")
		for _, line := range strings.Split(sub.Examples, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "":
				b.WriteString("\n")
			case strings.HasPrefix(trimmed, "$"):
				b.WriteString("  " + ui.TUI.HelpUsage().Render(trimmed) + "\n")
			case strings.HasPrefix(trimmed, "#"):
				b.WriteString("  " + ui.TUI.Dim().Render(trimmed) + "\n")
			default:
				b.WriteString("  " + ui.TUI.Dim().Render(trimmed) + "\n")
			}
		}
	}

	// Cast hint
	b.WriteString("\n")
	if sub.CastFile != "" {
		b.WriteString(ui.TUI.HeaderHint().Render("enter") + " " + ui.TUI.Dim().Render("play demo"))
	} else {
		b.WriteString(ui.TUI.Dim().Render("no demo available"))
	}

	content := b.String()

	// Wrap in a bordered box
	box := lipgloss.NewStyle().
		PaddingLeft(1).
		PaddingRight(1).
		Width(width)

	return box.Render(content)
}

func maxFlagWidth(flags []Flag) int {
	max := 0
	for _, f := range flags {
		if l := len(f.Names); l > max {
			max = l
		}
	}
	return max
}

func rpad(s string, w int) string {
	return fmt.Sprintf("%-*s", w, s)
}
