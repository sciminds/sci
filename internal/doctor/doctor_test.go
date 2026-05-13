package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/brew"
)

// TestMain neutralises the real `git xet install` shellout for every test
// in the package. checkIdentity auto-runs xet install when the binary is
// present but not registered — without this guard, running the test suite
// on a developer machine with git-xet installed would mutate the user's
// global git config.
func TestMain(m *testing.M) {
	gitXetInstallFn = func() error { return fmt.Errorf("test: gitXetInstallFn not stubbed") }
	os.Exit(m.Run())
}

func TestBoolStatus(t *testing.T) {
	if got := boolStatus(true); got != StatusPass {
		t.Errorf("boolStatus(true) = %q, want %q", got, StatusPass)
	}
	if got := boolStatus(false); got != StatusFail {
		t.Errorf("boolStatus(false) = %q, want %q", got, StatusFail)
	}
}

func TestCheckPreflight_Structure(t *testing.T) {
	sec := checkPreflight()

	if sec.Name != "Pre-flight" {
		t.Errorf("section name = %q, want %q", sec.Name, "Pre-flight")
	}
	if len(sec.Checks) != 3 {
		t.Fatalf("expected 3 checks (Homebrew, Xcode CLT, Shell), got %d", len(sec.Checks))
	}

	wantLabels := []string{"Homebrew", "Xcode CLT", "Shell"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
	}
}

func TestCheckIdentity_Structure(t *testing.T) {
	sec := checkIdentity()

	if sec.Name != "Identity" {
		t.Errorf("section name = %q, want %q", sec.Name, "Identity")
	}

	if len(sec.Checks) < 3 {
		t.Fatalf("expected at least 3 checks, got %d", len(sec.Checks))
	}

	wantLabels := []string{"Git user.name", "Git user.email", "GitHub CLI auth"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
		if c.Message == "" {
			t.Errorf("check %q has empty message", c.Label)
		}
	}
}

// setHFWhoami swaps the HF whoami hook for a test and restores it on cleanup.
func setHFWhoami(t *testing.T, fn func() (string, []string, error)) {
	t.Helper()
	orig := hfWhoamiFn
	hfWhoamiFn = fn
	t.Cleanup(func() { hfWhoamiFn = orig })
}

// setHFTokenPresent swaps the local-token-presence hook for a test.
func setHFTokenPresent(t *testing.T, present bool) {
	t.Helper()
	orig := hfTokenPresentFn
	hfTokenPresentFn = func() bool { return present }
	t.Cleanup(func() { hfTokenPresentFn = orig })
}

// isolateHFCache redirects the whoami cache to a temp file so tests don't
// leak into the user's real cache and vice versa.
func isolateHFCache(t *testing.T) {
	t.Helper()
	orig := hfCacheFile
	hfCacheFile = filepath.Join(t.TempDir(), "hf-whoami.json")
	t.Cleanup(func() { hfCacheFile = orig })
}

// setGitXetRegistered swaps the git-xet hook for a test and restores on cleanup.
func setGitXetRegistered(t *testing.T, fn func() bool) {
	t.Helper()
	orig := gitXetRegisteredFn
	gitXetRegisteredFn = fn
	t.Cleanup(func() { gitXetRegisteredFn = orig })
}

// setGitXetInstall swaps the `git xet install` hook for a test and restores
// it on cleanup. The default real function would shell out, so tests must
// always replace it (otherwise checkIdentity could mutate the developer's
// global git config on a machine where git-xet is installed).
func setGitXetInstall(t *testing.T, fn func() error) {
	t.Helper()
	orig := gitXetInstallFn
	gitXetInstallFn = fn
	t.Cleanup(func() { gitXetInstallFn = orig })
}

func findIdentityCheck(label string) *CheckResult {
	sec := checkIdentity()
	for i := range sec.Checks {
		if sec.Checks[i].Label == label {
			return &sec.Checks[i]
		}
	}
	return nil
}

