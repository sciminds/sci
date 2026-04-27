package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/lab"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/tui/labtui"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

var (
	putDryRun bool
	setupUser string
)

// warmMaster is the SSH ControlMaster warmup hook. Indirecting through this
// var lets unit tests stub the warm step without spinning up real ssh; it
// also gives every lab subcommand that shells out to ssh/rsync a single
// place to opt into the "auth on the real terminal first, then exec" flow.
// Without this, a stale ControlMaster surprises the user with a mid-command
// Duo prompt — found while triaging issue #2.
var warmMaster = lab.WarmMaster

func labCommand() *cli.Command {
	return &cli.Command{
		Name:        "lab",
		Usage:       "Access university lab storage (SFTP)",
		Description: "$ sci lab ls\n$ sci lab get data/results.csv\n$ sci lab put results.csv",
		Category:    "Commands",
		Before: func(_ context.Context, _ *cli.Command) (context.Context, error) {
			if !netutil.Online() {
				return nil, fmt.Errorf("no internet connection — sci lab requires network access")
			}
			if err := lab.Preflight(); err != nil {
				return nil, err
			}
			return nil, nil
		},
		Commands: []*cli.Command{
			labSetupCommand(),
			labLsCommand(),
			labGetCommand(),
			labPutCommand(),
			labBrowseCommand(),
			labConnectCommand(),
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
				if err := uikit.InputInto(&user, "SSH username", "Your UCSD username for "+lab.Host); err != nil {
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
				uikit.NextStep("sci lab ls", "Browse lab storage")
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

			if err := warmMaster(cfg); err != nil {
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

			if err := warmMaster(cfg); err != nil {
				return err
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

			if err := warmMaster(cfg); err != nil {
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
		Usage:       "Interactively browse lab storage and download folders",
		Description: "$ sci lab browse",
		Action: func(_ context.Context, _ *cli.Command) error {
			cfg, err := lab.RequireConfig()
			if err != nil {
				return err
			}
			// Warm the ControlMaster on the real terminal so the TUI inherits
			// an authenticated connection — avoids Duo prompts hanging behind
			// the alt-screen on every internal ssh/rsync call.
			if err := warmMaster(cfg); err != nil {
				return err
			}
			if err := labtui.Run(cfg); err != nil {
				if errors.Is(err, labtui.ErrInterrupted) {
					return cli.Exit("", 130)
				}
				return err
			}
			return nil
		},
	}
}

func labConnectCommand() *cli.Command {
	return &cli.Command{
		Name:        "connect",
		Usage:       "Open an SSH shell in lab storage",
		Description: "$ sci lab connect",
		Action: func(_ context.Context, _ *cli.Command) error {
			cfg, err := lab.RequireConfig()
			if err != nil {
				return err
			}

			// Warm the ControlMaster so the exec'd SSH inherits an
			// authenticated connection — avoids hanging on a stale master.
			if err := warmMaster(cfg); err != nil {
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
