# `sci tools ignore` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user exclude installed formulae, casks, and uv tools from every upgrade sci runs, without blocking `brew upgrade foo`.

**Architecture:** An ignore list in `~/.config/sci/tools.json` is loaded by each entry point and passed into the shared outdated check. The check splits packages into upgradable, ignored, and pinned. Upgrades name their packages; the bare `brew upgrade` method is deleted.

**Tech Stack:** Go, urfave/cli v3, samber/lo, `internal/sciconfig`.

**Spec:** `.superpowers/specs/2026-11-23-tools-ignore-design.md`

## Global Constraints

- Load the `go-modern` and `lo` skills before writing Go.
- Work on `main`. Only `just` recipes; `just ok` passes before each commit. No AI attribution.
- Types are exactly `formula`, `cask`, `uv`.
- An empty upgradable set makes no brew call; sci never runs a bare `brew upgrade`.
- Missing `tools.json` means nothing is ignored; a malformed one fails before any upgrade and names the file.
- No `time.Sleep` in tests; no test reads the developer's real config. Config tests set `XDG_CONFIG_HOME` to a temp dir and call `xdg.Reload()` (see `withXDGConfigHome` in `internal/sciconfig`), so they are not parallel.
- Every exported symbol has a godoc comment starting with its name. No flag uses `Destination`; bool flags on the new commands are `Local: true`.
- No new prompts: ambiguity is a usage error.

## Review Focus

- A `tools.json` with an unknown type (`"brew"`): error naming the file, not an entry that silently never matches. → Task 1
- A uv tool typed as `My_Tool` or `marimo[recommended]`: matches `my-tool` / `marimo`. → Task 1
- A package both ignored and pinned: listed once, as ignored. → Task 3
- The formula upgrade fails: the cask and uv upgrades still run, and the error is reported. → Task 2
- The list becomes malformed and `doctor --json --yes` runs: the upgrade check reports `failed` and nothing is upgraded. → Task 3

---

### Task 1: Ignore list

**Files:**
- Create: `internal/brew/ignore.go`, `internal/brew/ignore_test.go`
- Modify: `internal/brew/results.go`

**Interfaces:**
- Consumes: `CollectSnapshot`, `SystemSnapshot.IsInstalled` (its formula type is `"brew"`), `shortFormula`, `normalizePkgName`, `sciconfig.File`.
- Produces:
  - `const TypeFormula = "formula"`, `TypeCask = "cask"`, `TypeUV = "uv"`
  - `type IgnoredPackage struct { Type string; Name string }` (JSON `type`, `name`)
  - `type IgnoreList []IgnoredPackage` and `func (l IgnoreList) Contains(pkgType, name string) bool`
  - `func LoadIgnored() (IgnoreList, error)`
  - `func Ignore(r Runner, name, pkgType string, dryRun bool) (IgnoreResult, error)`
  - `func Unignore(name, pkgType string, dryRun bool) (IgnoreResult, error)`
  - `var ErrAmbiguousType`, `var ErrNotInstalled`
  - `type IgnoreResult struct { Action string; Package IgnoredPackage; Changed, DryRun bool }` (JSON `action`, `package`, `changed`, `dry_run`; action is `ignore` or `unignore`)
  - `type IgnoreListResult struct { Ignored IgnoreList }` (JSON `ignored`, `[]` when empty)

- [ ] **Step 1: Write the failing tests**

| Test | Setup | Assertions |
|---|---|---|
| `TestLoadIgnored_Missing` | no file | empty list, nil error |
| `TestLoadIgnored_Malformed` | `{"ignored":` | error contains the full `tools.json` path |
| `TestLoadIgnored_UnknownType` | `{"ignored":[{"type":"brew","name":"jq"}]}` | error contains the path and `"brew"` |
| `TestIgnoreList_Contains` | `{formula, oven-sh/bun/bun}`, `{cask, docker}`, `{uv, my-tool}` | true: `formula bun`, `formula oven-sh/bun/bun`, `cask docker`, `uv My_Tool`, `uv my-tool[x]`; false: `formula docker`, `cask bun` |
| `TestIgnore_SavesInstalled` | cask `libreoffice` installed | `Changed`; reload equals `[{cask libreoffice}]` |
| `TestIgnore_NotInstalled` | nothing installed | `errors.Is(err, ErrNotInstalled)`; no file written |
| `TestIgnore_Ambiguous` | formula and cask `docker` | `pkgType ""` → `errors.Is(err, ErrAmbiguousType)`; `cask` saves only the cask |
| `TestIgnore_Twice` | ignore twice | second: nil error, `Changed == false`, one entry |
| `TestIgnore_DryRun` | installed | `Changed` true, no file written |
| `TestUnignore_NotInstalled` | entry on disk, no runner | entry removed |
| `TestUnignore_NotIgnored` | empty list | nil error, `Changed == false` |
| `TestUnignore_Ambiguous` | formula and cask `docker` ignored | `pkgType ""` → `ErrAmbiguousType`; both entries remain |