func TestCheckIdentity_HuggingFace_NotAuthenticated(t *testing.T) {
	isolateHFCache(t)
	setHFTokenPresent(t, false)
	setHFWhoami(t, func() (string, []string, error) {
		t.Error("whoami must not be called when no token is present locally")
		return "", nil, nil
	})

	c := findIdentityCheck("Hugging Face auth")
	if c == nil {
		t.Fatal("Hugging Face auth check missing")
	}
	// Warn (not fail): sci cloud needs HF auth, but the rest of sci works
	// without it. Failing here would trip AllPassed for first-run users and
	// CI machines that never opted into cloud features.
	if c.Status != StatusWarn {
		t.Errorf("status = %q, want %q", c.Status, StatusWarn)
	}
	if !strings.Contains(c.Message, "hf auth login") {
		t.Errorf("message = %q, want hint containing 'hf auth login'", c.Message)
	}
}

func TestCheckIdentity_HuggingFace_NotInOrg(t *testing.T) {
	isolateHFCache(t)
	setHFTokenPresent(t, true)
	setHFWhoami(t, func() (string, []string, error) { return "alice", []string{"other-org"}, nil })

	c := findIdentityCheck("Hugging Face auth")
	if c == nil {
		t.Fatal("Hugging Face auth check missing")
	}
	if c.Status != StatusWarn {
		t.Errorf("status = %q, want %q", c.Status, StatusWarn)
	}
	if !strings.Contains(c.Message, "sciminds") {
		t.Errorf("message = %q, want mention of sciminds org", c.Message)
	}
	if !strings.Contains(c.Message, "alice") {
		t.Errorf("message = %q, want username", c.Message)
	}
}

func TestCheckIdentity_HuggingFace_OK(t *testing.T) {
	isolateHFCache(t)
	setHFTokenPresent(t, true)
	setHFWhoami(t, func() (string, []string, error) {
		return "alice", []string{"other", "sciminds"}, nil
	})

	c := findIdentityCheck("Hugging Face auth")
	if c == nil {
		t.Fatal("Hugging Face auth check missing")
	}
	if c.Status != StatusPass {
		t.Errorf("status = %q, want %q", c.Status, StatusPass)
	}
	if !strings.Contains(c.Message, "alice") {
		t.Errorf("message = %q, want username", c.Message)
	}
}

// TestCheckIdentity_HuggingFace_WhoamiFailsNoCache: user has a token but
// whoami fails (network flake) and there's no cached result yet. Should
// pass with a bare "logged in" — the token is hard evidence of a prior
// login, and the cloud command does its own org check at use time.
func TestCheckIdentity_HuggingFace_WhoamiFailsNoCache(t *testing.T) {
	isolateHFCache(t)
	setHFTokenPresent(t, true)
	setHFWhoami(t, func() (string, []string, error) {
		return "", nil, fmt.Errorf("context deadline exceeded")
	})

	c := findIdentityCheck("Hugging Face auth")
	if c == nil {
		t.Fatal("Hugging Face auth check missing")
	}
	if c.Status != StatusPass {
		t.Errorf("status = %q, want %q (logged-in user with no cache must pass)", c.Status, StatusPass)
	}
	if strings.Contains(c.Message, "not authenticated") {
		t.Errorf("message = %q, must not claim 'not authenticated' when token is present", c.Message)
	}
	if !strings.Contains(c.Message, "logged in") {
		t.Errorf("message = %q, should communicate that the user IS logged in", c.Message)
	}
}

// TestCheckIdentity_HuggingFace_WhoamiFailsUsesCache: after a previous
// successful whoami, doctor caches the user + org list. A later network
// blip falls back to that cache so the check stays stable instead of
// flipping between Pass and Warn — the original regression.
func TestCheckIdentity_HuggingFace_WhoamiFailsUsesCache(t *testing.T) {
	isolateHFCache(t)
	writeHFCache("alice", []string{"sciminds", "other"})

	setHFTokenPresent(t, true)
	setHFWhoami(t, func() (string, []string, error) {
		return "", nil, fmt.Errorf("context deadline exceeded")
	})

	c := findIdentityCheck("Hugging Face auth")
	if c == nil {
		t.Fatal("Hugging Face auth check missing")
	}
	if c.Status != StatusPass {
		t.Errorf("status = %q, want %q (cached membership should survive a network blip)", c.Status, StatusPass)
	}
	if !strings.Contains(c.Message, "alice") {
		t.Errorf("message = %q, should include the cached username", c.Message)
	}
	if !strings.Contains(c.Message, sciMindsOrg) {
		t.Errorf("message = %q, should mention the cached %s membership", c.Message, sciMindsOrg)
	}
}

