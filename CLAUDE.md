# CLAUDE.md — sci CLI (Go)

## Workflow

- **Audience: Python/JS developers learning Go.** Explicit over clever, readable over terse.
- **`just ok` is the gate** — run after every change. Never call `go build` / `go test` / `gofmt` / `golangci-lint` directly; always go through `justfile` recipes. Need a recipe that doesn't exist? Add it.
- **TDD by default.** Write the failing test first, then make it pass. Skip only for trivial edits (typos, docs, one-line refactors).
- **All work on `main`.**
- **CI mirrors `just check-ci`** — add a gate step there and `.github/workflows/release.yml` picks it up. One intentional divergence: CI runs the full suite (no `-short`).
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

- `just lint-style` — semgrep (`.semgrep/go-modern.yml`) bans `for`+`append` in favor of `lo`; ast-grep bans inline `lipgloss.NewStyle()` outside `internal/uikit/` (`rules/no-inline-newstyle.yml`), hardcoded colors outside palette files, manual `m.width`/`m.height` literal arithmetic outside `internal/uikit/` (`rules/no-manual-dimension-math.yml` — derive from the style/measure instead), nonzero literal sizes in `list.New` (pass `0, 0` and size on `WindowSizeMsg`), and raw `func() tea.Msg` closures (wrap in `uikit.SafeCmd` / `AsyncCmd`). `ast-grep test` validates the rules that have fixtures in `rule-tests/`.
- `just lint-guard` — import boundaries, flag conventions, API rules (`scripts/lint-guard.sh`, numbered). Highlights: rule 9 bans the legacy `sort` package (use `slices.Sort`/`SortFunc`/`SortStableFunc`/`BinarySearch`); rule 13 requires every new `huh` prompt / TUI launch to be registered in `scripts/scriptable-inventory.yml` (hard gate, hand-maintained YAML); rule 14 bans raw `huh` `.Run()` (use `uikit.RunForm`); rule 15 bans `huh` imports outside `internal/uikit/`; rule 16 requires every `api.ItemPatch` that carries `Version` to declare a `Rebuild` hook (a Zotero PATCH replaces whole arrays, so answering a 412 by resubmitting a locally-derived payload is silent data loss — see `internal/zot/CLAUDE.md`).
- `just lint-docs` — revive `package-comments` + `exported`: every package and every exported symbol gets a godoc comment **starting with its name**, and no stuttering names (`brew.CLI`, not `brew.BrewRunner`). Tests, `cmd/`, and `cli/` wiring are exempt. **In the `just ok` gate** — prefer enriching a symbol's godoc over re-explaining it in a CLAUDE.md.

## Test & dev recipes

```
just bootstrap       # once after cloning: install the pinned dev tools (goimports, golangci-lint, …)
just install         # dev mode: symlink ./sci to ~/.local/bin/sci (rationale in the justfile comment)
just ok              # gate: fmt + vet + lint + test + build (-short: skips cloud/lab command tests)
just ok-slow         # gate + proj/new integration; before merging changes to sci proj new
just test            # full suite, incl. the cloud/lab command tests the gate skips
just test-cloud      # just the cloud/lab command tests (sci cloud / sci lab); before merging those
just test-pkg PKG    # single-package fast TDD loop: just test-pkg ./internal/zot
just test-race       # race pass; run before merging concurrency-sensitive changes
just test-slow       # proj/new integration (SLOW=1, 10m timeout; needs pixi/uv/quarto/marimo/typst/node)
just test-canvas     # cass integration (needs CANVAS_TOKEN in .env + gh auth login)
just test-zot-real   # opt-in real-Zotero-DB smoke over ./zotero.sqlite or $ZOT_REAL_DB
just docs-uikit      # regenerate internal/uikit/REFERENCE.md — run after touching uikit godoc
just zot-gen         # regenerate internal/zot/client/zotero.gen.go — never hand-edit it
just changelog       # preview the next release's notes (git-cliff over commits since the last CalVer tag)
just lint-commits    # validate unpushed commit subjects against the commit convention
```

