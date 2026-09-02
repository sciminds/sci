package cli

// `sci zot setup` records one thing — where zotero.sqlite lives — and holds
// no credential of any kind. The flag that forgets it was called --logout,
// four lines above help text explaining that there is no account to log
// into. It is --clear, and the rename is deliberate: no alias, no shim, so
// a script passing the old name fails loudly instead of half-working.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/netutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// flagNames collects every name and alias a command declares.
func flagNames(cmd *cli.Command) []string {
	var out []string
	for _, f := range cmd.Flags {
		out = append(out, f.Names()...)
	}
	return out
}

func TestSetupCommand_ClearReplacesLogout(t *testing.T) {
	t.Parallel()
	cmd := setupCommand()
	names := flagNames(cmd)

	if !slices.Contains(names, "clear") {
		t.Errorf("setup declares no --clear flag; got %v", names)
	}
	if slices.Contains(names, "logout") {
		t.Errorf("--logout still parses; the rename is breaking on purpose, so no alias survives: %v", names)
	}
	if strings.Contains(cmd.Description, "--logout") {
		t.Errorf("setup --help still advertises --logout:\n%s", cmd.Description)
	}
	if !strings.Contains(cmd.Description, "--clear") {
		t.Errorf("setup --help does not show --clear:\n%s", cmd.Description)
	}
	for _, f := range cmd.Flags {
		if slices.Contains(f.Names(), "clear") && strings.Contains(strings.ToLower(f.String()), "log out") {
			t.Errorf("--clear's usage string still talks about logging out: %s", f.String())
		}
	}
}

// runSetupCmd runs `zot setup` with args against a root that mirrors the real
// mount, and returns the Run error.
func runSetupCmd(t *testing.T, args ...string) error {
	t.Helper()
	var jsonOut bool
	root := &cli.Command{
		Name:  "zot",
		Flags: []cli.Flag{cmdutil.JSONFlag(&jsonOut)},
		// Mirrors cmd/sci/root.go's Before: --json is what turns quiet mode
		// on, and quiet mode is what makes a confirmation refuse instead of
		// reaching for a TTY. Setting the global directly would test the
		// guard without testing the wiring that arms it.
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			return ctx, nil
		},
		Commands: []*cli.Command{setupCommand()},
	}
	t.Cleanup(func() { uikit.SetQuiet(false) })
	return root.Run(context.Background(), slices.Concat([]string{"zot", "setup"}, args))
}

// TestSetup_JSONOverwriteNeedsConsent pins the last instance of the
// "non-interactive is not consent" bug. The overwrite guard was
// `!jsonMode && !force`, so --json skipped the question entirely and
// rewrote a saved config — the comment above it said the caller "is
// expected to know what they're doing". Every other confirmation in the
// CLI stopped assuming that; this one is reached by a different route
// (an if-condition rather than cmdutil.Confirm's quiet check), so the
// contract has to be pinned here too.
func TestSetup_JSONOverwriteNeedsConsent(t *testing.T) {
	original := withTestConfig(t, "42", "6506098")
	replacement := t.TempDir()
	seedMinimalDB(t, replacement, "6506098")

	before, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if before == nil || before.DataDir != original {
		t.Fatalf("precondition: config should point at %q, got %+v", original, before)
	}

	runErr := runSetupCmd(t, "--json", "--data-dir", replacement)
	if runErr == nil {
		t.Fatal("setup --json overwrote an existing config without consent")
	}
	if !errors.Is(runErr, cmdutil.ErrNeedsConsent) {
		t.Fatalf("want ErrNeedsConsent, got %T: %v", runErr, runErr)
	}

	after, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after refusal: %v", err)
	}
	if after.DataDir != original {
		t.Errorf("config was rewritten despite the refusal: %q → %q", original, after.DataDir)
	}
}

