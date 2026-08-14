package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Point sci at your Zotero data directory",
		Description: "$ sci zot setup\n" +
			"$ sci zot setup --data-dir ~/Zotero\n" +
			"$ sci zot setup --logout\n\n" +
			"Setup records one thing: where zotero.sqlite lives. Every zot\n" +
			"command opens it read-only and answers from it — there is no\n" +
			"account to log into and no API key to paste. The shared group\n" +
			"backing `--library shared` is read out of that same database.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "shared-group-id", Usage: "numeric group ID to use as the shared library (only needed when the library holds >1 group)", Local: true},
			&cli.StringFlag{Name: "shared-group-name", Usage: "display name for the shared group (optional; auto-detected when --shared-group-id is set)", Local: true},
			&cli.StringFlag{Name: "data-dir", Usage: "path to directory containing zotero.sqlite (auto-detected if omitted)", Local: true},
			&cli.BoolFlag{Name: "logout", Usage: "clear the saved configuration", Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing config without prompting", Local: true},
		},
		Action: runSetup,
	}
}

// RunSetup runs the interactive Zotero setup flow — the exact flow behind
// `sci zot setup`. Exported so the top-level `sci setup` menu can delegate to
// it: one implementation, two entry points.
//
// Every pre-fill (--data-dir, --shared-group-id, …) is read off cmd, so the
// menu's cmd — which declares none of those flags — lands on the fully
// interactive path, which is what a menu pick means.
func RunSetup(ctx context.Context, cmd *cli.Command) error {
	return runSetup(ctx, cmd)
}

func runSetup(_ context.Context, cmd *cli.Command) error {
	if cmd.Bool("logout") {
		result, err := zot.Logout()
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	sharedGroupID := cmd.String("shared-group-id")
	sharedGroupName := cmd.String("shared-group-name")
	dataDir := cmd.String("data-dir")

	jsonMode := cmdutil.IsJSON(cmd)
	// `setup --json` with nothing to set → print the saved config and exit.
	// Nothing here is secret any more, so the whole stored shape is emitted.
	if jsonMode && dataDir == "" && sharedGroupID == "" {
		cfg, err := zot.LoadConfig()
		if err != nil {
			return err
		}
		if cfg == nil {
			return cmdutil.Coded(cmdutil.CodeNotConfigured, "zot not configured").
				WithTry("run 'sci zot setup' interactively, or pass --data-dir in --json mode")
		}
		cmdutil.Output(cmd, cfg)
		return nil
	}

	// Interactive overwrite guard. In --json (non-interactive) mode the caller
	// is expected to know what they're doing; --force bypasses the prompt.
	if !jsonMode && !cmd.Bool("force") && zot.ConfigExists() {
		if err := cmdutil.ConfirmYes("zot is already configured. Overwrite?"); err != nil {
			if errors.Is(err, cmdutil.ErrCancelled) {
				fmt.Fprintln(os.Stderr, "cancelled")
				return nil
			}
			return err
		}
	}

	// Probing the platform candidates first is what makes setup a no-argument
	// command on a standard install: DefaultDataDir finds ~/Zotero (and the
	// Linux native / snap / Flatpak paths) without asking.
	if dataDir == "" {
		dataDir = zot.DefaultDataDir()
	}
	switch {
	case jsonMode && dataDir == "":
		return fmt.Errorf("--data-dir is required when zotero.sqlite is not in a default location")
	case !jsonMode && zot.ValidateDataDir(dataDir) != nil:
		// Prompt only when the probe came back empty or wrong — a detected
		// directory is the answer, not a question.
		if err := uikit.NewForm(uikit.FormGroup(
			uikit.FormInput(&dataDir, "Data directory",
				uikit.WithDescription("Zotero's data dir (contains zotero.sqlite)"),
				uikit.WithValidation(func(s string) error { return zot.ValidateDataDir(s) })),
		)).Run(); err != nil {
			return err
		}
	}

	// Auto-detect the shared group from the local database when the user
	// didn't pre-specify one.
	probe := zot.LocalGroupProbe(dataDir)

	result, err := zot.Setup(zot.SetupInput{
		SharedGroupID:   sharedGroupID,
		SharedGroupName: sharedGroupName,
		DataDir:         dataDir,
		GroupProbe:      probe,
	})
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, result)
	return nil
}
