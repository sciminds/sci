// Package brew wraps Homebrew and uv commands to provide Brewfile-synced
// package management. The Runner interface enables testing without shelling out.
package brew

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
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

	// ExternalCasks are cask names whose app artifact exists on disk but
	// `brew list --cask` doesn't report them — e.g. an app the user dragged
	// into /Applications, or a .pkg-based cask like Zoom that was installed
	// via the vendor's official installer. Populated by
	// [CollectSnapshotForBrewfile]; empty after a plain [CollectSnapshot].
	ExternalCasks []string
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
// For casks, also returns true when the app exists on disk but brew didn't
// track it (see [SystemSnapshot.ExternalCasks]).
func (s SystemSnapshot) IsInstalled(typ, name string) bool {
	switch typ {
	case "brew":
		return slices.Contains(s.Formulae, name)
	case "cask":
		return slices.Contains(s.Casks, name) || slices.Contains(s.ExternalCasks, name)
	case "tap":
		return slices.Contains(s.Taps, name)
	case "uv":
		return slices.Contains(s.UVTools, name)
	default:
		return false
	}
}

// CollectSnapshotForBrewfile collects a [SystemSnapshot] and augments it with
// external-cask detection for casks declared in brewfileContent. Use this in
// flows that have a Brewfile in hand — Install, Sync, [doctor.RunToolChecks] —
// so apps the user installed manually (drag-to-Applications, or .pkg-based
// casks installed via the vendor) don't show up as "missing".
//
// Detection failures are non-fatal: if [Runner.CaskAppPaths] errors, the
// returned snapshot has no ExternalCasks but is otherwise valid.
func CollectSnapshotForBrewfile(r Runner, brewfileContent string) (SystemSnapshot, error) {
	snap, err := CollectSnapshot(r)
	if err != nil {
		return snap, err
	}
	declared := lo.FilterMap(ParseBrewfileEntries(brewfileContent), func(e BrewfileEntry, _ int) (string, bool) {
		return e.Name, e.Type == "cask"
	})
	// Only probe casks brew doesn't already track — saves a `brew info` round-trip.
	candidates := lo.Filter(declared, func(name string, _ int) bool {
		return !slices.Contains(snap.Casks, name)
	})
	if len(candidates) == 0 {
		return snap, nil
	}
	external, _ := ResolveExternalCasks(r, candidates) // non-fatal
	snap.ExternalCasks = external
	return snap, nil
}