- [ ] **Step 2:** Run `just test-pkg ./internal/brew`. Expected: build failure, `LoadIgnored` undefined.
- [ ] **Step 3:** Implement the interfaces above. Names are normalized on save and on comparison: formulae and casks with `shortFormula`; uv by cutting at `[` and then `normalizePkgName`. Error text for ambiguity: `"docker" matches formula and cask: pass --formula, --cask, or --uv`. Messages for unchanged results: `foo is already ignored`, `foo is not ignored`.
- [ ] **Step 4:** Run `just test-pkg ./internal/brew`, then `just ok`. Expected: pass.
- [ ] **Step 5:** `git commit -m "feat(brew): persist a list of ignored packages"`

### Task 2: Typed packages, pins, and named upgrades

**Files:**
- Modify: `internal/brew/brew.go`, `internal/brew/commands.go`, `internal/brew/outdated_check.go`, `internal/doctor/setup_flow.go`
- Test: `internal/brew/{mock,parse,update,outdated_check}_test.go`, `internal/doctor/{doctor,setup_flow}_test.go`

**Interfaces:**
- Consumes: the type constants from Task 1.
- Produces:
  - `OutdatedPackage.Type string` (JSON `type`), set by `parseOutdated` and `parseUVOutdated`
  - `Runner.UpgradeFormulae(names []string) (string, error)` and `Runner.UpgradeCasks(names []string) (string, error)`, replacing `Runner.Upgrade`
  - `func UpgradeBrew(r Runner, pkgs []OutdatedPackage) (string, error)`, used by `runUpgrades` and `RunSetup`
  - `OutdatedCheck.Pinned []OutdatedPackage`; `Brew` and `UV` hold only upgradable packages, and so does `All()`
  - `checkOutdated(r Runner) (OutdatedCheck, error)`

- [ ] **Step 1: Write the failing tests**
  - `TestParseOutdated_Types`: formula and cask JSON, the cask with `"pinned": true` → types `formula` and `cask`, cask `Pinned` true.
  - `TestParseOutdated_CaskWithoutPinnedField`: older brew JSON → `Pinned` false.
  - `TestUpgradeArgs`: `upgradeArgs(TypeFormula, []string{"jq","fd"})` is `["upgrade","--formula","jq","fd"]`; cask form uses `--cask`.
  - `TestUpdate_UpgradesByName`: formula and cask both named `docker` → one `UpgradeFormulae(["docker"])` and one `UpgradeCasks(["docker"])`.
  - `TestUpdate_SkipsPinned`: pinned `jq`, unpinned `fd` → formula calls are `[["fd"]]`, `Outdated` is `fd` only.
  - `TestUpdate_AllPinned`: no upgrade calls of any kind.
  - `TestUpgradeBrew_FormulaFailureStillUpgradesCasks`: both calls made, error contains `brew upgrade`. Extend the existing brew-failure test in `setup_flow_test.go` to check that uv still upgrades.
  - `TestRunSetup_SkipsPinned`: pinned formula → no upgrade call; absent from `Outdated` and `Upgraded`.
- [ ] **Step 2:** Run `just test-pkg ./internal/brew` and `just test-pkg ./internal/doctor`. Expected: build failures on the new names.
- [ ] **Step 3:** Implement. Add `upgradeArgs(pkgType string, names []string) []string`. Both `CLI` upgrade methods keep `noninteractiveEnv()` with `interactive=false`, and return `"", nil` for empty names. Delete `Upgrade` from the interface, `CLI`, and both mocks; mocks record `[][]string`. Add types to existing fixtures.
- [ ] **Step 4:** Run `just ok`. Expected: pass. `rg -n '\.Upgrade\(\)' internal cmd` prints nothing.
- [ ] **Step 5:** `git commit -m "fix(brew): stop reporting pinned packages as outdated and upgrade by name"`

### Task 3: Apply the list to every upgrade path

**Files:**
- Modify: `internal/brew/{commands,outdated_check,results}.go`, `internal/doctor/{setup_flow,result}.go`, `cmd/sci/{tools,doctor}_darwin.go`
- Test: `internal/brew/{update,outdated_check}_test.go`, create `internal/brew/results_test.go`; `internal/doctor/{doctor,setup_flow,upgrade_check}_test.go`

**Interfaces:**
- Consumes: `LoadIgnored`, `IgnoreList.Contains`, `OutdatedCheck`.
- Produces:
  - `Update(r Runner, ignored IgnoreList, checkOnly bool) (UpdateResult, error)`
  - `UpgradeOnly(r Runner, ignored IgnoreList) (UpdateResult, error)`
  - `CheckUpdates(ctx context.Context, r Runner, ignored IgnoreList) (OutdatedCheck, error)`
  - `OutdatedCheck.Ignored []OutdatedPackage`
  - `UpdateResult.Ignored`, `.Pinned []OutdatedPackage` (JSON `ignored`, `pinned`; `[]` when empty)
  - `func HeldBack(ignored, pinned []OutdatedPackage) string`: human lines, `""` when both are empty
  - `SetupResult` and the doctor result gain `Ignored` and `Pinned` (JSON `omitempty`, like `outdated`)