// TestCheckIdentity_HuggingFace_SuccessUpdatesCache asserts that a
// successful whoami refreshes the cache, so the next run (with whoami
// failing) sees the latest user + orgs.
func TestCheckIdentity_HuggingFace_SuccessUpdatesCache(t *testing.T) {
	isolateHFCache(t)
	// Stale cache that doesn't match what whoami will return.
	writeHFCache("stale-user", []string{"stale-org"})

	setHFTokenPresent(t, true)
	setHFWhoami(t, func() (string, []string, error) {
		return "alice", []string{"sciminds"}, nil
	})

	_ = findIdentityCheck("Hugging Face auth")

	gotUser, gotOrgs, ok := readHFCache()
	if !ok {
		t.Fatal("cache not written after successful whoami")
	}
	if gotUser != "alice" {
		t.Errorf("cached user = %q, want %q (whoami result must replace stale cache)", gotUser, "alice")
	}
	if len(gotOrgs) != 1 || gotOrgs[0] != "sciminds" {
		t.Errorf("cached orgs = %v, want [sciminds]", gotOrgs)
	}
}

func TestCheckIdentity_GitXet_NotRegistered_AutoInstallSucceeds(t *testing.T) {
	// First registration probe says "not registered" → triggers install →
	// second probe (called after install) says "registered". This models
	// `git xet install` writing the config entry.
	probe := 0
	setGitXetRegistered(t, func() bool {
		probe++
		return probe > 1
	})
	installed := false
	setGitXetInstall(t, func() error {
		installed = true
		return nil
	})

	c := findIdentityCheck("git-xet")
	if c == nil {
		t.Fatal("git-xet check missing")
	}
	// If git-xet isn't installed locally, the binary-missing branch wins
	// before we ever reach the install path — skip in that case.
	if strings.Contains(c.Message, "git-xet not found") {
		t.Skip("git-xet binary not present on this machine")
	}
	if !installed {
		t.Error("expected gitXetInstallFn to be invoked when binary is present but not registered")
	}
	if c.Status != StatusPass {
		t.Errorf("status = %q, want %q (auto-install should succeed)", c.Status, StatusPass)
	}
	if !strings.Contains(c.Message, "auto-installed") {
		t.Errorf("message = %q, want mention of auto-install", c.Message)
	}
}

func TestCheckIdentity_GitXet_NotRegistered_AutoInstallFails(t *testing.T) {
	setGitXetRegistered(t, func() bool { return false })
	setGitXetInstall(t, func() error { return fmt.Errorf("permission denied") })

	c := findIdentityCheck("git-xet")
	if c == nil {
		t.Fatal("git-xet check missing")
	}
	if strings.Contains(c.Message, "git-xet not found") {
		t.Skip("git-xet binary not present on this machine")
	}
	if c.Status != StatusFail {
		t.Errorf("status = %q, want %q (install failed)", c.Status, StatusFail)
	}
	if !strings.Contains(c.Message, "git xet install") {
		t.Errorf("message = %q, want hint pointing to git xet install", c.Message)
	}
}

func TestCheckIdentity_GitXet_Registered(t *testing.T) {
	setGitXetRegistered(t, func() bool { return true })

	c := findIdentityCheck("git-xet")
	if c == nil {
		t.Fatal("git-xet check missing")
	}
	// Only assert pass when the binary is actually present in PATH.
	// Otherwise the LookPath branch (StatusFail with brew hint) is expected.
	if c.Status == StatusPass && !strings.Contains(c.Message, "registered") {
		t.Errorf("expected 'registered' message, got %q", c.Message)
	}
}