// TestSetup_JSONOverwriteWithForce is the other half: --force is the consent
// flag, so the same command must go through. Without this the fix above
// would just make `setup --json` impossible to use on a configured machine.
func TestSetup_JSONOverwriteWithForce(t *testing.T) {
	withTestConfig(t, "42", "6506098")
	replacement := t.TempDir()
	seedMinimalDB(t, replacement, "6506098")

	// stdout carries the result payload; keep it out of the test log.
	old := os.Stdout
	devnull, _ := os.Open(os.DevNull)
	os.Stdout = devnull
	runErr := runSetupCmd(t, "--json", "--force", "--data-dir", replacement)
	os.Stdout = old
	_ = devnull.Close()

	if runErr != nil {
		t.Fatalf("setup --json --force: %v", runErr)
	}
	after, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if after.DataDir != replacement {
		t.Errorf("--force did not apply the new data dir: got %q, want %q", after.DataDir, replacement)
	}
}

// withFakeKeyVerifier swaps the /keys/current call for a canned answer and
// points the connectivity probe at a live local server, so the setup tests
// exercise the verify-then-store flow without the network. Both are package
// globals, so these tests do not run in parallel.
func withFakeKeyVerifier(t *testing.T, userID int, verifyErr error) *int {
	t.Helper()
	calls := 0
	orig := verifyAPIKey
	verifyAPIKey = func(_ context.Context, _ string) (*api.KeyInfo, error) {
		calls++
		if verifyErr != nil {
			return nil, verifyErr
		}
		return &api.KeyInfo{UserID: userID, Username: "alice", Access: api.KeyAccess{UserLibrary: true, UserWrite: true}}, nil
	}
	t.Cleanup(func() { verifyAPIKey = orig })

	online := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(online.Close)
	netutil.SetProbeURL(online.URL)
	t.Cleanup(netutil.ResetProbeURL)
	return &calls
}

// TestSetup_APIKeyIsVerifiedAndStoredWithUserID: the key goes to Zotero
// first, and what comes back — the user ID — is stored beside it. Nobody
// types a user ID.
func TestSetup_APIKeyIsVerifiedAndStoredWithUserID(t *testing.T) {
	withTestConfig(t, "42", "6506098")
	calls := withFakeKeyVerifier(t, 17450224, nil)
	replacement := t.TempDir()
	seedMinimalDB(t, replacement, "6506098")

	if err := runSetupCmdQuietly(t, "--json", "--force", "--data-dir", replacement, "--api-key", "zABCDEFGHIJKLMNOPQRSTUVW"); err != nil {
		t.Fatalf("setup with --api-key: %v", err)
	}
	if *calls != 1 {
		t.Errorf("key verified %d times, want 1", *calls)
	}
	cfg, err := zot.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "zABCDEFGHIJKLMNOPQRSTUVW" || cfg.UserID != "17450224" {
		t.Errorf("stored credential = %q / %q", cfg.APIKey, cfg.UserID)
	}
}

