# CLAUDE.md — sci CLI (Go)

## Workflow

- **Audience: Python/JS developers learning Go.** Explicit over clever, readable over terse.
- **`just ok` is the gate** — run after every change. Never call `go build` / `go test` / `gofmt` / `golangci-lint` directly; always go through `justfile` recipes. Need a recipe that doesn't exist? Add it.
- **TDD by default.** Write the failing test first, then make it pass. Skip only for trivial edits (typos, docs, one-line refactors).
- **All work on `main`.**
- **CI commit-message triggers** — bracket markers (not UPPERCASE prose, so describing them doesn't fire them): `[release]` publishes the build to the `latest` GitHub release; `[scenarios]` runs the brew/doctor matrix (otherwise weekly cron); combine for both. Every push/PR runs the gate + cross-compile regardless.

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

- `just lint-style` — semgrep (`.semgrep/go-modern.yml`) rewrites `for`+`append` to `lo`; ast-grep bans inline `lipgloss.NewStyle()` (`rules/no-inline-newstyle.yml`) and hardcoded colors outside palette files.
- `just lint-guard` — import boundaries, flag conventions, API rules. Rule 9 bans the legacy `sort` package (use `slices.Sort`/`SortFunc`/`SortStableFunc`/`BinarySearch`); rules 14–15 ban `huh` imports outside `internal/uikit/`.
- `just lint-docs` — revive `package-comments` + `exported`: every package and every exported symbol gets a godoc comment **starting with its name**. Tests, `cmd/`, and `cli/` wiring are exempt; stutter-name checks are off (naming ≠ docs). **In the `just ok` gate** — prefer enriching a symbol's godoc over re-explaining it in a CLAUDE.md.

## Test recipes

```
just ok              # gate: fmt + vet + lint + test + build
just ok-slow         # gate + proj/new integration; before merging changes to sci proj new
just test-pkg PKG    # single-package fast TDD loop: just test-pkg ./internal/zot
just test-slow       # proj/new integration (~2 min, SLOW=1; needs pixi/uv/quarto/marimo/typst/node)
just test-canvas     # cass integration (needs CANVAS_TOKEN in .env + gh auth login)
just test-zot-real   # opt-in real-Zotero-DB smoke (reads ./zotero.sqlite)
```

## Sub-CLAUDE pointers (read before editing these packages)

| When you touch… | Read first |
|---|---|
| `internal/tui/dbtui/` (SQLite/DuckDB browser) | `internal/tui/dbtui/CLAUDE.md` + `app/TESTING.md` |
| `internal/db/` (`sci db` verbs, dual-backend dispatch) | `internal/db/CLAUDE.md` |
| `internal/zot/` (Zotero CLI + hygiene) | `internal/zot/CLAUDE.md` |
| `internal/uikit/` (shared TUI + styling foundation) | `internal/uikit/doc.go` |

**Where knowledge lives** (route by scope, so docs don't drift):
- Signatures, types, call-flow → **read the code.** Never restate structure in prose. `go doc ./...` is the tour (there is no `ARCHITECTURE.md`).
- Intent, invariants, contracts, external-system quirks **local to a symbol or package** → **godoc** on that symbol / `doc.go`. Co-located, `go doc`-readable, lint-checked.
- Repo-wide rules, prohibitions, conventions, commands → **this file** (godoc is bad at "never" and at cross-package rules).

## Cross-cutting design rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`.
- **CLI:** urfave/cli v3; all flags `Local: true` — *except* slice flags, which corrupt under `Local` (waiver + reason in `internal/zot/CLAUDE.md`).
- **Config storage:** per-domain files at `~/.config/sci/<name>.json` via `internal/sciconfig`. Declare `var configFile = sciconfig.File[Config]{Name: "<name>.json"}` and delegate `ConfigPath`/`Load`/`Save`/`Exists`/`Clear` to it — don't re-roll the XDG fallback, JSON marshal, or `0600` write. Domain logic (validation, schema migration via `LoadRaw`, defaulting) layers on top. `sci setup` (`cmd/sci/setup.go`) is a hub/menu router; register a tool by adding a `setupEntry`, don't reimplement setup.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. Canonical store at `internal/store/sqlite/` (raw `database/sql`; used by `sci db`, `sci view`, dbtui). `internal/zot/local/` keeps its own connection (read-only immutable mode on `zotero.sqlite`).
- **DuckDB:** shell out to the `duckdb` CLI via `internal/duck/` (required dep in `internal/doctor/Brewfile`). `sci view foo.duckdb` opens the native subprocess store at `internal/store/duck/`. Details: `internal/db/CLAUDE.md`.
- **TUI stack:** Bubble Tea v2 + Bubbles v2 + Lip Gloss v2 only — no v1 imports. No inline `lipgloss.NewStyle()` outside `internal/uikit/` (lint-enforced) — use `uikit.TUI` accessors / `uikit.TUI.Base()`. Reach for `uikit` first (catalog in `internal/uikit/doc.go`); extend it when a pattern recurs in ≥ 2 TUIs.
- **Forms/prompts:** `uikit` owns `huh` — no other package imports it. Single prompts → `uikit.Input`/`InputInto`/`Select`/`MultiSelect`; multi-field forms → `uikit.NewForm(uikit.FormGroup(...))`; confirmations → `cmdutil.Confirm`/`ConfirmYes`/`ConfirmRequired`. Full wrapper catalog in `internal/uikit/doc.go`.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **New TUI apps** go under `internal/tui/<name>/` with an `app/` subpackage (model/update/view/keys/run) and a root entry calling `uikit.Run`/`RunModel`. Styles from `uikit` — no per-TUI `ui/` package.
- **Subcommands:** large trees (e.g. `zot`) live in `internal/<pkg>/cli.Commands()`, mounted via `cmd/sci/<pkg>.go`; small ones are declared directly in `cmd/sci/<pkg>.go`. No standalone binaries — everything is `sci <cmd>`. Namespace parents reject unknown children via `cmdutil.WireNamespaceDefaults(root)` (called once in `cmd/sci/root.go:buildRoot()`); don't wire per-command (and add a test if you ever disable it).

## Testing rules

- **teatest** for every Bubble Tea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- Verify DB mutations by querying the store directly, not by inspecting model state.
- No `time.Sleep` — use `teatest.WaitFor`.
- Golden updates: `go test ./path -run TestName -update` (the only sanctioned raw `go test` — `-update` isn't wired through `just`).

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1`.
- marimo export exits non-zero for `mo.md()` cells — assert on the produced file, not the exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- GitHub Classroom URL IDs are *org* IDs, not classroom IDs — `ResolveClassroomID` maps URL → API ID, cached in `cass.yaml` as `api_id`.
- `internal/brew/`: no `brew bundle` in hot paths (use direct `brew` + `brew.CollectSnapshot`); the Brewfile is a *lockfile* — resolve via `brew.LocateBrewfile()`, don't hardcode the XDG default.
