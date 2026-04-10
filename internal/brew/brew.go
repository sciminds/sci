// Package brew wraps brew bundle commands to provide atomic Brewfile-synced
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

// Runner abstracts brew bundle commands for testability.
type Runner interface {
	BundleAdd(file, pkg, pkgType string) error
	BundleRemove(file, pkg, pkgType string) error
	BundleInstall(file string) (string, error)
	BundleCheck(file string) ([]string, error)
	BundleCleanup(file string) (string, error)
	BundleDump(file string) error
	BundleDumpLive(file string) error
	BundleList(file, pkgType string) ([]string, error)
	Info(names []string, isCask bool) ([]PackageInfo, error)
	Update() error
	Outdated() ([]OutdatedPackage, error)
	Upgrade() (string, error)
	UVOutdated() ([]OutdatedPackage, error)
	UVUpgrade() (string, error)
	UVToolList() ([]string, error)
}

// BundleRunner shells out to brew bundle.
type BundleRunner struct{}

func (BundleRunner) BundleAdd(file, pkg, pkgType string) error {
	args := []string{"bundle", "add", "--file=" + file}
	if pkgType != "" {
		args = append(args, "--"+pkgType)
	}
	args = append(args, pkg)
	_, err := runBrewDirect(args...)
	return err
}

func (BundleRunner) BundleRemove(file, pkg, pkgType string) error {
	args := []string{"bundle", "remove", "--file=" + file}
	if pkgType != "" {
		args = append(args, "--"+pkgType)
	}
	args = append(args, pkg)
	_, err := runBrewDirect(args...)
	return err
}

func (BundleRunner) BundleInstall(file string) (string, error) {
	return runBrewLive("bundle", "install", "--verbose", "--file="+file)
}

// BundleCheck runs `brew bundle check --verbose` and returns the names of
// missing packages. An empty slice means all dependencies are satisfied.
func (BundleRunner) BundleCheck(file string) ([]string, error) {
	// brew bundle check exits non-zero when deps are missing, so we must
	// capture stdout regardless of exit code (runBrewOutput discards it on
	// error). Use CombinedOutput and parse the text for missing packages.
	cmd := exec.Command("brew", "bundle", "check", "--verbose", "--file="+file)
	out, _ := cmd.CombinedOutput()
	return parseBundleCheck(string(out)), nil
}

// BundleDump runs `brew bundle dump` to write the current system state to file.
// It uses --force to overwrite and --no-vscode to skip editor extensions.
func (BundleRunner) BundleDump(file string) error {
	_, err := runBrewOutput("bundle", "dump", "--force", "--no-vscode", "--file="+file)
	return err
}

// BundleDumpLive is like BundleDump but connects stdin so interactive
// prompts (e.g. sudo password) are visible.
func (BundleRunner) BundleDumpLive(file string) error {
	_, err := runBrewLive("bundle", "dump", "--force", "--no-vscode", "--file="+file)
	return err
}

func (BundleRunner) BundleCleanup(file string) (string, error) {
	return runBrewLive("bundle", "cleanup", "--force", "--file="+file)
}

func (BundleRunner) BundleList(file, pkgType string) ([]string, error) {
	args := []string{"bundle", "list", "--file=" + file}
	if pkgType != "" {
		args = append(args, "--"+pkgType)
	} else {
		args = append(args, "--all")
	}
	out, err := runBrewOutput(args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
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
	out, err := runBrewOutput(args...)
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
		pkgs := make([]PackageInfo, len(info.Casks))
		for i, c := range info.Casks {
			pkgs[i] = PackageInfo{Name: c.Token, Desc: c.Desc, Version: c.Version, Type: "cask"}
		}
		return pkgs, nil
	}

	pkgs := make([]PackageInfo, len(info.Formulae))
	for i, f := range info.Formulae {
		pkgs[i] = PackageInfo{Name: f.Name, Desc: f.Desc, Version: f.Versions.Stable, Type: "formula"}
	}
	return pkgs, nil
}

// OutdatedPackage holds info about a single outdated package.
type OutdatedPackage struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	CurrentVersion   string `json:"current_version"`
	Pinned           bool   `json:"pinned"`
}

func (BundleRunner) Update() error {
	_, err := runBrewLive("update")
	return err
}

func (BundleRunner) Outdated() ([]OutdatedPackage, error) {
	out, err := runBrewOutput("outdated", "--json=v2")
	if err != nil {
		return nil, err
	}
	return parseOutdated(out)
}

func (BundleRunner) Upgrade() (string, error) {
	return runBrewLive("upgrade")
}

func (BundleRunner) UVOutdated() ([]OutdatedPackage, error) {
	cmd := exec.Command("uv", "tool", "list", "--outdated")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv tool list --outdated: %w", err)
	}
	return parseUVOutdated(string(out)), nil
}

func (BundleRunner) UVUpgrade() (string, error) {
	cmd := exec.Command("uv", "tool", "upgrade", "--all")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return "", cmd.Run()
}

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

// runBrewLive runs a brew command with a PTY for stdout/stderr so output
// streams in real-time (brew bundle buffers without a PTY). Stdin remains
// the real terminal so sudo password prompts work via /dev/tty.
//
// This is deliberately minimal — no stall detection, no callbacks, no
// output parsing. The PTY just forces line-buffered output from brew.
func runBrewLive(args ...string) (string, error) {
	ptmx, pts, err := pty.Open()
	if err != nil {
		// Fallback: direct passthrough if PTY unavailable.
		return runBrewDirect(args...)
	}
	defer func() { _ = ptmx.Close() }()

	cmd := exec.Command("brew", args...)
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
