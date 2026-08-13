package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/netutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// setup command flag destinations (package-scoped like other sci commands).
var (
	setupAPIKey          string
	setupUserID          string
	setupSharedGroupID   string
	setupSharedGroupName string
	setupDataDir         string
	setupLogout          bool
	setupForce           bool
)

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Point sci at your Zotero library",
		Description: "$ sci zot setup\n" +
			"$ sci zot setup --api <key> --user-id <id>\n" +
			"$ sci zot setup --api <key> --user-id <id> --data-dir ~/Zotero\n" +
			"$ sci zot setup --logout\n\n" +
			"--data-dir is what the local reads need: search, bib, export and\n" +
			"the doctor reports all open the zotero.sqlite it names. The API key\n" +
			"is only for the `--remote` live reads and for naming your shared\n" +
			"group; sci never writes to a library with it.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "api", Usage: "Zotero Web API key (required in --json mode)", Destination: &setupAPIKey, Local: true},
			&cli.StringFlag{Name: "user-id", Usage: "Zotero numeric user ID (required in --json mode)", Destination: &setupUserID, Local: true},
			&cli.StringFlag{Name: "shared-group-id", Usage: "numeric group ID to use as the shared library (only needed when the account belongs to >1 group)", Destination: &setupSharedGroupID, Local: true},
			&cli.StringFlag{Name: "shared-group-name", Usage: "display name for the shared group (optional; auto-detected when --shared-group-id is set)", Destination: &setupSharedGroupName, Local: true},
			&cli.StringFlag{Name: "data-dir", Usage: "path to directory containing zotero.sqlite (auto-detected if omitted)", Destination: &setupDataDir, Local: true},
			&cli.BoolFlag{Name: "logout", Usage: "clear saved credentials", Destination: &setupLogout, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing config without prompting", Destination: &setupForce, Local: true},
		},
		Action: runSetup,
	}
}

// RunSetup runs the interactive Zotero setup flow — the exact flow behind
// `sci zot setup`. Exported so the top-level `sci setup` menu can delegate to
// it: one implementation, two entry points.
func RunSetup(ctx context.Context, cmd *cli.Command) error {
	return runSetup(ctx, cmd)
}

func runSetup(ctx context.Context, cmd *cli.Command) error {
	if setupLogout {
		result, err := zot.Logout()
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	apiKey := setupAPIKey
	userID := setupUserID
	sharedGroupID := setupSharedGroupID
	sharedGroupName := setupSharedGroupName
	dataDir := setupDataDir

	jsonMode := cmdutil.IsJSON(cmd)
	// `setup --json` with no creds → print the saved config and exit.
	if jsonMode && apiKey == "" && userID == "" {
		cfg, err := zot.LoadConfig()
		if err != nil {
			return err
		}
		if cfg == nil {
			return cmdutil.Coded(cmdutil.CodeNotConfigured, "zot not configured").
				WithTry("run 'sci zot setup' interactively, or pass --api and --user-id in --json mode")
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
		if apiKey == "" || userID == "" {
			return fmt.Errorf("--api and --user-id are required in --json mode")
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
		needForm := apiKey == "" || userID == "" || dataDir == ""
		if needForm {
			if err := uikit.NewForm(uikit.FormGroup(
				uikit.FormInput(&apiKey, "Zotero API key",
					uikit.WithDescription("From https://www.zotero.org/settings/keys"),
					uikit.WithValidation(func(s string) error { return zot.ValidateAPIKey(s) })),
				uikit.FormInput(&userID, "User ID",
					uikit.WithDescription("Numeric user ID (https://www.zotero.org/settings/keys — \"Your userID for use in API calls\")"),
					uikit.WithValidation(func(s string) error { return zot.ValidateUserID(s) })),
				uikit.FormInput(&dataDir, "Data directory",
					uikit.WithDescription("Zotero's data dir (contains zotero.sqlite)"),
					uikit.WithValidation(func(s string) error { return zot.ValidateDataDir(s) })),
			)).Run(); err != nil {
				return err
			}
		}
	}

	// Auto-detect the shared group when the account has network access and
	// the user didn't pre-specify one. Non-fatal on failure (offline, API
	// hiccup) — setup still succeeds with personal-only config.
	probe := setupGroupProbe(ctx, apiKey, userID)

	result, err := zot.Setup(zot.SetupInput{
		APIKey:          apiKey,
		UserID:          userID,
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

// setupGroupProbe returns a GroupProbeFunc that calls the Zotero Web API to
// enumerate the groups an account belongs to. Returns nil (no-op probe) when
// offline or when the API key is missing — Setup treats a nil probe as
// "shared auto-detect skipped" rather than an error.
func setupGroupProbe(ctx context.Context, apiKey, userID string) zot.GroupProbeFunc {
	if apiKey == "" || !netutil.Online() {
		return nil
	}
	return func() ([]zot.GroupRef, error) {
		cfg := &zot.Config{APIKey: apiKey, UserID: userID}
		c, err := api.New(cfg, api.WithLibrary(zot.LibraryRef{
			Scope:   zot.LibPersonal,
			APIPath: "users/" + userID,
		}))
		if err != nil {
			return nil, err
		}
		return c.ListGroups(ctx)
	}
}
