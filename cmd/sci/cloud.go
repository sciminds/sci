package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

var (
	putName      string
	putPublic    bool
	putForce     bool
	getPublic    bool
	removeYes    bool
	removePub    bool
	lsPublic     bool
	browsePublic bool
	setupLogout  bool
)

func cloudCommand() *cli.Command {
	return &cli.Command{
		Name:    "cloud",
		Aliases: []string{"cl"},
		Usage:   "Upload/download files to the SciMinds Hugging Face buckets",
		Description: "Default bucket is private (sciminds/private). Pass --public to use\n" +
			"the world-readable bucket (sciminds/public).\n\n" +
			"  $ sci cloud put results.csv               # private (default)\n" +
			"  $ sci cloud put results.csv --public      # public, prints URL\n" +
			"  $ sci cloud ls                            # your private files\n" +
			"  $ sci cloud ls --public                   # everyone's public files\n" +
			"  $ sci cloud browse                        # interactive TUI\n" +
			"  $ sci cloud get someone/data.csv --public",
		Category: "Commands",
		Before: func(_ context.Context, _ *cli.Command) (context.Context, error) {
			if !netutil.Online() {
				return nil, fmt.Errorf("no internet connection — sci cloud requires network access")
			}
			return nil, nil
		},
		Commands: []*cli.Command{
			cloudSetupCommand(),
			cloudLsCommand(),
			cloudGetCommand(),
			cloudPutCommand(),
			cloudBrowseCommand(),
			cloudRemoveCommand(),
		},
	}
}

func cloudSetupCommand() *cli.Command {
	return &cli.Command{
		Name:        "setup",
		Usage:       "Authenticate with Hugging Face (sciminds org)",
		Description: "$ sci cloud setup\n$ sci cloud setup --logout",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "logout", Usage: "log out of Hugging Face", Destination: &setupLogout, Local: true},
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

func cloudLsCommand() *cli.Command {
	return &cli.Command{
		Name:        "ls",
		Aliases:     []string{"list"},
		Usage:       "List shared files (private bucket by default; --public to list public)",
		Description: "$ sci cloud ls\n$ sci cloud ls --public",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "list the public bucket", Destination: &lsPublic, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, c, err := cloud.Setup(share.BucketFor(lsPublic))
			if err != nil {
				return err
			}
			result, err := share.SharedAll(c, true)
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
		Name:  "get",
		Usage: "Download a shared file (private bucket by default; --public for public bucket)",
		Description: "$ sci cloud get results.csv                  # your private file\n" +
			"$ sci cloud get results.csv ./local/         # download into ./local/\n" +
			"$ sci cloud get alice/data.csv --public      # someone else's public file",
		ArgsUsage: "<name> [local]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "fetch from the public bucket", Destination: &getPublic, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() < 1 {
				return fmt.Errorf("get requires a file name\n\n" +
					"  Download own private:    sci cloud get experiment.csv\n" +
					"  Download someone else's: sci cloud get alice/data.csv --public\n\n" +
					"  Run 'sci cloud get --help' for more options")
			}
			name := cmd.Args().Get(0)
			local := ""
			if cmd.Args().Len() >= 2 {
				local = cmd.Args().Get(1)
			}

			// Cross-user "<owner>/<file>" only makes sense for the public bucket.
			public := getPublic
			if strings.Contains(name, "/") {
				if !public {
					fmt.Fprintf(os.Stderr, "  %s cross-user downloads require --public; assuming public bucket.\n", uikit.SymWarn)
				}
				public = true
			}

			result, err := share.Get(name, local, public)
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
		Name:  "put",
		Usage: "Upload a file (private by default; --public to share)",
		Description: "$ sci cloud put results.csv\n" +
			"$ sci cloud put results.csv --public\n" +
			"$ sci cloud put results.csv --name my-results.csv --force",
		ArgsUsage: "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "upload name (default: filename)", Destination: &putName, Local: true},
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "upload to the public bucket (world-readable URL)", Destination: &putPublic, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing file without prompting", Destination: &putForce, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("put requires a file path\n\n" +
					"  Upload privately:  sci cloud put results.csv\n" +
					"  Share publicly:    sci cloud put results.csv --public\n\n" +
					"  Run 'sci cloud put --help' for more options")
			}
			filePath := cmd.Args().First()

			if putPublic {
				fmt.Fprintf(os.Stderr, "\n  %s --public creates a world-readable URL. Do not share sensitive or personally identifying information.\n", uikit.SymWarn)
				fmt.Fprintf(os.Stderr, "    Omit --public to keep this file in the private sciminds bucket.\n\n")
			}

			name := putName
			if name == "" {
				name = share.DefaultShareName(filePath)
			}

			force := putForce
			if cmdutil.IsJSON(cmd) {
				force = true
			}

			if !force {
				exists, err := share.CheckExists(name, putPublic)
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

			result, err := share.Share(filePath, share.ShareOpts{Name: name, Public: putPublic, Force: force})
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cloudBrowseCommand() *cli.Command {
	return &cli.Command{
		Name:        "browse",
		Usage:       "Interactively browse shared files (delete, copy URL, download)",
		Description: "$ sci cloud browse\n$ sci cloud browse --public",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "browse the public bucket", Destination: &browsePublic, Local: true},
		},
		Action: func(_ context.Context, _ *cli.Command) error {
			_, c, err := cloud.Setup(share.BucketFor(browsePublic))
			if err != nil {
				return err
			}
			result, err := share.SharedAll(c, false)
			if err != nil {
				return err
			}
			if err := share.RunCloudListTUI(result.Datasets, c); err != nil {
				if errors.Is(err, share.ErrInterrupted) {
					return cli.Exit("", 130)
				}
				return err
			}
			return nil
		},
	}
}

func cloudRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:        "remove",
		Aliases:     []string{"rm"},
		Usage:       "Remove a shared file (private by default; --public to remove from public bucket)",
		Description: "$ sci cloud remove results.csv\n$ sci cloud remove results.csv --public",
		ArgsUsage:   "<name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "remove from the public bucket", Destination: &removePub, Local: true},
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
			result, err := share.Unshare(name, removePub)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}
