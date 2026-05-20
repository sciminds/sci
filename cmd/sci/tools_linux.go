//go:build linux

package main

// tools_linux.go — `sci tools` is entirely brew/uv-via-brew shaped on macOS
// and there's no clean Linux equivalent (apt/dnf/pacman are out of scope).
// The stub returns nil so [buildRoot]'s lo.Compact can filter it out before
// urfave/cli sees the command list.

import "github.com/urfave/cli/v3"

func toolsCommand() *cli.Command { return nil }
