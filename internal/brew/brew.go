// Package brew wraps Homebrew and uv commands to provide Brewfile-synced
// package management. The Runner interface enables testing without shelling out.
package brew

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/creack/pty/v2"
	"github.com/samber/lo"
)

// DefaultBrewfile is the default Brewfile location (matches brew's XDG convention).
const DefaultBrewfile = "~/.config/homebrew/Brewfile"

// ExpandPath resolves ~ to the user's home directory.
func ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand home: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// PackageInfo holds a package name, description, and version.
type PackageInfo struct {
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Version string `json:"version,omitempty"`
	Type    string `json:"type"` // "formula", "cask", "uv", "go", "cargo"
}

// SystemSnapshot captures the installed state of the system at a point in time.
// Used by Sync, RunToolChecks, and Install to avoid redundant subprocess calls.
type SystemSnapshot struct {
	Leaves   []string // brew leaves -r (user-requested formulae)
	Formulae []string // brew list --formula --full-name (all installed, incl. deps)
	Casks    []string // brew list --cask
	Taps     []string // brew tap
	UVTools  []string // uv tool list
}

// CollectSnapshot queries all five data sources concurrently and returns the
// combined system state. Returns the first error encountered.
func CollectSnapshot(r Runner) (SystemSnapshot, error) {
	var (
		snap SystemSnapshot
		errs [5]error
		wg   sync.WaitGroup
	)
	wg.Add(5)
	go func() { defer wg.Done(); snap.Leaves, errs[0] = r.Leaves() }()
	go func() { defer wg.Done(); snap.Formulae, errs[1] = r.ListFormulae() }()
	go func() { defer wg.Done(); snap.Casks, errs[2] = r.ListCasks() }()
	go func() { defer wg.Done(); snap.Taps, errs[3] = r.Taps() }()
	go func() { defer wg.Done(); snap.UVTools, errs[4] = r.UVToolList() }()
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return SystemSnapshot{}, err
		}
	}
	return snap, nil
}

// IsInstalled reports whether a (type, name) pair is present in the snapshot.
// Uses the Formulae list (all installed, including deps) for brew entries.
func (s SystemSnapshot) IsInstalled(typ, name string) bool {
	switch typ {
	case "brew":
		return slices.Contains(s.Formulae, name)
	case "cask":
		return slices.Contains(s.Casks, name)
	case "tap":
		return slices.Contains(s.Taps, name)
	case "uv":
		return slices.Contains(s.UVTools, name)
	default:
		return false
	}
}

// Runner abstracts brew commands for testability.
type Runner interface {
	Info(names []string, isCask bool) ([]PackageInfo, error)
	Leaves() ([]string, error)
	ListFormulae() ([]string, error)
	ListCasks() ([]string, error)
	Taps() ([]string, error)
	DirectInstall(pkg, pkgType string) error
	DirectUninstall(pkg, pkgType string) error
	InstallFormulae(names []string) error
	InstallCasks(names []string) error
	InstallUVTools(names []string) error
	Update() error
	Outdated() ([]OutdatedPackage, error)
	Upgrade() (string, error)
	UVOutdated() ([]OutdatedPackage, error)
	UVUpgrade(names []string) (string, error)
	UVToolList() ([]string, error)
}

// BrewRunner shells out to brew.
type BrewRunner struct{}

