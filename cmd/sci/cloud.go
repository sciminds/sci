package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/tui/cloudbrowse"
	"github.com/sciminds/cli/internal/tui/fspicker"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

var (
	putName     string
	putPublic   bool
	putForce    bool
	getPublic   bool
	removeYes   bool
	removePub   bool
	lsPublic    bool
	setupLogout bool
)

func cloudCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "cloud",
		Aliases: []string{"cl"},
		Usage:   "Upload/download files to the SciMinds Hugging Face buckets",
		Description: "Default bucket is private (sciminds/private). Pass --public to use\n" +
			"the world-readable bucket (sciminds/public).\n\n" +
			"  $ sci cloud put results.csv               # private (default)\n" +
			"  $ sci cloud put results.csv --public      # public, prints URL\n" +
			"  $ sci cloud ls                            # your private files\n" +
			"  $ sci cloud ls --public                   # everyone's public files\n" +
			"  $ sci cloud get                           # interactive TUI\n" +
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
			cloudRemoveCommand(),
		},
	}
	cmdutil.MarkDeprecatedChildren(cmd, map[string]string{"browse": "get"})
	return cmd
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
		Name:    "ls",
		Aliases: []string{"list"},
		Usage:   "List shared files at a path (private bucket by default; --public to list public)",
		Description: "$ sci cloud ls                       # bucket root — one folder per user\n" +
			"$ sci cloud ls ejolly                 # one user's files/folders\n" +
			"$ sci cloud ls ejolly/python-tutorials\n" +
			"$ sci cloud ls --public",
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "list the public bucket", Destination: &lsPublic, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, c, err := cloud.Setup(share.BucketFor(lsPublic))
			if err != nil {
				return err
			}
			path := cmd.Args().First() // "" => root
			result, err := share.ListAt(c, path, true)
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
		Usage: "Download a shared file, or browse interactively with no arg (--public for public bucket)",
		Description: "$ sci cloud get                              # interactive TUI (browse + download)\n" +
			"$ sci cloud get --public                     # browse the public bucket\n" +
			"$ sci cloud get results.csv                  # your private file\n" +
			"$ sci cloud get results.csv ./local/         # download into ./local/\n" +
			"$ sci cloud get alice/data.csv --public      # someone else's public file",
		ArgsUsage: "[name [local]]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "fetch from / browse the public bucket", Destination: &getPublic, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				if cmdutil.IsJSON(cmd) {
					return fmt.Errorf("get requires a file name in JSON mode")
				}
				_, c, err := cloud.Setup(share.BucketFor(getPublic))
				if err != nil {
					return err
				}
				objects, err := share.FetchObjects(c, false)
				if err != nil {
					return err
				}
				if err := cloudbrowse.Run(objects, c); err != nil {
					if errors.Is(err, cloudbrowse.ErrInterrupted) {
						return cli.Exit("", 130)
					}
					return err
				}
				return nil
			}
			name := cmd.Args().Get(0)
			local := ""
			if cmd.Args().Len() >= 2 {
				local = cmd.Args().Get(1)
			}

			result, err := share.Get(name, local, getPublic)
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
		Description: "$ sci cloud put results.csv                   # upload a single file\n" +
			"$ sci cloud put ./myrepo                     # sync a directory as a tree\n" +
			"$ sci cloud put results.csv --public\n" +
			"$ sci cloud put results.csv --name my-results.csv --force\n" +
			"$ sci cloud put                              # interactive picker (u=upload, U=force)",
		ArgsUsage: "[file-or-dir]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: "upload name (default: filename)", Destination: &putName, Local: true},
			&cli.BoolFlag{Name: "public", Aliases: []string{"p"}, Usage: "upload to the public bucket (world-readable URL)", Destination: &putPublic, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing file without prompting", Destination: &putForce, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			var filePath string
			switch cmd.Args().Len() {
			case 1:
				filePath = cmd.Args().First()
			case 0:
				if cmdutil.IsJSON(cmd) {
					return fmt.Errorf("put requires a file path in JSON mode")
				}
				res, err := fspicker.Pick(fspicker.Opts{})
				if err != nil {
					if errors.Is(err, fspicker.ErrCancelled) {
						uikit.Hint("cancelled")
						return nil
					}
					return err
				}
				filePath = res.Path
				if res.Force {
					putForce = true
				}
			default:
				return fmt.Errorf("put takes at most one file path\n\n" +
					"  Upload privately:  sci cloud put results.csv\n" +
					"  Share publicly:    sci cloud put results.csv --public\n" +
					"  Pick interactively: sci cloud put\n\n" +
					"  Run 'sci cloud put --help' for more options")
			}

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
				info, statErr := os.Stat(filePath)
				if statErr != nil {
					return statErr
				}
				isDir := info.IsDir()
				exists, err := share.CheckExists(name, putPublic, isDir)
				if err != nil {
					return err
				}
				if exists {
					prompt := fmt.Sprintf("File %q already exists. Overwrite?", name)
					if isDir {
						prompt = fmt.Sprintf("Remote prefix %q is not empty. Overwrite differing files? (Non-conflicting remote files stay.)", name+"/")
					}
					if err := cmdutil.Confirm(prompt); err != nil {
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
