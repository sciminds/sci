//go:build linux

package main

// doctor_linux.go — slim doctor Action body for Linux. Runs the preflight
// (uv, git, shell) + identity checks, prompts for missing git identity, and
// returns. No brew, no Brewfile, no install side effects: package managers
// (apt/dnf/pacman) vary across distros, so we surface what's wrong and
// trust the user to fix it.

import (
	"context"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/doctor"
	"github.com/urfave/cli/v3"
)

func runDoctorCheck(_ context.Context, cmd *cli.Command) error {
	isJSON := cmdutil.IsJSON(cmd)

	if err := applyGitIdentityFlags(); err != nil {
		return err
	}

	var result doctor.DocResult
	result.Sections = doctor.RunPreflightIdentity()

	if isJSON {
		cmdutil.Output(cmd, result)
		if !result.AllPassed() {
			return cli.Exit("", 1)
		}
		return nil
	}

	cmdutil.Output(cmd, result)

	if err := promptGitIdentity(result); err != nil {
		return err
	}

	if result.AllPassed() {
		printAllSet()
	}
	return nil
}