// Leaves implements Runner. Returns user-requested formulae (not deps).
func (BrewRunner) Leaves() ([]string, error) {
	out, err := runBrewOutputLocal("leaves", "-r")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ListFormulae implements Runner. Returns all installed formulae (leaves + deps)
// with full tap-qualified names (e.g. "oven-sh/bun/bun" not just "bun").
func (BrewRunner) ListFormulae() ([]string, error) {
	out, err := runBrewOutputLocal("list", "--formula", "--full-name", "-1")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ListCasks implements Runner. Returns all installed casks.
func (BrewRunner) ListCasks() ([]string, error) {
	out, err := runBrewOutputLocal("list", "--cask")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Taps implements Runner. Returns user-added taps.
func (BrewRunner) Taps() ([]string, error) {
	out, err := runBrewOutputLocal("tap")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// DirectInstall implements Runner. Installs a single package by type.
func (BrewRunner) DirectInstall(pkg, pkgType string) error {
	switch pkgType {
	case "", "formula", "brew":
		_, err := runBrewLive("install", pkg)
		return err
	case "cask":
		_, err := runBrewLive("install", "--cask", pkg)
		return err
	case "tap":
		_, err := runBrewLive("tap", pkg)
		return err
	case "uv":
		cmd := exec.Command("uv", "tool", "install", pkg)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported package type for direct install: %s", pkgType)
	}
}

// DirectUninstall implements Runner. Uninstalls a single package by type.
func (BrewRunner) DirectUninstall(pkg, pkgType string) error {
	switch pkgType {
	case "", "formula", "brew":
		_, err := runBrewLive("uninstall", pkg)
		return err
	case "cask":
		_, err := runBrewLive("uninstall", "--cask", pkg)
		return err
	case "tap":
		_, err := runBrewLive("untap", pkg)
		return err
	case "uv":
		cmd := exec.Command("uv", "tool", "uninstall", pkg)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported package type for direct uninstall: %s", pkgType)
	}
}

// InstallFormulae implements Runner. Installs multiple formulae in one call.
func (BrewRunner) InstallFormulae(names []string) error {
	if len(names) == 0 {
		return nil
	}
	_, err := runBrewLive(slices.Concat([]string{"install"}, names)...)
	return err
}

// InstallCasks implements Runner. Installs multiple casks in one call.
func (BrewRunner) InstallCasks(names []string) error {
	if len(names) == 0 {
		return nil
	}
	_, err := runBrewLive(slices.Concat([]string{"install", "--cask"}, names)...)
	return err
}

// InstallUVTools implements Runner. Installs uv tools sequentially (no batch mode).
func (BrewRunner) InstallUVTools(names []string) error {
	for _, name := range names {
		cmd := exec.Command("uv", "tool", "install", name)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("uv tool install %s: %w", name, err)
		}
	}
	return nil
}

// Info fetches descriptions for formulae or casks via brew info --json=v2.
func (BrewRunner) Info(names []string, isCask bool) ([]PackageInfo, error) {
	if len(names) == 0 {
		return nil, nil
	}
	args := []string{"info", "--json=v2"}
	if isCask {
		args = append(args, "--cask")
	}
	args = append(args, names...)
	out, err := runBrewOutputLocal(args...)
	if err != nil {
		return nil, err
	}
	return parseBrewInfo(out, isCask)
}

// brewInfoJSON is the top-level brew info --json=v2 response.
type brewInfoJSON struct {
	Formulae []brewFormula `json:"formulae"`
	Casks    []brewCask    `json:"casks"`
}

type brewFormula struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Versions struct {
		Stable string `json:"stable"`
	} `json:"versions"`
}

type brewCask struct {
	Token   string `json:"token"`
	Desc    string `json:"desc"`
	Version string `json:"version"`
}

func parseBrewInfo(jsonData string, isCask bool) ([]PackageInfo, error) {
	var info brewInfoJSON
	if err := json.Unmarshal([]byte(jsonData), &info); err != nil {
		return nil, fmt.Errorf("parse brew info: %w", err)
	}

	if isCask {
		return lo.Map(info.Casks, func(c brewCask, _ int) PackageInfo {
			return PackageInfo{Name: c.Token, Desc: c.Desc, Version: c.Version, Type: "cask"}
		}), nil
	}

	return lo.Map(info.Formulae, func(f brewFormula, _ int) PackageInfo {
		return PackageInfo{Name: f.Name, Desc: f.Desc, Version: f.Versions.Stable, Type: "formula"}
	}), nil
}

// OutdatedPackage holds info about a single outdated package.
type OutdatedPackage struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	CurrentVersion   string `json:"current_version"`
	Pinned           bool   `json:"pinned"`
}

// Update implements Runner.
func (BrewRunner) Update() error {
	_, err := runBrewLive("update")
	return err
}

// Outdated implements Runner.
func (BrewRunner) Outdated() ([]OutdatedPackage, error) {
	out, err := runBrewOutput("outdated", "--json=v2")
	if err != nil {
		return nil, err
	}
	return parseOutdated(out)
}

// Upgrade implements Runner.
func (BrewRunner) Upgrade() (string, error) {
	return runBrewLive("upgrade")
}

// UVOutdated implements Runner. Returns empty if uv isn't installed
// (same rationale as UVToolList).
func (BrewRunner) UVOutdated() ([]OutdatedPackage, error) {
	if _, err := exec.LookPath("uv"); err != nil {
		return nil, nil
	}
	cmd := exec.Command("uv", "tool", "list", "--outdated")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv tool list --outdated: %w", err)
	}
	return parseUVOutdated(string(out)), nil
}

// UVUpgrade implements Runner.
func (BrewRunner) UVUpgrade(names []string) (string, error) {
	var out strings.Builder
	for _, name := range names {
		cmd := exec.Command("uv", "tool", "upgrade", name)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return out.String(), fmt.Errorf("uv tool upgrade %s: %w", name, err)
		}
	}
	return out.String(), nil
}

// UVToolList implements Runner. If uv is not on PATH, returns an empty
// slice (not an error) — uv is managed via a Brewfile entry and may not
// yet be installed on a fresh machine. Every caller of SystemSnapshot
// then sees "no uv tools installed," which is the correct interpretation
// when uv itself is missing.
func (BrewRunner) UVToolList() ([]string, error) {
	if _, err := exec.LookPath("uv"); err != nil {
		return nil, nil
	}
	cmd := exec.Command("uv", "tool", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv tool list: %w", err)
	}
	return parseUVToolList(string(out)), nil
}

