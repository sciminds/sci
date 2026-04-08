package main

import (
	"context"
	"fmt"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	shareName    string
	shareDesc    string
	shareForce   bool
	sharePrivate bool
	unshareYes   bool
	authLogout   bool
	listPlain    bool
	getPrivate   bool
	unshPrivate  bool
	listPrivate  bool
)

func cloudCommand() *cli.Command {
	return &cli.Command{
		Name:        "cloud",
		Aliases:     []string{"cl"},
		Usage:       "Share/download files from SciMinds cloud storage",
		Description: "$ sci cloud share results.csv\n$ sci cloud list\n$ sci cloud get my-data",
		Category:    "Commands",
		Commands: []*cli.Command{
			cloudAuthCommand(),
			cloudShareCommand(),
			cloudGetCommand(),
			cloudUnshareCommand(),
			cloudListCommand(),
		},
	}
}

func cloudAuthCommand() *cli.Command {
	return &cli.Command{
		Name:        "auth",
		Usage:       "Authenticate with GitHub to access SciMinds cloud storage",
		Description: "$ sci cloud auth\n$ sci cloud auth --logout",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "logout", Usage: "clear saved credentials", Destination: &authLogout, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if authLogout {
				result, err := share.AuthLogout()
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			result, err := share.Auth()
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cloudShareCommand() *cli.Command {
	return &cli.Command{
		Name:        "share",
		Usage:       "Upload a file to cloud storage",
		Description: "$ sci cloud share results.csv\n$ sci cloud share results.csv --private\n$ sci cloud share results.csv --name my-results.csv --desc 'Experiment results' --force",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "share name (default: filename)", Destination: &shareName, Local: true},
			&cli.StringFlag{Name: "desc", Aliases: []string{"d"}, Usage: "optional description", Destination: &shareDesc, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing file without prompting", Destination: &shareForce, Local: true},
			&cli.BoolFlag{Name: "private", Aliases: []string{"p"}, Usage: "upload to private bucket", Destination: &sharePrivate, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("share requires a file path\n\n" +
					"  Upload a file:   sci cloud share results.csv\n" +
					"  List files:      sci cloud list\n\n" +
					"  Run 'sci cloud share --help' for more options")
			}
			filePath := cmd.Args().First()

			name := shareName
			desc := shareDesc
			force := shareForce

			// Interactive flow when --name is not provided.
			if name == "" {
				defaultName := share.DefaultShareName(filePath)
				if err := huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Share name").
						Description("Name used to get/unshare this file").
						Placeholder(defaultName).
						Value(&name),
					huh.NewInput().
						Title("Description").
						Description("Optional — shown in cloud list").
						Value(&desc),
				)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
					return err
				}
				if name == "" {
					name = defaultName
				}
			}

			// Check for existing file and prompt unless --force.
			if !force {
				exists, err := share.CheckExists(name, sharePrivate)
				if err != nil {
					return err
				}
				if exists {
					var overwrite bool
					if err := huh.NewForm(huh.NewGroup(
						huh.NewConfirm().
							Title(fmt.Sprintf("File %q already exists. Overwrite?", name)).
							Value(&overwrite),
					)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
						return err
					}
					if !overwrite {
						ui.Hint("cancelled")
						return nil
					}
					force = true
				}
			}

			result, err := share.Share(filePath, share.ShareOpts{Name: name, Description: desc, Force: force, Private: sharePrivate})
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cloudGetCommand() *cli.Command {
	return &cli.Command{
		Name:        "get",
		Usage:       "Download a shared file",
		Description: "$ sci cloud get experiment-results.csv\n$ sci cloud get experiment-results.csv --private",
		ArgsUsage:   "<name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "private", Aliases: []string{"p"}, Usage: "download from private bucket", Destination: &getPrivate, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("get requires a file name\n\n" +
					"  Download a file:   sci cloud get experiment-results.csv\n" +
					"  List files:        sci cloud list\n\n" +
					"  Run 'sci cloud get --help' for more options")
			}
			result, err := share.Get(cmd.Args().First(), getPrivate)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cloudUnshareCommand() *cli.Command {
	return &cli.Command{
		Name:        "unshare",
		Usage:       "Remove a shared file",
		Description: "$ sci cloud unshare results.csv\n$ sci cloud unshare results.csv --private",
		ArgsUsage:   "<name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &unshareYes, Local: true},
			&cli.BoolFlag{Name: "private", Aliases: []string{"p"}, Usage: "remove from private bucket", Destination: &unshPrivate, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly 1 argument, got %d", cmd.Args().Len())
			}
			name := cmd.Args().First()
			if done, err := cmdutil.ConfirmOrSkip(unshareYes, fmt.Sprintf("Remove shared file %q?", name)); done || err != nil {
				return err
			}
			result, err := share.Unshare(name, unshPrivate)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cloudListCommand() *cli.Command {
	return &cli.Command{
		Name:        "list",
		Aliases:     []string{"ls"},
		Usage:       "List your shared files",
		Description: "$ sci cloud list\n$ sci cloud list --private\n$ sci cloud list --plain",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "plain", Usage: "plain table output instead of interactive TUI", Destination: &listPlain, Local: true},
			&cli.BoolFlag{Name: "private", Aliases: []string{"p"}, Usage: "list files in private bucket", Destination: &listPrivate, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, c, err := cloud.SetupBucket(listPrivate)
			if err != nil {
				return err
			}

			plain := cmdutil.IsJSON(cmd) || listPlain
			result, err := share.SharedWithOpts(c, plain)
			if err != nil {
				return err
			}

			if plain {
				cmdutil.Output(cmd, result)
				return nil
			}
			return share.RunCloudListTUI(result.Datasets, c)
		},
	}
}
