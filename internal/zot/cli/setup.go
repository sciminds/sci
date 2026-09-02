package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/netutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Point sci at your Zotero data directory, and optionally store an API key for writes",
		Description: "$ sci zot setup\n" +
			"$ sci zot setup --data-dir ~/Zotero\n" +
			"$ sci zot setup --api-key zXXXXXXXXXXXXXXXXXXXXXXXX\n" +
			"$ sci zot setup --clear\n\n" +
			"Setup records where zotero.sqlite lives — every read verb opens it\n" +
			"and answers from it, no account needed — and, optionally, a Zotero\n" +
			"Web API key, which is what the write verbs (collection create, item\n" +
			"add, …) use. Create one with library write access at\n" +
			"https://www.zotero.org/settings/keys/new; setup verifies it with\n" +
			"Zotero and records the user ID it belongs to. The shared group\n" +
			"backing `--library shared` is read out of the local database.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "api-key", Usage: "Zotero Web API key (enables writes; verified before it is stored)", Local: true},
			&cli.StringFlag{Name: "shared-group-id", Usage: "numeric group ID to use as the shared library (only needed when the library holds >1 group)", Local: true},
			&cli.StringFlag{Name: "shared-group-name", Usage: "display name for the shared group (optional; auto-detected when --shared-group-id is set)", Local: true},
			&cli.StringFlag{Name: "data-dir", Usage: "path to directory containing zotero.sqlite (auto-detected if omitted)", Local: true},
			// --clear, not --logout: it forgets the data directory and the
			// API key sci stored, and leaves every key sci does not own in
			// the file untouched.
			&cli.BoolFlag{Name: "clear", Usage: "forget the saved configuration (data directory and API key)", Local: true},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing config without prompting", Local: true},
		},
		Action: runSetup,
	}
}

// verifyAPIKey asks Zotero who a key belongs to and what it may do. It is
// the one network call setup makes, and it runs before anything is written
// so a mistyped key is refused rather than stored.
//
// A package var so tests substitute a fake; production never reassigns it.
var verifyAPIKey = func(ctx context.Context, key string) (*api.KeyInfo, error) {
	// /keys/current carries no library prefix, so the placeholder user ID
	// never reaches a URL — it only satisfies the client's constructor.
	c, err := api.New(api.Credentials{APIKey: key, UserID: "0"}, zot.LibraryRef{Scope: zot.LibPersonal})
	if err != nil {
		return nil, err
	}
	return c.CurrentKey(ctx)
}

// keyRejected tells a key Zotero refused (403) or does not know (404) from
// a request that never got an answer — an outage must not read as a typo.
func keyRejected(err error) bool {
	httpErr, ok := errors.AsType[*api.HTTPError](err)
	return ok && (httpErr.Status == http.StatusForbidden || httpErr.Status == http.StatusNotFound)
}

