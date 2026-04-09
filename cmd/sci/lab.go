package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/lab"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	putDryRun bool
	setupUser string
)

func labCommand() *cli.Command {
	return &cli.Command{
		Name:        "lab",
		Usage:       "Access university lab storage (SFTP)",
		Description: "$ sci lab ls\n$ sci lab get data/results.csv\n$ sci lab put results.csv",
		Category:    "Commands",
		Commands: []*cli.Command{
			labSetupCommand(),
			labLsCommand(),
			labGetCommand(),
			labPutCommand(),
			labBrowseCommand(),
		},
	}
}

func labSetupCommand() *cli.Command {
	return &cli.Command{
		Name:        "setup",
		Usage:       "Configure SSH access to lab storage",
		Description: "$ sci lab setup\n$ sci lab setup --user myname",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "user", Aliases: []string{"u"}, Usage: "SSH username (required in --json mode)", Destination: &setupUser, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			user := setupUser

			if cmdutil.IsJSON(cmd) && user == "" {
				return fmt.Errorf("--user is required in --json mode")
			}

			if user == "" {
				if err := huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("SSH username").
						Description("Your UCSD username for " + lab.Host).
						Value(&user),
				)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
					return err
				}
			}
			if err := lab.ValidateUser(user); err != nil {
				return err
			}

			result, err := lab.Setup(user)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			if result.OK {
				ui.NextStep("sci lab ls", "Browse lab storage")
			}
			return nil
		},
	}
}

func labLsCommand() *cli.Command {
	return &cli.Command{
		Name:        "ls",
		Aliases:     []string{"list"},
		Usage:       "List remote directory contents",
		Description: "$ sci lab ls\n$ sci lab ls data/experiment",
		ArgsUsage:   "[path]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, err := lab.RequireConfig()
			if err != nil {
				return err
			}

			rel := cmd.Args().First() // empty string → lab root
			remotePath, err := lab.SafeReadPath(rel)
			if err != nil {
				return err
			}

			argv := lab.BuildLsArgs(cfg, remotePath)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					return fmt.Errorf("ls %s: timed out — check your VPN/network connection", remotePath)
				}
				return fmt.Errorf("ls %s: %s", remotePath, string(out))
			}

			result := &lab.LsResult{Path: remotePath, Raw: string(out)}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func labGetCommand() *cli.Command {
	return &cli.Command{
		Name:        "get",
		Usage:       "Download a file or directory from lab storage",
		Description: "$ sci lab get data/results.csv\n$ sci lab get data/experiment/ ./local/",
		ArgsUsage:   "<remote> [local]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, err := lab.RequireConfig()
			if err != nil {
				return err
			}
			if cmd.Args().Len() < 1 {
				return cmdutil.UsageErrorf(cmd, "expected at least 1 argument, got %d", cmd.Args().Len())
			}

			remotePath, err := lab.SafeReadPath(cmd.Args().Get(0))
			if err != nil {
				return err
			}

			localPath := "."
			if cmd.Args().Len() >= 2 {
				localPath = cmd.Args().Get(1)
			}

			argv := lab.BuildGetArgs(cfg, remotePath, localPath)
			bin, err := exec.LookPath(argv[0])
			if err != nil {
				return fmt.Errorf("rsync not found in PATH")
			}
			return syscall.Exec(bin, argv, os.Environ())
		},
	}
}

func labPutCommand() *cli.Command {
	return &cli.Command{
		Name:        "put",
		Usage:       "Upload a file or directory to your lab space",
		Description: "$ sci lab put results.csv\n$ sci lab put results.csv experiment/results.csv\n$ sci lab put results.csv --dry-run",
		ArgsUsage:   "<local> [remote]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Aliases: []string{"n"}, Usage: "show what would be transferred without uploading", Destination: &putDryRun, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, err := lab.RequireConfig()
			if err != nil {
				return err
			}
			if cmd.Args().Len() < 1 {
				return cmdutil.UsageErrorf(cmd, "expected at least 1 argument, got %d", cmd.Args().Len())
			}

			localPath := cmd.Args().Get(0)
			if _, err := os.Stat(localPath); err != nil {
				return fmt.Errorf("local file not found: %s", localPath)
			}

			// Default remote name = basename of local file.
			rel := filepath.Base(localPath)
			if cmd.Args().Len() >= 2 {
				rel = cmd.Args().Get(1)
			}

			remotePath, err := lab.SafeWritePath(cfg, rel)
			if err != nil {
				return err
			}

			argv := lab.BuildPutArgs(cfg, localPath, remotePath, putDryRun)
			bin, err := exec.LookPath(argv[0])
			if err != nil {
				return fmt.Errorf("rsync not found in PATH")
			}
			return syscall.Exec(bin, argv, os.Environ())
		},
	}
}

func labBrowseCommand() *cli.Command {
	return &cli.Command{
		Name:        "browse",
		Usage:       "Open an SSH shell in lab storage",
		Description: "$ sci lab browse",
		Action: func(_ context.Context, _ *cli.Command) error {
			cfg, err := lab.RequireConfig()
			if err != nil {
				return err
			}

			argv := lab.BuildOpenArgs(cfg)
			bin, err := exec.LookPath(argv[0])
			if err != nil {
				return fmt.Errorf("ssh not found in PATH")
			}
			return syscall.Exec(bin, argv, os.Environ())
		},
	}
}