// TestSetup_RejectedAPIKeyWritesNothing: a key Zotero refuses is not stored,
// and the rest of the config is left exactly as it was.
func TestSetup_RejectedAPIKeyWritesNothing(t *testing.T) {
	original := withTestConfig(t, "42", "6506098")
	withFakeKeyVerifier(t, 0, &api.HTTPError{Method: "GET", Path: "/keys/current", Status: http.StatusForbidden})

	err := runSetupCmdQuietly(t, "--json", "--api-key", "zBADBADBADBADBADBADBADBAD")
	if err == nil {
		t.Fatal("a rejected key must fail setup")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok || coded.Code != cmdutil.CodeUsage {
		t.Fatalf("want a usage CodedError, got %T: %v", err, err)
	}
	cfg, loadErr := zot.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if cfg.APIKey != "" || cfg.DataDir != original {
		t.Errorf("config changed after a rejected key: %+v", cfg)
	}
}

// TestSetup_UnreachableZoteroIsNotARejectedKey: a 5xx or a dropped
// connection while verifying must not read as "your key is wrong" — the
// remedy is to try again, and the code says so.
func TestSetup_UnreachableZoteroIsNotARejectedKey(t *testing.T) {
	withTestConfig(t, "42", "6506098")
	withFakeKeyVerifier(t, 0, &api.HTTPError{Method: "GET", Path: "/keys/current", Status: http.StatusBadGateway})

	err := runSetupCmdQuietly(t, "--json", "--api-key", "zABCDEFGHIJKLMNOPQRSTUVW")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok || coded.Code != cmdutil.CodeRuntime {
		t.Fatalf("want a runtime CodedError, got %T: %v", err, err)
	}
	if cfg, _ := zot.LoadConfig(); cfg.APIKey != "" {
		t.Errorf("an unverified key was stored: %+v", cfg)
	}
}

// TestSetup_ReadOnlyKeyIsRefused: a key that exists but cannot write is
// useless to sci — writing is the only thing it stores a key for — so it is
// refused at setup with the permission to enable, not stored to 403 later.
func TestSetup_ReadOnlyKeyIsRefused(t *testing.T) {
	withTestConfig(t, "42", "6506098")
	orig := verifyAPIKey
	verifyAPIKey = func(_ context.Context, _ string) (*api.KeyInfo, error) {
		return &api.KeyInfo{UserID: 17450224, Access: api.KeyAccess{UserLibrary: true, UserWrite: false}}, nil
	}
	t.Cleanup(func() { verifyAPIKey = orig })
	withOnlineProbe(t)

	err := runSetupCmdQuietly(t, "--json", "--api-key", "zREADONLYREADONLYREADONLY")
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok || coded.Code != cmdutil.CodeUsage || !strings.Contains(coded.Try, "write access") {
		t.Fatalf("want a usage error naming write access, got %T: %v", err, err)
	}
	if cfg, _ := zot.LoadConfig(); cfg.APIKey != "" {
		t.Errorf("a read-only key was stored: %+v", cfg)
	}
}

// TestSetup_APIKeyOnlyAddsToExistingConfig pins the one-liner every
// "no credential" error names as its fix: `sci zot setup --api-key K` on a
// configured library stores the key without asking to overwrite a config it
// does not otherwise change. --json here proves no prompt was reached.
func TestSetup_APIKeyOnlyAddsToExistingConfig(t *testing.T) {
	original := withTestConfig(t, "42", "6506098")
	withFakeKeyVerifier(t, 17450224, nil)

	if err := runSetupCmdQuietly(t, "--json", "--api-key", "zABCDEFGHIJKLMNOPQRSTUVW"); err != nil {
		t.Fatalf("setup --api-key on an existing config: %v", err)
	}
	cfg, err := zot.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != original {
		t.Errorf("data dir moved: %q → %q", original, cfg.DataDir)
	}
	if cfg.SharedGroupID != "6506098" {
		t.Errorf("shared group lost: %+v", cfg)
	}
	if !cfg.HasCredential() {
		t.Errorf("key not stored: %+v", cfg)
	}
}

// TestSetup_JSONStatusMasksTheKey: `setup --json` with nothing to set is a
// status report, and the key in it is a prefix, never the whole credential.
func TestSetup_JSONStatusMasksTheKey(t *testing.T) {
	dataDir := withTestConfig(t, "42", "6506098")
	if err := zot.SaveConfig(&zot.Config{DataDir: dataDir, SharedGroupID: "6506098", APIKey: "zABCDEFGHIJKLMNOPQRSTUVW", UserID: "17450224"}); err != nil {
		t.Fatal(err)
	}
	read, restore := captureStdout(t)
	runErr := runSetupCmd(t, "--json")
	out := read()
	restore()
	if runErr != nil {
		t.Fatalf("setup --json: %v", runErr)
	}
	if strings.Contains(out, "EFGHIJ") {
		t.Errorf("more than a prefix of the API key leaked into the status report: %s", out)
	}
	if !strings.Contains(out, "zABC") {
		t.Errorf("masked prefix missing from the status report: %s", out)
	}
}

// runSetupCmdQuietly is runSetupCmd with stdout swallowed — the result
// payload is not what these tests assert on.
func runSetupCmdQuietly(t *testing.T, args ...string) error {
	t.Helper()
	read, restore := captureStdout(t)
	err := runSetupCmd(t, args...)
	_ = read()
	restore()
	return err
}