- The `-short` gate: tests marked `testing.Short()` (the `sci cloud` / `sci lab` command tests in `cmd/sci`, which drive the online-gated `Before` hooks) skip locally so a flaky network can't stall the gate; they still run in `just test` / `just test-cloud` and CI. Mark a new test short (via the `skipCloudShort` helper) only when it genuinely needs the network — most "cloud" tests use `httptest` and stay in the gate.
- `SCI_ASSUME=yes|no` answers every `cmdutil.Confirm*` non-interactively — use it when driving confirmation-gated commands from scripts or agents.
- `just modernize-*` recipes stage Go `go fix` rewrites — read their justfile comments before running.

## Sub-CLAUDE pointers (read before editing these packages)

| When you touch… | Read first |
|---|---|
| `internal/tui/dbtui/` (SQLite/DuckDB browser) | `internal/tui/dbtui/CLAUDE.md` + `app/TESTING.md` |
| `internal/db/` (`sci db` verbs, dual-backend dispatch) | `internal/db/CLAUDE.md` |
| `internal/zot/` (Zotero CLI + hygiene) | `internal/zot/CLAUDE.md` |
| `internal/uikit/` (shared TUI + styling foundation) | `internal/uikit/doc.go` (catalog); generated `REFERENCE.md` for the full API |

`templates/` and `scripts/zot-graph/` are gitignored sibling projects staged in this tree — each carries its own CLAUDE.md when present locally.

