# CLAUDE.md — sci CLI (Go)

## Workflow

- **Audience: Python/JS developers learning Go.** Explicit over clever, readable over terse.
- **`just ok` is the gate** — run after every change. Never call `go build` / `go test` / `gofmt` / `golangci-lint` directly; always go through `justfile` recipes. Need a recipe that doesn't exist? Add it.
- **TDD by default.** Write the failing test first, then make it pass. Skip only for trivial edits (typos, docs, one-line refactors).
- **All work on `main`.**
- **CI mirrors `just check-ci`** — add a gate step there and `.github/workflows/release.yml` picks it up. Four deliberate divergences from the local `just ok`: `check-ci` is verify-only (it fails on `gofmt`/`goimports`/`go.mod` drift rather than rewriting), it rejects conflict markers anywhere in the tracked tree and whitespace errors in the branch diff, it skips `lint-style` (only `semgrep` is absent from the runner — `ast-grep` is installed there, because lint-guard rule 13 shells out to it) and the local `build` (the workflow cross-compiles the four release binaries instead), and it runs the full suite under `-race` where the local gate runs `-short` without it.
- **Commit convention — Conventional Commits, enforced.** Subjects are `<type>(<scope>)!: subject` — types `feat fix docs refactor perf test chore ci build style`; scope optional, lowercase package/area (`zot`, `db`, `uikit`, …); `!` before the colon marks a breaking change. Enforced by the commit-msg hook (`just bootstrap` installs it) and a CI range check (`scripts/lint-commits.sh` is the shared logic). Release notes are generated from these subjects by git-cliff (`cliff.toml`) and posted as the GitHub Release body: `feat`/`fix`/`perf`/`refactor`/`docs` are published (breaking changes get their own section), `chore`/`ci`/`test`/`style`/`build` are not — pick the type accordingly for user-visible work. Preview the next release's notes with `just changelog` (needs `brew install git-cliff`).
- **CI commit-message triggers** — bracket markers (not UPPERCASE prose, so describing them doesn't fire them): `[release]` publishes an immutable CalVer-tagged release (`vYYYY.MM.DD[.N]`); `[scenarios]` runs the brew/doctor matrix (also auto-runs on pushes touching cmdutil/brew/doctor/netutil/tools code; otherwise weekly cron); combine for both. Every push/PR runs the gate + cross-compile regardless.

## Skills — invoke BEFORE the work, not after

Blocking: load the skill before you start, so you write it right the first time instead of leaning on linters to catch legacy patterns afterward. Each skill carries the full catalog, conventions, and migration maps — this file states only the rules they don't own.

| Before you… | Invoke |
|---|---|
| write or edit **any** Go (`.go`) | `go-modern` — stdlib + language idioms (1.21–1.26); replaces the legacy forms the linters ban |
| write any slice / map / set transform | `lo` — Map/Filter/Reduce/GroupBy/KeyBy, set ops, `*Err` variants |
| build or modify a TUI screen, layout, or mouse/keyboard handling | `bubbletea` — Elm loop, layouts, teatest; its `references/golden-rules.md` prevents the common border/overflow bugs |
| style, measure, or lay out terminal output (or debug overflow) | `lipgloss` — sizing discipline, borders, tables/trees |

`go-modern` owns stdlib/language; `lo` owns functional transforms — they hand off cleanly. Forms go through `uikit`, never the `huh` skill directly (see below).

## Lint enforcement (don't create new debt)

- `just lint-style` — semgrep (`.semgrep/go-modern.yml`) bans `for`+`append` in favor of `lo`; ast-grep bans inline `lipgloss.NewStyle()` outside `internal/uikit/` (`rules/no-inline-newstyle.yml`), hardcoded colors outside palette files, manual `m.width`/`m.height` literal arithmetic outside `internal/uikit/` (`rules/no-manual-dimension-math.yml` — derive from the style/measure instead), nonzero literal sizes in `list.New` (pass `0, 0` and size on `WindowSizeMsg`), and raw `func() tea.Msg` closures (wrap in `uikit.SafeCmd`, returning your own named message struct — a generic result wrapper cannot carry the correlation data async TUI commands need, which is why sci has none). `ast-grep test` validates the rules that have fixtures in `rule-tests/`.
- `just lint-guard` — import boundaries, flag conventions, API rules (`scripts/lint-guard.sh`, numbered). Highlights: rule 7 requires a `uikit.DrainStdin()` after every `tea.Program.Run()` — without it the terminal's stale DECRQM replies land in whatever reads stdin next — and only `internal/uikit/` is exempt, because that is where the drain lives; rule 9 bans the legacy `sort` package (use `slices.Sort`/`SortFunc`/`SortStableFunc`/`BinarySearch`); rule 12 requires every package under `internal/` to carry a package doc comment (a `doc.go` counts; the waiver is a `.lint-no-pkg-doc` file in the directory); rule 13 requires every new `huh` prompt / TUI launch to be registered in `scripts/scriptable-inventory.yml` (hard gate, hand-maintained YAML); rule 14 bans calling `.Run()` on a raw `huh` form — every form goes through `uikit`, whose runner sets the theme and keymap and drains stdin afterwards (single prompts via `uikit.Input`/`Select`/…, multi-field forms via `uikit.NewForm(...).Run()`); rule 15 bans `huh` imports outside `internal/uikit/`; rule 16 is retired and its number is never reused, so commit history keeps meaning what it said; rule 17 is the inverse of rule 4 — a `*SliceFlag` must **not** carry `Local: true`, because urfave/cli truncates a `Local` slice flag to its last occurrence (mark it `// lint:no-local` instead); rule 18 is the zot read/write boundary — **only `internal/zot/{api,doiorg,cli}` may reach `net/http`**; every other package under `internal/zot/` and all of `internal/local/` must not, so reads stay local and credential-free while writes go through the one client (see `internal/zot/CLAUDE.md`, "The boundary"); rule 19 bans `Destination` on a flag outright (the check does not inspect what the pointer aims at) — urfave stamps every declared flag's default through that pointer on every `Run`, which makes flag state shared across command trees (a `-race` failure in CI on a green `just ok`) and, within one package, silently aliases two commands that name the same variable. Read the value in the `Action` via `cmd.String("name")`/`cmd.Bool`/`cmd.Int` on the flag's **primary** name and pass it to helpers as a parameter; the sole waiver is `// lint:destination-ok` for a value read after `Run()` returns, when no `*cli.Command` is left in scope.
- `just lint-docs` — revive `package-comments` + `exported`: every package and every exported symbol gets a godoc comment **starting with its name**, and no stuttering names (`brew.CLI`, not `brew.BrewRunner`). Tests, `cmd/`, and `cli/` wiring are exempt. **In the `just ok` gate** — prefer enriching a symbol's godoc over re-explaining it in a CLAUDE.md.

## Test & dev recipes

```
just bootstrap       # once after cloning: install the pinned dev tools (goimports, golangci-lint, …)
just install         # dev mode: symlink ./sci to ~/.local/bin/sci (rationale in the justfile comment)
just build-linux     # cross-check the build-tagged _linux.go files the host never compiles
just ok              # gate: fmt + vet + lint + test + build (-short: skips cloud/lab command tests)
just ok-slow         # gate + proj/new integration; before merging changes to sci proj new
just test            # full suite, incl. the cloud/lab command tests the gate skips
just test-cloud      # just the cloud/lab command tests (sci cloud / sci lab); before merging those
just test-pkg PKG    # single-package fast TDD loop: just test-pkg ./internal/zot
just test-race       # race pass; run before merging concurrency-sensitive changes
just test-slow       # proj/new integration (SLOW=1, 10m timeout; needs pixi/uv/quarto/marimo/typst/node)
just test-zot-real   # opt-in real-Zotero-DB smoke over ./zotero.sqlite or $ZOT_REAL_DB
just docs-uikit      # regenerate internal/uikit/REFERENCE.md — run after touching uikit godoc
just doc-coverage    # report gaps in user-facing CLI docs (casts, gifs, README embeds, help descriptions)
just casts-gif       # render internal/{help,learn}/casts/*.cast to docs/casts/*.gif (needs `agg`); takes a stem or glob
just changelog       # preview the next release's notes (git-cliff over commits since the last CalVer tag)
just lint-commits    # validate unpushed commit subjects against the commit convention
```

- The `-short` gate: tests marked `testing.Short()` (the `sci cloud` / `sci lab` command tests in `cmd/sci`, which drive the online-gated `Before` hooks) skip locally so a flaky network can't stall the gate; they still run in `just test` / `just test-cloud` and CI. Mark a new test short (via the `skipCloudShort` helper) only when it genuinely needs the network — most "cloud" tests use `httptest` and stay in the gate.
- `SCI_ASSUME=yes|no` answers every `cmdutil.Confirm*` non-interactively, and is consulted before anything else — use it when driving confirmation-gated commands from scripts or agents. `--json` alone is **not** a substitute: quiet mode is not consent (see the Forms/prompts rule below).
- `just modernize-*` recipes stage Go `go fix` rewrites — read their justfile comments before running.

## Sub-CLAUDE pointers (read before editing these packages)

| When you touch… | Read first |
|---|---|
| `internal/tui/dbtui/` (SQLite/DuckDB browser) | `internal/tui/dbtui/CLAUDE.md` + `app/TESTING.md` |
| `internal/db/` (`sci db` verbs, dual-backend dispatch) | `internal/db/CLAUDE.md` |
| `internal/zot/` (Zotero local read surface + hygiene) | `internal/zot/CLAUDE.md` |
| `internal/uikit/` (shared TUI + styling foundation) | `internal/uikit/doc.go` (catalog); generated `REFERENCE.md` for the full API |
| any Bubble Tea model, if the Elm loop is new to you | `internal/tui/BUBBLE_TEA_PRIMER.md` (MVU, messages/commands, teatest) |

`templates/` and `scripts/zot-graph/` are gitignored sibling projects staged in this tree — each carries its own CLAUDE.md when present locally.

**Where knowledge lives** (route by scope, so docs don't drift):
- Signatures, types, call-flow → **read the code.** Never restate structure in prose. `go doc ./...` is the tour (there is no `ARCHITECTURE.md`).
- Intent, invariants, contracts, external-system quirks **local to a symbol or package** → **godoc** on that symbol / `doc.go`. Co-located, `go doc`-readable, lint-checked.
- Repo-wide rules, prohibitions, conventions, commands → **this file** (godoc is bad at "never" and at cross-package rules).

## Cross-cutting design rules

- **Streams:** stdout carries the *answer*, stderr carries *diagnostics* (library banner, update notices, error envelopes under human mode). Human output goes through `cmdutil.HumanWriter()` — an `os.Stdout` wrapped in a `colorprofile.Writer` — so ANSI is stripped when the destination isn't a terminal and `NO_COLOR`/`CLICOLOR_FORCE` are honored. Never `fmt.Print` styled output straight to `os.Stdout`; piping (`sci zot bib paper.qmd --json | jq`) is a first-class use. urfave's own `--help` pages obey the same rule: `cmd/sci/root.go` sets `root.Writer` to a `humanHelpWriter` that resolves `cmdutil.HumanWriter()` **per write** (a cached writer keeps painting the real terminal after a test swaps `os.Stdout`), so `sci zot --help | rg search` is plain text.
- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`. Failures render through `cmdutil.HandleError` in `main()` — never hand-emit a `--json` envelope. Envelope shape, the closed `cmdutil.Code` vocabulary, `Fix` vs `Try`, exit-code partition, and `Warner`: `internal/cmdutil/output.go` package godoc.
- **CLI:** urfave/cli v3; all flags `Local: true` — *except* slice flags, which corrupt under `Local` and are gate-rejected by lint-guard rule 17 (waiver + reason in `internal/zot/CLAUDE.md`), and parent flags that deliberately cascade to subcommands, which carry a `// lint:no-local` waiver. A slice flag whose values are human names or free text also needs `DisableSliceFlagSeparator` on its command, or urfave splits them on comma. **A flag never carries `Destination`** (rule 19): read its value in the `Action` via `cmd.String`/`Bool`/`Int` on the flag's **primary** name, and pass it to helpers as a parameter.
- **Config storage:** per-domain files at `~/.config/sci/<name>.json` via `internal/sciconfig`. Declare `var configFile = sciconfig.File[Config]{Name: "<name>.json"}` and delegate `Path`/`Load`/`Save`/`Exists`/`Clear` to it — don't re-roll the XDG fallback, JSON marshal, or `0600` write. Domain logic (validation, schema migration via `LoadRaw`, defaulting) layers on top. `sci setup` (`cmd/sci/setup.go`) is a hub/menu router; register a tool by adding a `setupEntry`, don't reimplement setup.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. Canonical store at `internal/store/sqlite/` (raw `database/sql`; used by `sci db`, `sci view`, dbtui). `internal/local/` keeps its own connection (read-only immutable mode on `zotero.sqlite`).
- **DuckDB:** shell out to the `duckdb` CLI via `internal/duck/` (required dep in `internal/doctor/Brewfile`). `sci view foo.duckdb` opens the native subprocess store at `internal/store/duck/`. Details: `internal/db/CLAUDE.md`.
- **TUI stack:** Bubble Tea v2 + Bubbles v2 + Lip Gloss v2 only — import them as `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`, `charm.land/huh/v2`. Lint-guard rule 1 rejects exactly two forms for those four packages: the `charm.land/…` path without the `/v2` suffix, and the old `github.com/charmbracelet/{bubbletea,bubbles,lipgloss,huh}` path. **Every other `github.com/charmbracelet/*` import is legal and in use** — `x/exp/teatest/v2`, `colorprofile`, `glamour` and `x/ansi` account for 35 import lines in the tree today; they have no `charm.land` equivalent and must not be "migrated". No inline `lipgloss.NewStyle()` outside `internal/uikit/` (lint-enforced) — use `uikit.TUI` accessors / `uikit.TUI.Base()`. Reach for `uikit` first; extend it when a pattern recurs in ≥ 2 TUIs.
- **Layout sizing — derive, don't hardcode:** no manual `m.width`/`m.height` literal arithmetic in `View()`/render code outside `internal/uikit/` (lint-enforced). Subtract a *measured* size (`lipgloss.Width`/`Height`, `style.Get*FrameSize`) or use `uikit.Box` / `VStack` / `OverlayInnerWidth` / `OverlayBodyBudget`. Overlay bodies must size from the live frame + measured chrome so adding a line or changing the border can't silently drift them. A named reserve const is the escape hatch when a fixed inset is genuinely needed.
- **Forms/prompts:** `uikit` owns `huh` — nothing outside `internal/uikit/` imports it. Single prompts → `uikit.Input`/`InputInto`/`Select`/`MultiSelect`; multi-field forms → `uikit.NewForm(uikit.FormGroup(...))`; confirmations → `cmdutil.Confirm`/`ConfirmYes`/`ConfirmRequired`. Full wrapper catalog in `internal/uikit/doc.go`. **Quiet mode is not consent:** `--json` sets quiet, and a prompt that cannot be shown is not a prompt that was answered yes — every confirmation returns `cmdutil.ErrNeedsConsent` (a `CodeUsage` failure, exit 2) unless `SCI_ASSUME=yes` or the command's own consent flag was passed. So **any command that can reach a prompt under `--json` must have a `--yes`/`--force` flag**, threaded into `cmdutil.ConfirmOrSkip`'s `skip` argument; without one the prompt is an unanswerable dead end. Match the sentinel with `errors.Is` — never reuse `ErrCancelled` for it, because call sites map a decline to a clean exit 0 and would turn a refusal into a silent no-op.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **New TUI apps** go under `internal/tui/<name>/` with an `app/` subpackage (model/update/view/keys/run) and a root entry calling `uikit.Run`/`RunModel`. Styles from `uikit` — no per-TUI `ui/` package.
- **Everything is `internal/`. There is no `pkg/`, and adding one needs a reason that names its consumer.** A package under `pkg/` is importable from outside the module, which makes its exported API a compatibility commitment and its dependency list part of every consumer's module graph. sci has no external consumers, so publishing anything would buy a commitment nothing exercises and pay for it in `!` commits on ordinary refactors. `internal/` also lets the compiler enforce the boundary instead of a convention. If a package ever does need publishing, the bar is a named in-repo importer *and* a named outside one — not a courtesy to a sibling project, which is better served by copying what it needs or pinning an older module version through the Go proxy.
- **Subcommands:** large trees (e.g. `zot`) live in `internal/<pkg>/cli.Commands()`, mounted via `cmd/sci/<pkg>.go`; small ones are declared directly in `cmd/sci/<pkg>.go`. No standalone user-facing binaries — everything ships as `sci <cmd>` (in-repo codegen tools like `internal/uikit/cmd/gen-reference` are fine). Namespace parents reject unknown children via `cmdutil.WireNamespaceDefaults(root)` (called once in `cmd/sci/root.go:buildRoot()`); don't wire per-command (and add a test if you ever disable it).

## Testing rules

- **teatest** for every Bubble Tea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- Verify DB mutations by querying the store directly, not by inspecting model state.
- No `time.Sleep` in any test (lint-guard rule 2, which exempts only `httptest` handlers simulating a slow server). Wait on output with `tuitest.WaitFor` (`internal/tuitest`), which strips ANSI before matching.

## Debugging a live TUI

When a TUI misbehaves and you need to *see the message stream* (which `tea.Msg` drives an overlay/mode transition, why a key seems ignored), run it with `SCI_TUI_DEBUG` pointed at a file and `tail -f` that file in another pane:

```
SCI_TUI_DEBUG=/tmp/sci-tui.log sci view data.db   # works for every sci TUI
tail -f /tmp/sci-tui.log                           # other pane: every tea.Msg, pretty-printed
```

Every message reaching the program is dumped via go-spew, truncated per run. It's tapped in `uikit.panicGuard`, so every `uikit.Run`/`RunModel` program gets it. **Dev/debugging only** — off by default, suppressed under `--json`; never wire it into shipping code paths. Mechanism: `internal/uikit/run_debug.go` ([TUIDebugEnv] in godoc). Fastest debugger for dbtui's overlay stack.

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1`.
- marimo export exits non-zero for `mo.md()` cells — assert on the produced file, not the exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- `internal/brew/`: no `brew bundle` in hot paths (use direct `brew` + `brew.CollectSnapshot`); the Brewfile is a *lockfile* — resolve via `brew.LocateBrewfile()`, don't hardcode the XDG default.
