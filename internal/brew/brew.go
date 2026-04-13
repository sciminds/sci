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
	"strings"

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

// Runner abstracts brew commands for testability.
type Runner interface {
	BundleInstall(file string) (string, error)
	BundleCheck(file string) ([]string, error)
	BundleList(file, pkgType string) ([]string, error)
	Info(names []string, isCask bool) ([]PackageInfo, error)
	Leaves() ([]string, error)
	ListFormulae() ([]string, error)
	ListCasks() ([]string, error)
	Taps() ([]string, error)
	DirectInstall(pkg, pkgType string) error
	DirectUninstall(pkg, pkgType string) error
	Update() error
	Outdated() ([]OutdatedPackage, error)
	Upgrade() (string, error)
	UVOutdated() ([]OutdatedPackage, error)
	UVUpgrade(names []string) (string, error)
	UVToolList() ([]string, error)
}

// BundleRunner shells out to brew.
type BundleRunner struct{}

// BundleInstall implements Runner.
func (BundleRunner) BundleInstall(file string) (string, error) {
	return runBrewLive("bundle", "install", "--verbose", "--no-upgrade", "--file="+file)
}

// BundleCheck runs `brew bundle check --verbose` and returns the names of
// missing packages. An empty slice means all dependencies are satisfied.
func (BundleRunner) BundleCheck(file string) ([]string, error) {
	// brew bundle check exits non-zero when deps are missing — that's normal.
	// We distinguish "missing deps" from real failures by inspecting the
	// output: if it contains recognized package lines or the "satisfied"
	// message, the exit code is just the missing-deps signal. Otherwise
	// something is actually broken (bad Brewfile, brew not found, etc.).
	cmd := exec.Command("brew", "bundle", "check", "--verbose", "--file="+file)
	cmd.Env = offlineEnv()
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err != nil && !isBundleCheckOutput(s) {
		return nil, fmt.Errorf("brew bundle check: %w: %s", err, s)
	}
	return parseBundleCheck(s), nil
}

// BundleList implements Runner.
func (BundleRunner) BundleList(file, pkgType string) ([]string, error) {
	args := []string{"bundle", "list", "--file=" + file}
	if pkgType != "" {
		args = append(args, "--"+pkgType)
	} else {
		args = append(args, "--all")
	}
	out, err := runBrewOutputLocal(args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Leaves implements Runner. Returns user-requested formulae (not deps).
func (BundleRunner) Leaves() ([]string, error) {
	out, err := runBrewOutputLocal("leaves", "-r")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ListFormulae implements Runner. Returns all installed formulae (leaves + deps)
// with full tap-qualified names (e.g. "oven-sh/bun/bun" not just "bun").
func (BundleRunner) ListFormulae() ([]string, error) {
	out, err := runBrewOutputLocal("list", "--formula", "--full-name", "-1")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ListCasks implements Runner. Returns all installed casks.
func (BundleRunner) ListCasks() ([]string, error) {
	out, err := runBrewOutputLocal("list", "--cask")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Taps implements Runner. Returns user-added taps.
func (BundleRunner) Taps() ([]string, error) {
	out, err := runBrewOutputLocal("tap")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// DirectInstall implements Runner. Installs a single package by type.
func (BundleRunner) DirectInstall(pkg, pkgType string) error {
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
func (BundleRunner) DirectUninstall(pkg, pkgType string) error {
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

// Info fetches descriptions for formulae or casks via brew info --json=v2.
func (BundleRunner) Info(names []string, isCask bool) ([]PackageInfo, error) {
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
func (BundleRunner) Update() error {
	_, err := runBrewLive("update")
	return err
}

// Outdated implements Runner.
func (BundleRunner) Outdated() ([]OutdatedPackage, error) {
	out, err := runBrewOutput("outdated", "--json=v2")
	if err != nil {
		return nil, err
	}
	return parseOutdated(out)
}

// Upgrade implements Runner.
func (BundleRunner) Upgrade() (string, error) {
	return runBrewLive("upgrade")
}

// UVOutdated implements Runner.
func (BundleRunner) UVOutdated() ([]OutdatedPackage, error) {
	cmd := exec.Command("uv", "tool", "list", "--outdated")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv tool list --outdated: %w", err)
	}
	return parseUVOutdated(string(out)), nil
}

// UVUpgrade implements Runner.
func (BundleRunner) UVUpgrade(names []string) (string, error) {
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

// UVToolList implements Runner.
func (BundleRunner) UVToolList() ([]string, error) {
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
// Use for commands that only read local state (bundle list, info, outdated).
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
// streams in real-time (brew bundle buffers without a PTY). Stdin remains
// the real terminal so sudo password prompts work via /dev/tty.
//
// This is deliberately minimal — no stall detection, no callbacks, no
// output parsing. The PTY just forces line-buffered output from brew.
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

// isBundleCheckOutput returns true if the output looks like a valid
// `brew bundle check` response — either listing missing deps or
// confirming everything is satisfied. Used to distinguish a normal
// non-zero exit (missing deps) from a real failure (e.g. bad Brewfile).
func isBundleCheckOutput(s string) bool {
	return strings.Contains(s, "needs to be installed") ||
		strings.Contains(s, "dependencies are satisfied")
}

// parseBundleCheck extracts missing package names from `brew bundle check --verbose` output.
// Lines look like: "→ Formula git needs to be installed or updated."
var bundleCheckRe = regexp.MustCompile(`→ (?:Formula|Cask|uv Tool) (\S+) needs to be installed`)

func parseBundleCheck(output string) []string {
	var missing []string
	for _, m := range bundleCheckRe.FindAllStringSubmatch(output, -1) {
		missing = append(missing, m[1])
	}
	return missing
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
