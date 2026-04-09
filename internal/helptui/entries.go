package helptui

import (
	"strings"

	"github.com/urfave/cli/v3"
)

// CommandGroup represents a top-level command in the picker (level 0).
type CommandGroup struct {
	Name     string
	Desc     string
	Category string
	FullName string       // e.g. "sci cloud"
	Subs     []SubCommand // empty for leaf commands
}

func (g CommandGroup) Title() string       { return g.Name }
func (g CommandGroup) Description() string { return g.Desc }
func (g CommandGroup) FilterValue() string { return g.Name + " " + g.Desc }

// SubCommand represents a subcommand entry shown in the level 1 list.
type SubCommand struct {
	Name      string
	Usage     string
	FullName  string // e.g. "sci cloud put"
	ArgsUsage string
	Flags     []Flag
	Examples  string // raw Description field
	CastFile  string // empty if no cast available
}

func (s SubCommand) Title() string       { return s.Name }
func (s SubCommand) Description() string { return s.Usage }
func (s SubCommand) FilterValue() string { return s.Name + " " + s.Usage }

// Flag holds simplified flag info for the detail pane.
type Flag struct {
	Names string
	Usage string
}

// BuildGroups extracts CommandGroups from the root cli.Command.
func BuildGroups(root *cli.Command) []CommandGroup {
	var groups []CommandGroup
	for _, cmd := range root.Commands {
		if cmd.Hidden || cmd.Name == "help" {
			continue
		}
		g := CommandGroup{
			Name:     cmd.Name,
			Desc:     cmd.Usage,
			Category: cmd.Category,
			FullName: cmd.FullName(),
		}
		for _, sub := range cmd.Commands {
			if sub.Hidden || sub.Name == "help" {
				continue
			}
			castName := cmd.Name + "-" + sub.Name + ".cast"
			cast := ""
			if hasCast(castName) {
				cast = castName
			}
			g.Subs = append(g.Subs, SubCommand{
				Name:      sub.Name,
				Usage:     sub.Usage,
				FullName:  sub.FullName(),
				ArgsUsage: sub.ArgsUsage,
				Flags:     extractFlags(sub),
				Examples:  sub.Description,
				CastFile:  cast,
			})
		}
		// For leaf commands (no subcommands), create a single synthetic
		// SubCommand representing the command itself so level 1 works.
		if len(g.Subs) == 0 && cmd.Action != nil {
			castName := cmd.Name + ".cast"
			cast := ""
			if hasCast(castName) {
				cast = castName
			}
			g.Subs = append(g.Subs, SubCommand{
				Name:      cmd.Name,
				Usage:     cmd.Usage,
				FullName:  cmd.FullName(),
				ArgsUsage: cmd.ArgsUsage,
				Flags:     extractFlags(cmd),
				Examples:  cmd.Description,
				CastFile:  cast,
			})
		}
		groups = append(groups, g)
	}
	return groups
}

func extractFlags(cmd *cli.Command) []Flag {
	var flags []Flag
	for _, f := range cmd.VisibleFlags() {
		names := f.Names()
		if len(names) == 0 {
			continue
		}
		// Skip the global help flag.
		if names[0] == "help" {
			continue
		}
		flags = append(flags, Flag{
			Names: formatFlagNames(names),
			Usage: flagUsage(f),
		})
	}
	return flags
}

func formatFlagNames(names []string) string {
	var parts []string
	for _, n := range names {
		if len(n) == 1 {
			parts = append(parts, "-"+n)
		} else {
			parts = append(parts, "--"+n)
		}
	}
	return strings.Join(parts, ", ")
}

func flagUsage(f cli.Flag) string {
	if df, ok := f.(cli.DocGenerationFlag); ok {
		return df.GetUsage()
	}
	return ""
}

// FindGroup returns the group matching name, or nil.
func FindGroup(groups []CommandGroup, name string) *CommandGroup {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}

// GroupsByCategory returns groups ordered by the standard category order.
func GroupsByCategory(groups []CommandGroup) []CommandGroup {
	order := map[string]int{
		"What Can I Do?":  0,
		"Getting Started": 1,
		"Commands":        2,
		"Maintenance":     3,
		"Experimental":    4,
	}
	out := make([]CommandGroup, len(groups))
	copy(out, groups)
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			oi := order[out[i].Category]
			oj := order[out[j].Category]
			if oi > oj || (oi == oj && out[i].Name > out[j].Name) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