// resolveAPIKey turns the --api-key flag (or the optional interactive
// prompt) into a verified credential, or returns blanks when the user is
// setting up reads only. A stored key is never re-prompted for: re-running
// setup keeps it (zot.Setup carries it forward), and --clear forgets it.
func resolveAPIKey(ctx context.Context, cmd *cli.Command, existing *zot.Config, jsonMode bool) (apiKey, userID string, err error) {
	apiKey = cmd.String("api-key")
	if apiKey == "" && !jsonMode && (existing == nil || existing.APIKey == "") {
		if err := uikit.NewForm(uikit.FormGroup(
			uikit.FormInput(&apiKey, "Zotero API key (optional)",
				uikit.WithDescription("Enables writes (collections, items, tags). Leave blank for reads only. Create one at zotero.org/settings/keys/new"),
				uikit.WithPassword(),
				uikit.WithValidation(func(s string) error {
					if s == "" {
						return nil
					}
					return zot.ValidateAPIKey(s)
				})),
		)).Run(); err != nil {
			return "", "", err
		}
	}
	if apiKey == "" {
		return "", "", nil
	}
	if err := zot.ValidateAPIKey(apiKey); err != nil {
		return "", "", cmdutil.UsageErrorf(cmd, "%v", err)
	}
	if !netutil.Online() {
		return "", "", cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — setup verifies the API key with Zotero before storing it").
			WithTry("re-run setup when online; reads work without a key, so `sci zot setup` with no --api-key still configures them")
	}
	info, err := verifyAPIKey(ctx, apiKey)
	switch {
	case keyRejected(err):
		return "", "", cmdutil.Coded(cmdutil.CodeUsage, "Zotero rejected the API key: %v", err).
			WithTry("check the key at https://www.zotero.org/settings/keys — it must exist and allow library access; nothing was saved")
	case err != nil:
		return "", "", cmdutil.Coded(cmdutil.CodeRuntime, "could not verify the API key with Zotero: %v", err).
			WithTry("Zotero did not answer — try again in a moment; nothing was saved")
	case !info.Access.UserWrite:
		// The key exists but cannot write, and writing is the only thing sci
		// stores a key for. Refusing here beats a 403 on the first write.
		return "", "", cmdutil.Coded(cmdutil.CodeUsage, "the API key is valid but has no write access to your library").
			WithTry("edit the key at https://www.zotero.org/settings/keys and enable 'Allow write access' (and group write access if you use --library shared); nothing was saved")
	}
	if existing != nil && existing.SharedGroupID != "" && !info.Access.GroupWrite(existing.SharedGroupID) {
		fmt.Fprintf(os.Stderr, "  note: this key cannot write to the shared group %s — --library shared writes will be refused by Zotero until the key allows group write access\n", existing.SharedGroupID)
	}
	return apiKey, strconv.Itoa(info.UserID), nil
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

func runSetup(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("clear") {
		result, err := zot.ClearSetup()
		if err != nil {
			return err
		}
		cmdutil.Output(cmd, result)
		return nil
	}

	sharedGroupID := cmd.String("shared-group-id")
	sharedGroupName := cmd.String("shared-group-name")
	dataDir := cmd.String("data-dir")
	apiKeyFlag := cmd.String("api-key")

	jsonMode := cmdutil.IsJSON(cmd)
	// `setup --json` with nothing to set → print the saved config (key
	// masked) and exit.
	if jsonMode && dataDir == "" && sharedGroupID == "" && apiKeyFlag == "" {
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

	existing, err := zot.LoadConfig()
	if err != nil {
		return err
	}

	// `setup --api-key K` on an already-configured library ADDS the key —
	// the data directory and shared group are kept as they are. That is the
	// one-liner every "no credential" error names as its fix, so it must not
	// stop to ask whether the user wants to overwrite a config it will not
	// change.
	addingKeyOnly := apiKeyFlag != "" && dataDir == "" && sharedGroupID == "" &&
		existing != nil && zot.ValidateDataDir(existing.DataDir) == nil
	if addingKeyOnly {
		dataDir = existing.DataDir
		sharedGroupID, sharedGroupName = existing.SharedGroupID, existing.SharedGroupName
	}

	// Overwrite guard. --force is the consent flag; without it an existing
	// config is never rewritten silently.
	//
	// This deliberately does not exempt --json. Quiet mode is not consent:
	// under --json the prompt cannot be shown, so ConfirmYes refuses with
	// cmdutil.ErrNeedsConsent and the caller is told to pass --force. The
	// exemption used to be here, justified by "the caller is expected to know
	// what they're doing" — which is the assumption every other confirmation
	// in the CLI stopped making.
	if !addingKeyOnly && !cmd.Bool("force") && zot.ConfigExists() {
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

	// The key is verified (and the user ID derived) before anything is
	// written, so a rejected key leaves the file exactly as it was.
	apiKey, userID, err := resolveAPIKey(ctx, cmd, existing, jsonMode)
	if err != nil {
		return err
	}

	// Auto-detect the shared group from the local database when the user
	// didn't pre-specify one.
	probe := zot.LocalGroupProbe(dataDir)

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
