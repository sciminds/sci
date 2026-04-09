// Package brew wraps brew bundle commands to provide atomic Brewfile-synced
// package management. The Runner interface enables testing without shelling out.
package brew

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	BundleInstallLive(file string, onLine func(string), onSuspend, onResume func()) (string, error)
	BundleCheck(file string) ([]string, error)
	BundleCleanup(file string) (string, error)
	BundleDump(file string) error
	BundleList(file, pkgType string) ([]string, error)
	Info(names []string, isCask bool) ([]PackageInfo, error)
	Update(onLine func(string)) error
	Outdated() ([]OutdatedPackage, error)
	Upgrade(onLine func(string)) (string, error)
	UVOutdated() ([]OutdatedPackage, error)
	UVUpgrade(onLine func(string)) (string, error)
}

// BundleRunner shells out to brew bundle.
type BundleRunner struct{}

func (BundleRunner) BundleAdd(file, pkg, pkgType string) error {
	args := []string{"bundle", "add", "--file=" + file}
	if pkgType != "" {
		args = append(args, "--"+pkgType)
	}
	args = append(args, pkg)
	return runBrew(args...)
}

func (BundleRunner) BundleRemove(file, pkg, pkgType string) error {
	args := []string{"bundle", "remove", "--file=" + file}
	if pkgType != "" {
		args = append(args, "--"+pkgType)
	}
	args = append(args, pkg)
	return runBrew(args...)
}

func (BundleRunner) BundleInstall(file string) (string, error) {
	return runBrewOutput("bundle", "install", "--file="+file)
}

func (BundleRunner) BundleInstallLive(file string, onLine func(string), onSuspend, onResume func()) (string, error) {
	return runBrewInteractive(onLine, onSuspend, onResume, "bundle", "install", "--file="+file)
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

func (BundleRunner) BundleCleanup(file string) (string, error) {
	return runBrewOutput("bundle", "cleanup", "--force", "--file="+file)
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

func (BundleRunner) Update(onLine func(string)) error {
	_, err := runBrewLive(onLine, "update")
	return err
}

func (BundleRunner) Outdated() ([]OutdatedPackage, error) {
	out, err := runBrewOutput("outdated", "--json=v2")
	if err != nil {
		return nil, err
	}
	return parseOutdated(out)
}

func (BundleRunner) Upgrade(onLine func(string)) (string, error) {
	return runBrewLive(onLine, "upgrade")
}

func (BundleRunner) UVOutdated() ([]OutdatedPackage, error) {
	cmd := exec.Command("uv", "tool", "list", "--outdated")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv tool list --outdated: %w", err)
	}
	return parseUVOutdated(string(out)), nil
}

func (BundleRunner) UVUpgrade(onLine func(string)) (string, error) {
	return runLive(onLine, "uv", "tool", "upgrade", "--all")
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

// runLive runs a command, captures its combined output, and calls
// onLine for each non-empty line as it arrives. Returns the full output.
func runLive(onLine func(string), name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}

	var full strings.Builder
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		full.WriteString(line + "\n")
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && onLine != nil {
			onLine(trimmed)
		}
	}

	if err := cmd.Wait(); err != nil {
		return full.String(), err
	}
	return full.String(), nil
}

// runBrewLive runs a brew command via runLive.
func runBrewLive(onLine func(string), args ...string) (string, error) {
	return runLive(onLine, "brew", args...)
}

// runBrewInteractive is like runBrewLive but suspends the caller's UI when
// the process stalls (e.g. waiting for a sudo password prompt). It connects
// stdin so interactive prompts can be answered, and calls onSuspend/onResume
// to hide/show the spinner around the stall.
func runBrewInteractive(onLine func(string), onSuspend, onResume func(), args ...string) (string, error) {
	const stallTimeout = 2 * time.Second

	cmd := exec.Command("brew", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}

	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	var full strings.Builder
	suspended := false

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				goto wait
			}
			full.WriteString(line + "\n")
			if suspended {
				suspended = false
				if onResume != nil {
					onResume()
				}
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && onLine != nil {
				onLine(trimmed)
			}
		case <-time.After(stallTimeout):
			if !suspended {
				suspended = true
				if onSuspend != nil {
					onSuspend()
				}
			}
		}
	}

wait:
	if suspended && onResume != nil {
		onResume()
	}
	if err := cmd.Wait(); err != nil {
		return full.String(), err
	}
	return full.String(), nil
}

func runBrew(args ...string) error {
	cmd := exec.Command("brew", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