**Where knowledge lives** (route by scope, so docs don't drift):
- Signatures, types, call-flow → **read the code.** Never restate structure in prose. `go doc ./...` is the tour (there is no `ARCHITECTURE.md`).
- Intent, invariants, contracts, external-system quirks **local to a symbol or package** → **godoc** on that symbol / `doc.go`. Co-located, `go doc`-readable, lint-checked.
- Repo-wide rules, prohibitions, conventions, commands → **this file** (godoc is bad at "never" and at cross-package rules).

## Cross-cutting design rules

- **Streams:** stdout carries the *answer*, stderr carries *diagnostics* (library banner, update notices, error envelopes under human mode). Human output goes through `cmdutil.HumanWriter()` — an `os.Stdout` wrapped in a `colorprofile.Writer` — so ANSI is stripped when the destination isn't a terminal and `NO_COLOR`/`CLICOLOR_FORCE` are honored. Never `fmt.Print` styled output straight to `os.Stdout`; piping (`sci zot content read KEY | llm`) is a first-class use.
- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`. Failures render through `cmdutil.HandleError` in `main()` — never hand-emit a `--json` envelope. Envelope shape, the closed `cmdutil.Code` vocabulary, `Fix` vs `Try`, exit-code partition, and `Warner`: `internal/cmdutil/output.go` package godoc.
- **CLI:** urfave/cli v3; all flags `Local: true` — *except* slice flags, which corrupt under `Local` (waiver + reason in `internal/zot/CLAUDE.md`).
- **Config storage:** per-domain files at `~/.config/sci/<name>.json` via `internal/sciconfig`. Declare `var configFile = sciconfig.File[Config]{Name: "<name>.json"}` and delegate `Path`/`Load`/`Save`/`Exists`/`Clear` to it — don't re-roll the XDG fallback, JSON marshal, or `0600` write. Domain logic (validation, schema migration via `LoadRaw`, defaulting) layers on top. `sci setup` (`cmd/sci/setup.go`) is a hub/menu router; register a tool by adding a `setupEntry`, don't reimplement setup.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. Canonical store at `internal/store/sqlite/` (raw `database/sql`; used by `sci db`, `sci view`, dbtui). `internal/zot/local/` keeps its own connection (read-only immutable mode on `zotero.sqlite`).
- **DuckDB:** shell out to the `duckdb` CLI via `internal/duck/` (required dep in `internal/doctor/Brewfile`). `sci view foo.duckdb` opens the native subprocess store at `internal/store/duck/`. Details: `internal/db/CLAUDE.md`.
- **TUI stack:** Bubble Tea v2 + Bubbles v2 + Lip Gloss v2 only — module paths are `charm.land/*` (`charm.land/bubbletea/v2`, …), **not** `github.com/charmbracelet/*`; both v1 and github-path imports fail lint-guard. No inline `lipgloss.NewStyle()` outside `internal/uikit/` (lint-enforced) — use `uikit.TUI` accessors / `uikit.TUI.Base()`. Reach for `uikit` first; extend it when a pattern recurs in ≥ 2 TUIs.
- **Layout sizing — derive, don't hardcode:** no manual `m.width`/`m.height` literal arithmetic in `View()`/render code outside `internal/uikit/` (lint-enforced). Subtract a *measured* size (`lipgloss.Width`/`Height`, `style.Get*FrameSize`) or use `uikit.Box` / `VStack` / `OverlayInnerWidth` / `OverlayBodyBudget`. Overlay bodies must size from the live frame + measured chrome so adding a line or changing the border can't silently drift them. A named reserve const is the escape hatch when a fixed inset is genuinely needed.
- **Forms/prompts:** `uikit` owns `huh` — nothing outside `internal/uikit/` imports it. Single prompts → `uikit.Input`/`InputInto`/`Select`/`MultiSelect`; multi-field forms → `uikit.NewForm(uikit.FormGroup(...))`; confirmations → `cmdutil.Confirm`/`ConfirmYes`/`ConfirmRequired`. Full wrapper catalog in `internal/uikit/doc.go`.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **New TUI apps** go under `internal/tui/<name>/` with an `app/` subpackage (model/update/view/keys/run) and a root entry calling `uikit.Run`/`RunModel`. Styles from `uikit` — no per-TUI `ui/` package.
- **`pkg/` is the shared surface; `internal/` is everything else.** `pkg/{openalex,citekey,doi,doiorg}` are imported by sibling SciMinds repos (today: `zotero-mcp`), so their exported API is a compatibility commitment — a breaking change there needs a `!` commit and a heads-up to the consumer. **Their dependency weight is part of that commitment**, because everything a `pkg/` package imports propagates into every consumer's module graph. Measure it with `go list -deps ./pkg/<name>` before adding an import; as of Phase 1 `doi` and `doiorg` are stdlib-only, `openalex` adds `samber/lo` + `x/text`, and `citekey` drags in `modernc.org/sqlite` — which is a real cost to impose on a consumer that only wanted to format a citekey, and worth splitting if one ever needs it alone. Everything else stays in `internal/`, including `zot/api` and `zot/local` — those are the Zotero *operate* plane and are deliberately not shared. `just ok` covers `./pkg/...` everywhere it covers `./internal/...` (vet, lint, lint-docs, semgrep, lint-guard rule 12).
- **Subcommands:** large trees (e.g. `zot`) live in `internal/<pkg>/cli.Commands()`, mounted via `cmd/sci/<pkg>.go`; small ones are declared directly in `cmd/sci/<pkg>.go`. No standalone user-facing binaries — everything ships as `sci <cmd>` (in-repo codegen tools like `internal/uikit/cmd/gen-reference` are fine). Namespace parents reject unknown children via `cmdutil.WireNamespaceDefaults(root)` (called once in `cmd/sci/root.go:buildRoot()`); don't wire per-command (and add a test if you ever disable it).

## Testing rules

- **teatest** for every Bubble Tea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- Verify DB mutations by querying the store directly, not by inspecting model state.
- No `time.Sleep` — use `teatest.WaitFor` (lint-guard-enforced).

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
- GitHub Classroom URL IDs are *org* IDs, not classroom IDs — `ResolveClassroomID` maps URL → API ID, cached in `cass.yaml` as `api_id`.
- `internal/brew/`: no `brew bundle` in hot paths (use direct `brew` + `brew.CollectSnapshot`); the Brewfile is a *lockfile* — resolve via `brew.LocateBrewfile()`, don't hardcode the XDG default.