The list is a required parameter so no caller can omit it. `RunSetup` calls `brew.LoadIgnored()` itself, because a forgotten `SetupOpts` field would mean "ignore nothing".

- [ ] **Step 1: Write the failing tests**
  - `TestUpdate_SkipsIgnored`: cask `docker` ignored; formula and cask `docker` outdated → only `UpgradeFormulae(["docker"])`; `Ignored` holds the cask.
  - `TestUpdate_SkipsIgnoredUV`: `ruff` ignored → no uv upgrade call.
  - `TestUpdate_AllIgnored`: no upgrade calls; `Outdated` empty, `Ignored` populated.
  - `TestUpdate_IgnoredTapFormula`: ignored `bun`, reported `oven-sh/bun/bun` → not upgraded.
  - `TestUpgradeOnly_SkipsIgnored`, `TestCheckUpdates_SplitsIgnored`: same exclusion on those entry points.
  - `TestUpdate_IgnoredAndPinned`: in `Ignored`, absent from `Pinned`.
  - `TestUpdateResult_ShowsHeldBack`: nothing outdated, ignored `libreoffice 26.8.0 → 26.8.3`, pinned `jq` → human output contains `1 ignored`, `libreoffice`, `26.8.3`, `1 pinned`, `jq`, and not `Everything is up to date`.
  - `TestUpdateResult_JSONEmptyLists`: marshals `"ignored":[]` and `"pinned":[]`.
  - `TestRunSetup_SkipsIgnored`: saved list → no upgrade; present in `Ignored`, absent from `Outdated` and `Upgraded`.
  - `TestRunSetup_MalformedIgnoreList`: `UpgradeCheck.Status == UpgradeCheckFailed`, `UpdateError` names `tools.json`, zero upgrade calls.
- [ ] **Step 2:** Run `just test-pkg ./internal/brew` and `just test-pkg ./internal/doctor`. Expected: build failures on the new signatures.
- [ ] **Step 3:** Implement. Classify ignored before pinned. Doctor's `TestMain` points `XDG_CONFIG_HOME` at a temp dir. In `cmd/sci`, `runToolsUpdate`, `runToolsOutdated`, and `runDoctorUpdateCheck` load the list before touching brew. The two tools commands return the load error; `runDoctorUpdateCheck` warns and finishes as it does for other check failures. Each prompt path prints `HeldBack` to stderr below its outdated list, or alone when nothing is outdated. Doctor's JSON mapping copies both new fields.
- [ ] **Step 4:** Run `just ok`. Expected: pass.
- [ ] **Step 5:** `git commit -m "feat(brew): leave ignored packages out of every upgrade"`

### Task 4: Commands and documentation

**Files:**
- Modify: `cmd/sci/tools_darwin.go`, `README.md` (the `sci tools` table)
- Test: `cmd/sci/tools_darwin_test.go`

**Interfaces:**
- Consumes: `LoadIgnored`, `Ignore`, `Unignore`, `IgnoreListResult`, `ErrAmbiguousType`, `resolveToolsPkgType`.
- Produces: `toolsIgnoreCommand()`, `toolsUnignoreCommand()`.

- [ ] **Step 1: Write the failing tests**
  - `TestToolsIgnoreFlags`: both commands exist under `tools` with `formula`, `cask`, `uv`, and without `tap`, `go`, `cargo`.
  - `TestToolsIgnore_ListsWithoutNetwork`: temp config with `{cask libreoffice}`; `sci tools ignore --json` output has that entry.
  - `TestToolsUnignore_RequiresName`: no argument → usage error.
  - `TestToolsUnignore_DryRun`: file unchanged after `sci tools --dry-run unignore libreoffice`.
  - `TestToolsIgnore_HelpStatesDependencyLimit`: `ignore` description contains `dependency`.
- [ ] **Step 2:** Run `just test-pkg ./cmd/sci`. Expected: FAIL, commands not found.
- [ ] **Step 3:** Implement. No `netutil.Online()` check, no Brewfile lookup, and no `syncBrewfile`. More than one argument is a usage error; `ErrAmbiguousType` is returned through `cmdutil.UsageErrorf`. Usage lines: `Exclude a package from sci upgrades, or list ignored packages` and `Include an ignored package in sci upgrades again`. The `ignore` description ends: `Homebrew may still upgrade an ignored formula as a dependency or dependent of another package.` Add both commands to the parent description and the README table.
- [ ] **Step 4:** Run `just ok`, then `just doc-coverage` and report any new gap (recording a cast is out of scope).
- [ ] **Step 5: Check the real binary** with `XDG_CONFIG_HOME` set to a temp dir: `./sci tools ignore libreoffice`, `./sci tools ignore`, `./sci tools outdated --json`, `./sci tools unignore libreoffice`. Expected: the cask is in `ignored` and not in `outdated`, assuming it is still outdated. Do not run `tools update`: it would upgrade real packages.
- [ ] **Step 6:** `git commit -m "feat(brew): sci tools ignore and unignore"`
