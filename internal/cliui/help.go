package cliui

// help.go — styled help output for urfave/cli v3, replacing the default
// HelpPrinter with project-branded formatting.

import (
	"cmp"
	"fmt"

	"io"
	"slices"
	"strings"
	"sync"

	"github.com/sciminds/cli/internal/uikit"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"
)

// categoryOrder defines the display order for command categories in root help.
var categoryOrder = []string{"What Can I Do?", "Getting Started", "Commands", "Maintenance", "Experimental"}

var setupHelpOnce sync.Once

// SetupHelp configures styled help rendering on the root command.
// In urfave/cli v3 this is done by replacing the HelpPrinter function.
// Idempotent — safe to call concurrently from parallel tests, which is
// why the write is guarded by sync.Once (the race detector flags even
// same-value repeat writes to a package global).
func SetupHelp(root *cli.Command) {
	setupHelpOnce.Do(func() {
		cli.HelpPrinterCustom = renderHelpCustom
	})
}

func renderHelpCustom(w io.Writer, templ string, data any, fm map[string]any) {
	cmd, ok := data.(*cli.Command)
	if !ok {
		return
	}
	renderHelp(w, cmd)
}

func renderHelp(w io.Writer, c *cli.Command) {
	p := func(a ...any) { _, _ = fmt.Fprintln(w, a...) }
	pf := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

	isRoot := c.Root() == c

	// ── Banner (root only) ────────────────────────────────────────────────
	if isRoot {
		p()
		p(uikit.TUI.TextBlueBold().Render("  🔬🧠 sci") + " " + uikit.TUI.Dim().Render("— your scientific computing toolkit"))
	}

	// ── Description (skip for root — banner covers it) ───────────────────
	if !isRoot {
		desc := c.Usage
		if desc != "" {
			p()
			p(uikit.TUI.HelpDesc().Render(desc))
		}
	}

	// ── Usage ────────────────────────────────────────────────────────────
	p()
	p(uikit.TUI.HelpSection().Render("Usage"))
	if c.Action != nil {
		pf("    %s\n", uikit.TUI.HelpUsage().Render(buildUsageLine(c)))
	}
	if len(c.VisibleCommands()) > 0 {
		pf("    %s\n", uikit.TUI.HelpUsage().Render(c.FullName()+" <command>"))
	}

	// ── Flags ────────────────────────────────────────────────────────────
	localFlags := c.VisibleFlags()
	if len(localFlags) > 0 {
		pad := maxFlagNameLen(localFlags) + 2
		for _, f := range localFlags {
			name := flagNamePart(f)
			usage := flagUsage(f)
			padded := uikit.TUI.TextBright().Render(rpad("  "+name, pad+2))
			if usage != "" {
				pf("  %s%s\n", padded, uikit.TUI.TextMid().Render(usage))
			} else {
				pf("  %s\n", padded)
			}
		}
	}

	// ── Commands ──────────────────────────────────────────────────────────
	cmds := c.VisibleCommands()
	if len(cmds) > 0 {
		padding := maxNameLen(cmds) + 2

		// Collect categories in order.
		categories := c.VisibleCategories()
		if len(categories) > 1 || (len(categories) == 1 && categories[0].Name() != "") {
			// Index categories by name.
			catMap := make(map[string]cli.CommandCategory, len(categories))
			for _, cat := range categories {
				if cat.Name() != "" {
					catMap[cat.Name()] = cat
				}
			}
			// Print in explicit order, then any remaining.
			printed := make(map[string]bool)
			for _, name := range categoryOrder {
				cat, ok := catMap[name]
				if !ok {
					continue
				}
				printed[name] = true
				p()
				p(categoryHeading(name))
				sorted := sortedCommands(cat.VisibleCommands())
				for _, sub := range sorted {
					printCommandCli(w, sub, padding)
				}
			}
			for _, cat := range categories {
				if cat.Name() == "" || printed[cat.Name()] {
					continue
				}
				p()
				p(categoryHeading(cat.Name()))
				sorted := sortedCommands(cat.VisibleCommands())
				for _, sub := range sorted {
					printCommandCli(w, sub, padding)
				}
			}
		} else {
			p()
			p(uikit.TUI.HelpSection().Render("Commands"))
			sorted := sortedCommands(cmds)
			for _, sub := range sorted {
				printCommandCli(w, sub, padding)
			}
		}
	}

	// ── Examples (from Description) ──────────────────────────────────────
	if c.Description != "" {
		p()
		p(uikit.TUI.HelpSection().Render("Examples"))
		for _, line := range strings.Split(c.Description, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "":
				p()
			case strings.HasPrefix(trimmed, "#"):
				pf("    %s\n", uikit.TUI.Dim().Render(line))
			case strings.HasPrefix(trimmed, "$"):
				pf("    %s\n", uikit.TUI.HelpUsage().Render(line))
			default:
				pf("    %s\n", uikit.TUI.Dim().Render(line))
			}
		}
	}

	p()
}

func buildUsageLine(c *cli.Command) string {
	parts := []string{c.FullName()}
	if c.ArgsUsage != "" {
		parts = append(parts, c.ArgsUsage)
	}
	return strings.Join(parts, " ")
}

func printCommandCli(w io.Writer, cmd *cli.Command, padding int) {
	name := uikit.TUI.TextBlue().Render(rpad(cmd.Name, padding))
	desc := uikit.TUI.TextMid().Render(cmd.Usage)
	_, _ = fmt.Fprintf(w, "    %s%s\n", name, desc)
}

func flagNamePart(f cli.Flag) string {
	names := f.Names()
	parts := lo.Map(names, func(n string, _ int) string {
		return lo.Ternary(len(n) == 1, "-"+n, "--"+n)
	})
	return strings.Join(parts, ", ")
}

func flagUsage(f cli.Flag) string {
	if df, ok := f.(cli.DocGenerationFlag); ok {
		return df.GetUsage()
	}
	return ""
}

func sortedCommands(cmds []*cli.Command) []*cli.Command {
	out := slices.Clone(cmds)
	slices.SortFunc(out, func(a, b *cli.Command) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func maxFlagNameLen(flags []cli.Flag) int {
	max := 0
	for _, f := range flags {
		if l := len(flagNamePart(f)); l > max {
			max = l
		}
	}
	return max
}

func rpad(s string, padding int) string {
	f := fmt.Sprintf("%%-%ds", padding)
	return fmt.Sprintf(f, s)
}

func categoryHeading(name string) string {
	if name == "Experimental" {
		return uikit.TUI.TextPinkBold().Render(name)
	}
	return uikit.TUI.HelpSection().Render(name)
}

func maxNameLen(cmds []*cli.Command) int {
	max := 0
	for _, c := range cmds {
		if l := len(c.Name); l > max {
			max = l
		}
	}
	return max
}
