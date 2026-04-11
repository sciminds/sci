// Package cli builds the urfave/cli v3 command tree for Zotero operations.
//
// The tree is consumed by two entry points:
//   - cmd/zot (standalone `zot` binary)
//   - cmd/sci/zot.go (integrated `sci zot` subcommand)
//
// Both share identical behavior: any change here shows up in both surfaces.
package cli

import "github.com/urfave/cli/v3"

// Commands returns the full zot subcommand tree.
// Entry points wrap this in their own root cli.Command.
func Commands() []*cli.Command {
	cmds := []*cli.Command{setupCommand()}
	cmds = append(cmds, readCommands()...)
	cmds = append(cmds, writeCommands()...)
	return cmds
}
