package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// setup command flag destinations (package-scoped like other sci commands).
var (
	setupAPIKey          string
	setupUserID          string
	setupSharedGroupID   string
	setupSharedGroupName string
	setupDataDir         string
	setupOpenAlexEmail   string
	setupOpenAlexAPIKey  string
	setupLogout          bool
	setupForce           bool
)

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Configure Zotero API credentials and data directory",
		Description: "$ zot setup\n" +
			"$ zot setup --api <key> --user-id <id>\n" +
			"$ zot setup --api <key> --user-id <id> --data-dir ~/Zotero\n" +
			"$ zot setup --openalex-email you@example.com --openalex-api <key>\n" +
			"$ zot setup --logout",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "api", Usage: "Zotero Web API key (required in --json mode)", Destination: &setupAPIKey, Local: true},
			&cli.StringFlag{Name: "user-id", Usage: "Zotero numeric user ID (required in --json mode)", Destination: &setupUserID, Local: true},
			&cli.StringFlag{Name: "shared-group-id", Usage: "numeric group ID to use as the shared library (only needed when the account belongs to >1 group)", Destination: &setupSharedGroupID, Local: true},
			&cli.StringFlag{Name: "shared-group-name", Usage: "display name for the shared group (optional; auto-detected when --shared-group-id is set)", Destination: &setupSharedGroupName, Local: true},
			&cli.StringFlag{Name: "data-dir", Usage: "path to directory containing zotero.sqlite (auto-detected if omitted)", Destination: &setupDataDir, Local: true},
			&cli.StringFlag{Name: "openalex-email", Usage: "email for the OpenAlex polite pool (optional, ~10 req/s)", Destination: &setupOpenAlexEmail, Local: true},
			&cli.StringFlag{Name: "openalex-api", Usage: "OpenAlex premium API key (optional, ~100 req/s)", Destination: &setupOpenAlexAPIKey, Local: true},
			&cli.BoolFlag{Name: "logout", Usage: "clear saved credentials", Destination: &setupLogout, Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing config without prompting", Destination: &setupForce, Local: true},
		},
		Action: runSetup,
	}
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
	openAlexEmail := setupOpenAlexEmail
	openAlexAPIKey := setupOpenAlexAPIKey

	// Prefill OpenAlex fields from any existing config so --openalex-* flags
	// behave as partial updates rather than wiping the other slot.
	if existing, _ := zot.LoadConfig(); existing != nil {
		if openAlexEmail == "" {
			openAlexEmail = existing.OpenAlexEmail
		}
		if openAlexAPIKey == "" {
			openAlexAPIKey = existing.OpenAlexAPIKey
		}
	}

	jsonMode := cmdutil.IsJSON(cmd)
	// `setup --json` with no creds → print the saved config and exit.
	if jsonMode && apiKey == "" && userID == "" {
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
			if err := uikit.RunForm(huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Zotero API key").
					Description("From https://www.zotero.org/settings/keys").
					Value(&apiKey).
					Validate(func(s string) error { return zot.ValidateAPIKey(s) }),
				huh.NewInput().
					Title("User ID").
					Description("Numeric user ID (https://www.zotero.org/settings/keys — \"Your userID for use in API calls\")").
					Value(&userID).
					Validate(func(s string) error { return zot.ValidateUserID(s) }),
				huh.NewInput().
					Title("Data directory").
					Description("Zotero's data dir (contains zotero.sqlite)").
					Value(&dataDir).
					Validate(func(s string) error { return zot.ValidateDataDir(s) }),
				huh.NewInput().
					Title("OpenAlex email (optional)").
					Description("Unlocks the polite pool (~10 req/s). Leave blank to skip.").
					Value(&openAlexEmail),
				huh.NewInput().
					Title("OpenAlex API key (optional)").
					Description("Premium tier (~100 req/s). Leave blank to skip.").
					Value(&openAlexAPIKey),
			))); err != nil {
				return err
			}
		}
	}

	// Auto-detect the shared group when the account has network access and
	// the user didn't pre-specify one. Non-fatal on failure (offline, API
	// hiccup) — setup still succeeds with personal-only config.
	probe := setupGroupProbe(ctx, apiKey)

	result, err := zot.Setup(zot.SetupInput{
		APIKey:          apiKey,
		UserID:          userID,
		SharedGroupID:   sharedGroupID,
		SharedGroupName: sharedGroupName,
		DataDir:         dataDir,
		OpenAlexEmail:   openAlexEmail,
		OpenAlexAPIKey:  openAlexAPIKey,
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
func setupGroupProbe(ctx context.Context, apiKey string) zot.GroupProbeFunc {
	if apiKey == "" || !netutil.Online() {
		return nil
	}
	return func(key, userID string) ([]zot.GroupRef, error) {
		cfg := &zot.Config{APIKey: key, UserID: userID}
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
