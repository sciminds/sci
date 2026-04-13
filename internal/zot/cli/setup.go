package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cliui"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// setup command flag destinations (package-scoped like other sci commands).
var (
	setupAPIKey    string
	setupLibraryID string
	setupDataDir   string
	setupLogout    bool
	setupForce     bool
)

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Configure Zotero API credentials and data directory",
		Description: "$ zot setup\n" +
			"$ zot setup --api <key> --library <id>\n" +
			"$ zot setup --api <key> --library <id> --data-dir ~/Zotero\n" +
			"$ zot setup --logout",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "api", Usage: "Zotero Web API key (required in --json mode)", Destination: &setupAPIKey, Local: true},
			&cli.StringFlag{Name: "library", Usage: "Zotero numeric user ID (required in --json mode)", Destination: &setupLibraryID, Local: true},
			&cli.StringFlag{Name: "data-dir", Usage: "path to directory containing zotero.sqlite (auto-detected if omitted)", Destination: &setupDataDir, Local: true},
			&cli.BoolFlag{Name: "logout", Usage: "clear saved credentials", Destination: &setupLogout, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing config without prompting", Destination: &setupForce, Local: true},
		},
		Action: runSetup,
	}
}

func runSetup(_ context.Context, cmd *cli.Command) error {
	if setupLogout {
		result, err := zot.Logout()
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	apiKey := setupAPIKey
	libraryID := setupLibraryID
	dataDir := setupDataDir

	jsonMode := cmdutil.IsJSON(cmd)
	// `setup --json` with no creds → print the saved config and exit.
	if jsonMode && apiKey == "" && libraryID == "" {
		cfg, err := zot.LoadConfig()
		if err != nil {
			return err
		}
		if cfg == nil {
			return fmt.Errorf("zot not configured — run 'zot setup' first")
		}
		cmdutil.Output(cmd, cfg)
		return nil
	}

	// Interactive overwrite guard. In --json (non-interactive) mode the caller
	// is expected to know what they're doing; --force bypasses the prompt.
	if !jsonMode && !setupForce && zot.ConfigExists() {
		if err := cmdutil.ConfirmYes("zot is already configured. Overwrite?"); err != nil {
			if errors.Is(err, cmdutil.ErrCancelled) {
				fmt.Fprintln(os.Stderr, "cancelled")
				return nil
			}
			return err
		}
	}

	if jsonMode {
		if apiKey == "" || libraryID == "" {
			return fmt.Errorf("--api and --library are required in --json mode")
		}
		if dataDir == "" {
			dataDir = zot.DefaultDataDir()
			if dataDir == "" {
				return fmt.Errorf("--data-dir is required when zotero.sqlite is not in a default location")
			}
		}
	} else {
		// Interactive: prompt for anything missing, prefilling detected defaults.
		if dataDir == "" {
			dataDir = zot.DefaultDataDir()
		}
		needForm := apiKey == "" || libraryID == "" || dataDir == ""
		if needForm {
			form := huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Zotero API key").
					Description("From https://www.zotero.org/settings/keys").
					Value(&apiKey).
					Validate(func(s string) error { return zot.ValidateAPIKey(s) }),
				huh.NewInput().
					Title("Library ID").
					Description("Numeric user ID (https://www.zotero.org/settings/keys — \"Your userID for use in API calls\")").
					Value(&libraryID).
					Validate(func(s string) error { return zot.ValidateLibraryID(s) }),
				huh.NewInput().
					Title("Data directory").
					Description("Zotero's data dir (contains zotero.sqlite)").
					Value(&dataDir).
					Validate(func(s string) error { return zot.ValidateDataDir(s) }),
			)).WithTheme(cliui.HuhTheme()).WithKeyMap(cliui.HuhKeyMap())
			if err := form.Run(); err != nil {
				return err
			}
		}
	}

	result, err := zot.Setup(apiKey, libraryID, dataDir)
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, result)
	return nil
}