// outdatedJSON is the top-level brew outdated --json=v2 response.
type outdatedJSON struct {
	Formulae []outdatedFormula `json:"formulae"`
	Casks    []outdatedCask    `json:"casks"`
}

type outdatedFormula struct {
	Name              string   `json:"name"`
	InstalledVersions []string `json:"installed_versions"`
	CurrentVersion    string   `json:"current_version"`
	Pinned            bool     `json:"pinned"`
}

type outdatedCask struct {
	Name              string   `json:"name"`
	InstalledVersions []string `json:"installed_versions"`
	CurrentVersion    string   `json:"current_version"`
}

func parseOutdated(jsonData string) ([]OutdatedPackage, error) {
	if strings.TrimSpace(jsonData) == "" {
		return nil, nil
	}
	var info outdatedJSON
	if err := json.Unmarshal([]byte(jsonData), &info); err != nil {
		return nil, fmt.Errorf("parse brew outdated: %w", err)
	}

	var pkgs []OutdatedPackage
	for _, f := range info.Formulae {
		installed := ""
		if len(f.InstalledVersions) > 0 {
			installed = f.InstalledVersions[len(f.InstalledVersions)-1]
		}
		pkgs = append(pkgs, OutdatedPackage{
			Name:             f.Name,
			InstalledVersion: installed,
			CurrentVersion:   f.CurrentVersion,
			Pinned:           f.Pinned,
		})
	}
	for _, c := range info.Casks {
		installed := ""
		if len(c.InstalledVersions) > 0 {
			installed = c.InstalledVersions[len(c.InstalledVersions)-1]
		}
		pkgs = append(pkgs, OutdatedPackage{
			Name:             c.Name,
			InstalledVersion: installed,
			CurrentVersion:   c.CurrentVersion,
		})
	}
	return pkgs, nil
}

// offlineEnv returns the current environment with variables set to prevent
// brew from making any network requests. Used for brew commands that are
// local reads (list, check, info) so they don't hang offline.
func offlineEnv() []string {
	return append(os.Environ(),
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ANALYTICS=1",
		"HOMEBREW_NO_GITHUB_API=1",
	)
}

// runBrewOutputLocal is like runBrewOutput but suppresses brew's auto-update.
// Use for commands that only read local state (list, info, outdated).
func runBrewOutputLocal(args ...string) (string, error) {
	cmd := exec.Command("brew", args...)
	cmd.Env = offlineEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// runBrewLive runs a brew command with a PTY for stdout/stderr so output
// streams in real-time with line buffering. Stdin remains the real terminal
// so sudo password prompts work via /dev/tty.
func runBrewLive(args ...string) (string, error) {
	return runBrewLiveWithEnv(nil, args...)
}

func runBrewLiveWithEnv(env []string, args ...string) (string, error) {
	ptmx, pts, err := pty.Open()
	if err != nil {
		// Fallback: direct passthrough if PTY unavailable.
		return runBrewDirect(args...)
	}
	defer func() { _ = ptmx.Close() }()

	cmd := exec.Command("brew", args...)
	cmd.Env = env
	cmd.Stdout = pts
	cmd.Stderr = pts
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		_ = pts.Close()
		return "", fmt.Errorf("start: %w", err)
	}
	_ = pts.Close() // close slave in parent; child inherited it

	// Splice PTY output to stderr in real-time.
	_, _ = io.Copy(os.Stderr, ptmx)

	if err := cmd.Wait(); err != nil {
		return "", err
	}
	return "", nil
}

// runBrewDirect runs a brew command with direct terminal access (no PTY).
// Used as a fallback and for commands that don't need real-time output.
func runBrewDirect(args ...string) (string, error) {
	cmd := exec.Command("brew", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return "", cmd.Run()
}

func runBrewOutput(args ...string) (string, error) {
	cmd := exec.Command("brew", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// parseUVToolList extracts package names from `uv tool list` output.
// Package lines look like: "marimo v0.22.4". Executable lines ("- marimo") are skipped.
var uvToolListRe = regexp.MustCompile(`^(\S+)\s+v\S+`)

func parseUVToolList(output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		if m := uvToolListRe.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

// parseUVOutdated extracts outdated packages from `uv tool list --outdated` output.
// Lines look like: "marimo v0.22.4 [latest: 0.23.0]"
// Executable lines (starting with "- ") are skipped.
var uvOutdatedRe = regexp.MustCompile(`^(\S+)\s+v(\S+)\s+\[latest:\s+(\S+)]`)

func parseUVOutdated(output string) []OutdatedPackage {
	var pkgs []OutdatedPackage
	for _, line := range strings.Split(output, "\n") {
		if m := uvOutdatedRe.FindStringSubmatch(line); m != nil {
			pkgs = append(pkgs, OutdatedPackage{
				Name:             m[1],
				InstalledVersion: m[2],
				CurrentVersion:   m[3],
			})
		}
	}
	return pkgs
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