// ResolveExternalCasks queries cask metadata for the given names and returns
// those whose primary app artifact path exists on disk. Used by
// [CollectSnapshotForBrewfile]; exposed for callers that already know which
// candidates to check.
func ResolveExternalCasks(r Runner, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	paths, err := r.CaskAppPaths(names)
	if err != nil {
		return nil, err
	}
	var external []string
	for _, name := range names {
		for _, p := range paths[name] {
			if _, statErr := os.Stat(p); statErr == nil {
				external = append(external, name)
				break
			}
		}
	}
	return external, nil
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
	UVUpgrade(specs []string) (string, error)
	UVToolList() ([]string, error)

	// CaskAppPaths returns the .app filesystem paths declared by each named
	// cask's artifact metadata — both the `app` artifact and any
	// `uninstall.delete` entries that point at /Applications/*.app. Used to
	// detect casks the user installed manually so doctor doesn't try to
	// reinstall them. Missing names are simply absent from the result map.
	CaskAppPaths(names []string) (map[string][]string, error)
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
//
// Casks pass --adopt so an existing app at the destination (e.g. a VLC.app
// the user dragged in manually before sci managed it) is claimed by brew
// instead of failing the install. --adopt is a no-op when the destination
// is empty, so it's safe to pass unconditionally.
func (BrewRunner) DirectInstall(pkg, pkgType string) error {
	switch pkgType {
	case "", "formula", "brew":
		_, err := runBrewLive("install", pkg)
		return err
	case "cask":
		_, err := runBrewLive("install", "--cask", "--adopt", pkg)
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

// InstallCasks implements Runner. Installs casks one at a time so a single
// failure (e.g. a pre-existing app brew can't adopt) doesn't poison the
// rest of the batch. --adopt claims an existing app at the destination
// instead of failing — it's a no-op when nothing is there yet.
//
// Errors are accumulated via errors.Join so the caller sees every failure
// at once instead of just the first one.
func (BrewRunner) InstallCasks(names []string) error {
	var errs []error
	for _, name := range names {
		if _, err := runBrewLive("install", "--cask", "--adopt", name); err != nil {
			errs = append(errs, fmt.Errorf("install cask %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
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
	// `uv tool list --outdated` reports the highest *published* version, even
	// when that version can't be installed under the default (stable) resolver
	// — e.g. markitdown 0.1.6 declares a dependency that only exists as a
	// pre-release. Re-resolve each candidate and keep only the ones whose
	// newest *installable* version actually beats what's installed, so we never
	// nag about (or try to apply) an upgrade uv would refuse.
	return filterUVUpgradable(parseUVOutdated(string(out)), uvResolveVersion), nil
}

// uvResolveVersion resolves spec under uv's default (stable) resolver and
// returns the installable version of pkgName — the primary package — or ""
// when the spec doesn't resolve. `uv pip compile` reads the requirement from
// stdin and prints the fully-resolved set without touching any environment.
func uvResolveVersion(spec, pkgName string) (string, error) {
	cmd := exec.Command("uv", "pip", "compile", "-", "--no-annotate", "--no-header", "--quiet")
	cmd.Stdin = strings.NewReader(spec + "\n")
	out, err := cmd.Output()
	if err != nil {
		// Unsatisfiable under the stable resolver (the markitdown case): there
		// is no installable upgrade, so signal "nothing to offer."
		return "", err
	}
	return parseResolvedVersion(string(out), pkgName), nil
}

// filterUVUpgradable drops outdated candidates whose newest *installable*
// version (via resolve) doesn't actually beat the installed version, and
// rewrites CurrentVersion to that installable version for the ones it keeps.
// Probes run concurrently since each shells out to a network resolve. A probe
// that errors means the candidate can't be installed at all → drop it; a probe
// that succeeds but yields no version (parse miss) is kept unchanged so a
// genuine upgrade is never hidden by a quirk in the output.
func filterUVUpgradable(candidates []OutdatedPackage, resolve func(spec, pkgName string) (string, error)) []OutdatedPackage {
	if len(candidates) == 0 {
		return candidates
	}
	names := lo.Map(candidates, func(p OutdatedPackage, _ int) string { return p.Name })
	specs := ResolveUVSpecs(names)

	resolved := make([]string, len(candidates))
	resolveErr := make([]error, len(candidates))
	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resolved[i], resolveErr[i] = resolve(specs[i], candidates[i].Name)
		}(i)
	}
	wg.Wait()

	var out []OutdatedPackage
	for i, c := range candidates {
		switch {
		case resolveErr[i] != nil:
			// No installable version — held back (e.g. latest needs a pre-release).
			continue
		case resolved[i] == "":
			// Couldn't read the resolved version; keep the candidate as-is
			// rather than risk hiding a real upgrade.
			out = append(out, c)
		case newerVersion(resolved[i], c.InstalledVersion):
			c.CurrentVersion = resolved[i]
			out = append(out, c)
		}
	}
	return out
}

// UVUpgrade implements Runner. Reinstalls each tool at its newest installable
// version via `uv tool install <spec> --upgrade`. Plain `uv tool upgrade` is a
// no-op for tools installed with an exact-version pin (uv prints "Nothing to
// upgrade" plus a hint to reinstall to lift the pin) — that left pinned tools
// like `hf` stranded as outdated on every doctor/sci tools run. `--upgrade`
// (`-U`) ignores those pins, so it lifts them and upgrades in one shot.
//
// Unlike the earlier `<spec>@latest` form, `--upgrade` does not hard-pin to the
// highest *published* version: when that version is unsatisfiable (e.g.
// markitdown 0.1.6 depends on a dependency that only exists as a pre-release),
// uv backtracks to the highest *installable* version instead of erroring out.
//
// Specs come from the Brewfile when callers can resolve them, so bracket
// extras like `marimo[recommended]` survive the reinstall instead of being
// silently dropped. Continues past per-tool failures and joins errors at
// the end so one bad tool doesn't block the rest.
func (BrewRunner) UVUpgrade(specs []string) (string, error) {
	var errs []error
	for _, spec := range specs {
		cmd := exec.Command("uv", uvUpgradeArgs(spec)...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Errorf("uv tool install %s: %w", spec, err))
		}
	}
	return "", errors.Join(errs...)
}

// uvUpgradeArgs builds the `uv` argv that upgrades a single tool. Extracted so
// tests can assert on the flags without shelling out.
func uvUpgradeArgs(spec string) []string {
	return []string{"tool", "install", spec, "--upgrade"}
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

// CaskAppPaths implements Runner. Calls `brew info --cask --json=v2` for the
// given casks and extracts the .app paths brew would install or remove.
func (BrewRunner) CaskAppPaths(names []string) (map[string][]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	args := slices.Concat([]string{"info", "--json=v2", "--cask"}, names)
	out, err := runBrewOutputLocal(args...)
	if err != nil {
		return nil, err
	}
	return parseCaskAppPaths(out)
}

// caskWithArtifacts captures only the fields we need from brew info v2 for casks.
type caskWithArtifacts struct {
	Token     string            `json:"token"`
	Artifacts []json.RawMessage `json:"artifacts"`
}

type caskArtifactsJSON struct {
	Casks []caskWithArtifacts `json:"casks"`
}

func parseCaskAppPaths(jsonData string) (map[string][]string, error) {
	var doc caskArtifactsJSON
	if err := json.Unmarshal([]byte(jsonData), &doc); err != nil {
		return nil, fmt.Errorf("parse cask info: %w", err)
	}
	return lo.SliceToMap(doc.Casks, func(c caskWithArtifacts) (string, []string) {
		return c.Token, extractCaskAppPaths(c.Artifacts)
	}), nil
}

// extractCaskAppPaths walks the heterogeneous artifacts array brew returns
// and pulls out every /Applications/*.app path. Two sources matter:
//
//   - `app` artifacts (e.g. {"app": ["VLC.app"]}) — the install target;
//     missing for .pkg-based casks like Zoom.
//   - `uninstall.delete` paths (e.g. ["/Applications/zoom.us.app"]) — what
//     brew would delete on uninstall. For pkg-based casks this is the only
//     hint of where the app actually lives.
//
// app artifact values can be either a bare string ("VLC.app") or an object
// with rename rules ({"target": "MyName.app"}); both are handled.
func extractCaskAppPaths(artifacts []json.RawMessage) []string {
	var paths []string
	for _, raw := range artifacts {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		if v, ok := obj["app"]; ok {
			paths = append(paths, appArtifactPaths(v)...)
		}
		if v, ok := obj["uninstall"]; ok {
			paths = append(paths, uninstallDeletePaths(v)...)
		}
	}
	return paths
}

func appArtifactPaths(raw json.RawMessage) []string {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	var paths []string
	for _, item := range arr {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			if strings.HasSuffix(s, ".app") {
				paths = append(paths, filepath.Join("/Applications", s))
			}
			continue
		}
		// Object form with a `target` override: {"target": "Foo.app"}.
		var obj map[string]string
		if err := json.Unmarshal(item, &obj); err == nil {
			if target := obj["target"]; strings.HasSuffix(target, ".app") {
				paths = append(paths, filepath.Join("/Applications", target))
			}
		}
	}
	return paths
}

func uninstallDeletePaths(raw json.RawMessage) []string {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	var paths []string
	for _, m := range arr {
		dv, ok := m["delete"]
		if !ok {
			continue
		}
		var dpaths []string
		if err := json.Unmarshal(dv, &dpaths); err != nil {
			continue
		}
		paths = append(paths, lo.Filter(dpaths, func(p string, _ int) bool {
			return strings.HasSuffix(p, ".app")
		})...)
	}
	return paths
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
	for line := range strings.SplitSeq(output, "\n") {
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
	for line := range strings.SplitSeq(output, "\n") {
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

// parseResolvedVersion pulls the pinned version of pkgName out of `uv pip
// compile` output (lines look like "markitdown==0.1.5"). Package names are
// compared after PEP 503 normalization (lower-case, runs of -_. collapsed to a
// single -) so "Markitdown" / "markitdown_all" still match.
func parseResolvedVersion(output, pkgName string) string {
	want := normalizePkgName(pkgName)
	for line := range strings.SplitSeq(output, "\n") {
		name, version, ok := strings.Cut(strings.TrimSpace(line), "==")
		if !ok {
			continue
		}
		if normalizePkgName(name) == want {
			return strings.TrimSpace(version)
		}
	}
	return ""
}

var pkgNameSepRe = regexp.MustCompile(`[-_.]+`)

func normalizePkgName(name string) string {
	return pkgNameSepRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
}

// newerVersion reports whether candidate is a strictly higher version than
// installed. Falls back to a plain string inequality when either side isn't
// valid semver, so an odd version string never silently hides an upgrade.
func newerVersion(candidate, installed string) bool {
	cv, cerr := semver.NewVersion(candidate)
	iv, ierr := semver.NewVersion(installed)
	if cerr != nil || ierr != nil {
		return candidate != installed
	}
	return cv.GreaterThan(iv)
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