func TestParseHFWhoami(t *testing.T) {
	cases := []struct {
		in       string
		wantUser string
		wantOrgs []string
		wantErr  bool
	}{
		{"user=ejolly orgs=py-feat,nltools,sciminds\n", "ejolly", []string{"py-feat", "nltools", "sciminds"}, false},
		{"user=alice orgs=", "alice", nil, false},
		{"user=alice", "alice", nil, false},
		{"orgs=foo", "", nil, true},
		{"", "", nil, true},
	}
	for _, c := range cases {
		gotUser, gotOrgs, gotErr := parseHFWhoami(c.in)
		if (gotErr != nil) != c.wantErr {
			t.Errorf("parseHFWhoami(%q) err = %v, wantErr=%v", c.in, gotErr, c.wantErr)
			continue
		}
		if gotUser != c.wantUser {
			t.Errorf("parseHFWhoami(%q) user = %q, want %q", c.in, gotUser, c.wantUser)
		}
		if !strSliceEq(gotOrgs, c.wantOrgs) {
			t.Errorf("parseHFWhoami(%q) orgs = %v, want %v", c.in, gotOrgs, c.wantOrgs)
		}
	}
}

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunPreflightIdentity_ReturnsSections(t *testing.T) {
	sections := RunPreflightIdentity()

	if len(sections) != len(checkFuncs) {
		t.Fatalf("RunPreflightIdentity returned %d sections, want %d", len(sections), len(checkFuncs))
	}

	for i, sec := range sections {
		if sec.Name == "" {
			t.Errorf("section[%d] has empty name", i)
		}
		if len(sec.Checks) == 0 {
			t.Errorf("section %q has no checks", sec.Name)
		}
	}

	if sections[0].Name != "Pre-flight" {
		t.Errorf("first section = %q, want %q", sections[0].Name, "Pre-flight")
	}
	if sections[1].Name != "Identity" {
		t.Errorf("second section = %q, want %q", sections[1].Name, "Identity")
	}
}

