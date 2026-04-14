package helptui

import (
	"strings"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"
)

// CommandGroup represents a top-level command in the picker (level 0).
type CommandGroup struct {
	Name     string
	Desc     string
	Category string
	FullName string       // e.g. "sci cloud"
	LongDesc string       // wiki-style overview shown above the subcommand list
	Subs     []SubCommand // empty for leaf commands
}

// Title implements list.DefaultItem.
func (g CommandGroup) Title() string { return g.Name }

// Description implements list.DefaultItem.
func (g CommandGroup) Description() string { return g.Desc }

// FilterValue implements list.DefaultItem.
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

// Title implements list.DefaultItem.
func (s SubCommand) Title() string { return s.Name }

// Description implements list.DefaultItem.
func (s SubCommand) Description() string { return s.Usage }

// FilterValue implements list.DefaultItem.
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
			LongDesc: longDescs[cmd.Name],
		}
		for _, sub := range cmd.Commands {
			if sub.Hidden || sub.Name == "help" {
				continue
			}
			// Flatten nested subcommands (e.g. cass canvas → canvas modules, canvas assignments, ...).
			// Use hasRealSubs to ignore the auto-injected "help" command from urfave/cli.
			if hasRealSubs(sub) {
				for _, child := range sub.Commands {
					if child.Hidden || child.Name == "help" {
						continue
					}
					castName := cmd.Name + "-" + sub.Name + "-" + child.Name + ".cast"
					cast := ""
					if hasCast(castName) {
						cast = castName
					}
					g.Subs = append(g.Subs, SubCommand{
						Name:      sub.Name + " " + child.Name,
						Usage:     child.Usage,
						FullName:  child.FullName(),
						ArgsUsage: child.ArgsUsage,
						Flags:     extractFlags(child),
						Examples:  child.Description,
						CastFile:  cast,
					})
				}
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

// hasRealSubs reports whether cmd has subcommands beyond the auto-injected "help".
func hasRealSubs(cmd *cli.Command) bool {
	return lo.ContainsBy(cmd.Commands, func(c *cli.Command) bool {
		return !c.Hidden && c.Name != "help"
	})
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

// FindGroup returns the group matching name, or nil.
func FindGroup(groups []CommandGroup, name string) *CommandGroup {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}
