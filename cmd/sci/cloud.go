package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

var (
	putName     string
	putDesc     string
	putForce    bool
	removeYes   bool
	setupLogout bool
	listPlain   bool
)

func cloudCommand() *cli.Command {
	return &cli.Command{
		Name:        "cloud",
		Aliases:     []string{"cl"},
		Usage:       "Share/download files from SciMinds cloud storage",
		Description: "$ sci cloud put results.csv\n$ sci cloud list\n$ sci cloud get my-data",
		Category:    "Commands",
		Before: func(_ context.Context, _ *cli.Command) (context.Context, error) {
			if !netutil.Online() {
				return nil, fmt.Errorf("no internet connection — sci cloud requires network access")
			}
			return nil, nil
		},
		Commands: []*cli.Command{
			cloudSetupCommand(),
			cloudPutCommand(),
			cloudGetCommand(),
			cloudRemoveCommand(),
			cloudListCommand(),
		},
	}
}

func cloudSetupCommand() *cli.Command {
	return &cli.Command{
		Name:        "setup",
		Usage:       "Authenticate with GitHub to access SciMinds cloud storage",
		Description: "$ sci cloud setup\n$ sci cloud setup --logout",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "logout", Usage: "clear saved credentials", Destination: &setupLogout, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if setupLogout {
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

func cloudPutCommand() *cli.Command {
	return &cli.Command{
		Name:        "put",
		Usage:       "Upload a file to cloud storage",
		Description: "$ sci cloud put results.csv\n$ sci cloud put results.csv --name my-results.csv --desc 'Experiment results' --force",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "upload name (default: filename)", Destination: &putName, Local: true},
			&cli.StringFlag{Name: "desc", Aliases: []string{"d"}, Usage: "optional description", Destination: &putDesc, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing file without prompting", Destination: &putForce, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("put requires a file path\n\n" +
					"  Upload a file:   sci cloud put results.csv\n" +
					"  List files:      sci cloud list\n\n" +
					"  Run 'sci cloud put --help' for more options")
			}
			filePath := cmd.Args().First()

			// Warn about public access.
			fmt.Fprintf(os.Stderr, "\n  %s This creates a public URL for file access. Do not share sensitive or personally identifying information.\n", uikit.SymWarn)
			fmt.Fprintf(os.Stderr, "    Use %s for private lab storage.\n\n", uikit.TUI.TextBlue().Render("sci lab put"))

			name := putName
			desc := putDesc
			force := putForce

			// In JSON mode, require --name (no interactive form).
			if cmdutil.IsJSON(cmd) {
				if name == "" {
					return fmt.Errorf("--name is required in --json mode")
				}
				force = true // auto-confirm overwrite in non-interactive mode
			}

			// Interactive flow when --name is not provided.
			if name == "" {
				defaultName := share.DefaultShareName(filePath)
				if err := uikit.RunForm(huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Upload name").
						Description("Name used to get/remove this file").
						Placeholder(defaultName).
						Value(&name),
					huh.NewInput().
						Title("Description").
						Description("Optional — shown in cloud list").
						Value(&desc),
				))); err != nil {
					return err
				}
				if name == "" {
					name = defaultName
				}
			}

			// Check for existing file and prompt unless --force.
			if !force {
				exists, err := share.CheckExists(name)
				if err != nil {
					return err
				}
				if exists {
					if err := cmdutil.Confirm(fmt.Sprintf("File %q already exists. Overwrite?", name)); err != nil {
						if errors.Is(err, cmdutil.ErrCancelled) {
							uikit.Hint("cancelled")
							return nil
						}
						return err
					}
					force = true
				}
			}

			result, err := share.Share(filePath, share.ShareOpts{Name: name, Description: desc, Force: force})
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
		Description: "$ sci cloud get experiment-results.csv",
		ArgsUsage:   "<name>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("get requires a file name\n\n" +
					"  Download a file:   sci cloud get experiment-results.csv\n" +
					"  List files:        sci cloud list\n\n" +
					"  Run 'sci cloud get --help' for more options")
			}
			result, err := share.Get(cmd.Args().First())
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cloudRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:        "remove",
		Aliases:     []string{"rm"},
		Usage:       "Remove a shared file",
		Description: "$ sci cloud remove results.csv",
		ArgsUsage:   "<name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation prompt", Destination: &removeYes, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly 1 argument, got %d", cmd.Args().Len())
			}
			name := cmd.Args().First()
			if done, err := cmdutil.ConfirmOrSkip(removeYes, fmt.Sprintf("Remove shared file %q?", name)); done || err != nil {
				return err
			}
			result, err := share.Unshare(name)
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
		Usage:       "List all shared files",
		Description: "$ sci cloud list\n$ sci cloud list --plain",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "plain", Usage: "plain table output instead of interactive TUI", Destination: &listPlain, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, c, err := cloud.Setup()
			if err != nil {
				return err
			}

			plain := cmdutil.IsJSON(cmd) || listPlain
			result, err := share.SharedAll(c, plain)
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