func TestParseBrewfileNames(t *testing.T) {
	content := `brew "git"
brew "uv"
# a comment
cask "visual-studio-code"
`
	names := brew.ParseBrewfileNames(content)
	want := []string{"git", "uv", "visual-studio-code"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestParseBrewfileNames_Empty(t *testing.T) {
	names := brew.ParseBrewfileNames("")
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
}

func TestBrewfileEmbedded(t *testing.T) {
	if Brewfile == "" {
		t.Fatal("embedded Brewfile is empty")
	}
	names := brew.ParseBrewfileNames(Brewfile)
	if len(names) == 0 {
		t.Fatal("embedded Brewfile has no packages")
	}
}

func TestRunToolChecks(t *testing.T) {
	mock := &mockBrewRunner{
		// Simulate: git, node installed as formulae; uv NOT installed.
		listFormulaeResult: []string{"git", "node", "ffmpeg", "gh", "openssh", "oven-sh/bun/bun", "pixi", "sqlite", "rsync"},
		listCasksResult:    []string{"1password", "slack", "visual-studio-code", "vlc", "zed", "zoom", "quarto"},
		uvToolListResult:   []string{"marimo", "mystmd"},
	}
	infos, err := RunToolChecks(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(infos) == 0 {
		t.Fatal("expected tool infos from embedded Brewfile")
	}

	// uv should be marked as not installed (missing from formulae list).
	for _, ti := range infos {
		if ti.Name == "uv" && ti.Installed {
			t.Error("expected uv to be marked as not installed")
		}
		if ti.Name == "git" && !ti.Installed {
			t.Error("expected git to be marked as installed")
		}
		if ti.Name == "node" && !ti.Installed {
			t.Error("expected node to be marked as installed")
		}
		if ti.Name == "marimo" && !ti.Installed {
			t.Error("expected marimo to be marked as installed")
		}
	}
}

// mockBrewRunner implements brew.Runner for testing.
// All error fields default to nil (no error), so existing tests are unaffected.
type mockBrewRunner struct {
	// Snapshot fields — used by CollectSnapshot via RunToolChecks.
	leavesResult       []string
	leavesErr          error
	listFormulaeResult []string
	listFormulaeErr    error
	listCasksResult    []string
	listCasksErr       error
	tapsResult         []string
	tapsErr            error
	uvToolListResult   []string
	uvToolListErr      error

	// Install tracking.
	installFormulaeCalls [][]string
	installFormulaeErr   error
	installCasksCalls    [][]string
	installCasksErr      error
	installUVToolsCalls  [][]string
	installUVToolsErr    error

	// Update/upgrade.
	outdated      []brew.OutdatedPackage
	outdatedErr   error
	uvOutdated    []brew.OutdatedPackage
	uvOutdatedErr error
	updateErr     error
	upgradeCalls  int
	upgradeErr    error
	uvUpgCalls    int
	uvUpgradeErr  error
}

func (m *mockBrewRunner) Info(_ []string, _ bool) ([]brew.PackageInfo, error) {
	return nil, nil
}

func (m *mockBrewRunner) Leaves() ([]string, error) { return m.leavesResult, m.leavesErr }
func (m *mockBrewRunner) ListFormulae() ([]string, error) {
	return m.listFormulaeResult, m.listFormulaeErr
}
func (m *mockBrewRunner) ListCasks() ([]string, error)      { return m.listCasksResult, m.listCasksErr }
func (m *mockBrewRunner) Taps() ([]string, error)           { return m.tapsResult, m.tapsErr }
func (m *mockBrewRunner) DirectInstall(_, _ string) error   { return nil }
func (m *mockBrewRunner) DirectUninstall(_, _ string) error { return nil }
func (m *mockBrewRunner) InstallFormulae(names []string) error {
	m.installFormulaeCalls = append(m.installFormulaeCalls, names)
	return m.installFormulaeErr
}
func (m *mockBrewRunner) InstallCasks(names []string) error {
	m.installCasksCalls = append(m.installCasksCalls, names)
	return m.installCasksErr
}
func (m *mockBrewRunner) InstallUVTools(names []string) error {
	m.installUVToolsCalls = append(m.installUVToolsCalls, names)
	return m.installUVToolsErr
}
func (m *mockBrewRunner) UVToolList() ([]string, error) { return m.uvToolListResult, m.uvToolListErr }
func (m *mockBrewRunner) Update() error                 { return m.updateErr }
func (m *mockBrewRunner) Outdated() ([]brew.OutdatedPackage, error) {
	return m.outdated, m.outdatedErr
}
func (m *mockBrewRunner) Upgrade() (string, error) {
	m.upgradeCalls++
	return "", m.upgradeErr
}
func (m *mockBrewRunner) UVOutdated() ([]brew.OutdatedPackage, error) {
	return m.uvOutdated, m.uvOutdatedErr
}
func (m *mockBrewRunner) UVUpgrade(_ []string) (string, error) {
	m.uvUpgCalls++
	return "", m.uvUpgradeErr
}
func TestRunToolChecks_SnapshotError(t *testing.T) {
	mock := &mockBrewRunner{listFormulaeErr: fmt.Errorf("brew not installed")}
	infos, err := RunToolChecks(mock)
	if err == nil {
		t.Fatal("expected error from RunToolChecks when snapshot fails")
	}
	if infos != nil {
		t.Errorf("expected nil infos on error, got %v", infos)
	}
}

func TestCheckPreflight_ShellUnset(t *testing.T) {
	t.Setenv("SHELL", "")

	sec := checkPreflight()

	var shellCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Shell" {
			shellCheck = &sec.Checks[i]
			break
		}
	}
	if shellCheck == nil {
		t.Fatal("Shell check not found in pre-flight section")
	}
	if shellCheck.Status != StatusWarn {
		t.Errorf("Shell status = %q, want %q when SHELL is empty", shellCheck.Status, StatusWarn)
	}
	if shellCheck.Message != "not set — expected zsh" {
		t.Errorf("Shell message = %q, want %q", shellCheck.Message, "not set — expected zsh")
	}
}

func TestCheckPreflight_NonZshShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")

	sec := checkPreflight()

	var shellCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Shell" {
			shellCheck = &sec.Checks[i]
			break
		}
	}
	if shellCheck == nil {
		t.Fatal("Shell check not found")
	}
	if shellCheck.Status != StatusWarn {
		t.Errorf("Shell status = %q, want %q for bash", shellCheck.Status, StatusWarn)
	}
	if shellCheck.Message != "bash — expected zsh" {
		t.Errorf("Shell message = %q, want %q", shellCheck.Message, "bash — expected zsh")
	}
}

func TestCheckPreflight_BrewMissing(t *testing.T) {
	// Hide brew from PATH by setting PATH to an empty directory.
	t.Setenv("PATH", t.TempDir())

	sec := checkPreflight()

	var brewCheck *CheckResult
	for i := range sec.Checks {
		if sec.Checks[i].Label == "Homebrew" {
			brewCheck = &sec.Checks[i]
			break
		}
	}
	if brewCheck == nil {
		t.Fatal("Homebrew check not found")
	}
	if brewCheck.Status != StatusFail {
		t.Errorf("Homebrew status = %q, want %q when brew not in PATH", brewCheck.Status, StatusFail)
	}
	if brewCheck.Message != "not installed — visit https://brew.sh" {
		t.Errorf("Homebrew message = %q", brewCheck.Message)
	}
}
