# CLAUDE.md — sci CLI (Go)

## Workflow

- **Audience: Python/JS developers learning Go.** Prefer `lo` over hand-rolled loops, explicit over clever, readable over terse.
- **`just ok` is the gate.** Run after every change. Never invoke `go build` / `go test` / `gofmt` directly — always go through `justfile` recipes (`just test`, `just run …`, `just lint`, etc.). If you need a recipe that doesn't exist, add it.
- **TDD by default** for new features and bug fixes: write the failing test first, then make it pass. Skip TDD only for trivial edits (typos, doc tweaks, one-line refactors).
- **Bubbletea work → invoke the `bubbletea` skill** before designing layouts, fixing rendering bugs, or adding mouse/keyboard handling. Its `references/golden-rules.md` prevents the most common border/overflow bugs. Required for any new TUI screen.
- **All work on `main`.**
- **CI commit-message triggers:** push/PR always runs the gate + cross-compile. Add `[release]` to publish the build to the `latest` GitHub release. Add `[scenarios]` to run the brew/doctor scenarios matrix (otherwise weekly cron only). Combine both for a release that also clears the scenarios. Bracket markers (not UPPERCASE prose) so describing them in commit bodies doesn't fire them.

## Test recipes

```
just ok              # gate: fmt + vet + lint + test + build
just ok-slow         # gate + test-slow (proj/new integration); use before merging changes that touch sci proj new
just test-pkg PKG    # single-package tests (fast TDD loop): just test-pkg ./internal/zot
just test-slow       # proj/new integration (~2 min, SLOW=1, needs pixi/uv/quarto/marimo/typst/node)
just test-canvas     # cass integration (needs CANVAS_TOKEN in .env + gh auth login)
just test-zot-real   # opt-in real-Zotero-DB smoke (reads ./zotero.sqlite)
```

## Sub-CLAUDE pointers (read before editing these packages)

| When you touch… | Read first |
|---|---|
| `internal/tui/dbtui/` (SQLite browser) | `internal/tui/dbtui/CLAUDE.md` + `app/TESTING.md` |
| `internal/zot/` (Zotero CLI + hygiene) | `internal/zot/CLAUDE.md` |
| `internal/uikit/` (shared TUI + styling foundation) | `internal/uikit/doc.go` |

`ARCHITECTURE.md` is a narrative tour written for Python/JS devs new to Go — read it to orient, not as spec.

## Modern Go style

- **Prefer `lo` patterns over vanilla loops whenever equivalently correct.** Audience is Python/JS devs — `lo.Map` / `lo.Filter` / `lo.GroupBy` / `lo.KeyBy` read closer to their intuitions than `for`+`append`. **Invoke the `lo` skill** before writing any slice/map/set transform — it has the full catalog, decision framework, and `*Err` variant tables.
- **Stdlib when it suffices** (`slices`, `maps`, `cmp`). **No legacy `sort` package** — use `slices.Sort` / `slices.SortFunc` / `slices.SortStableFunc` / `slices.BinarySearch`. Banned by lint-guard rule 9.
- **Semgrep enforces loop-to-`lo` rewrites** via `.semgrep/go-modern.yml` (run through `just lint-style`). Don't create new debt.

## Cross-cutting design rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`.
- **CLI framework:** urfave/cli v3. All flags use `Local: true`.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. The canonical store lives at `internal/store/sqlite/` (raw `database/sql`, used by `sci db`, `sci view`, and dbtui). `internal/zot/local/` keeps its own raw `database/sql` connection because it opens `zotero.sqlite` in read-only immutable mode.
- **DuckDB:** shell out to the `duckdb` CLI via `internal/duck/` (required dep in `internal/doctor/Brewfile`). Every `sci db` verb dispatches on `.duckdb` extension; `sci view foo.duckdb` opens through the native subprocess-backed store at `internal/store/duck/`. Row-level mutations (UpdateCell / DeleteRows / InsertRows) work on tables with a PRIMARY KEY; DDL (RenameTable / DropTable / CreateEmptyTable) and import (ImportCSV / AppendCSV / ImportFile via `read_csv_auto` / `read_json_auto`) are bodied out against the native subprocess. See `internal/db/CLAUDE.md` for details.
- **Bubbletea v2 + bubbles v2** everywhere. No v1 imports.
- **No inline `lipgloss.NewStyle()`** outside `internal/uikit/`. Access via the `uikit.TUI` singleton — semantic styles via accessors, raw containers via `uikit.TUI.Base()`. Enforced by `rules/no-inline-newstyle.yml`.
- **`huh` forms go through `uikit`:** use `uikit.RunForm` / `uikit.Input` / `uikit.InputInto` / `uikit.Select`. Never call `.Run()` on a huh form directly — `RunForm` handles theme, keymap, and stdin drain. Confirmations use `cmdutil.Confirm`/`cmdutil.ConfirmYes`. Enforced by lint-guard rule 14.
- **`uikit` first for TUI components.** See `internal/uikit/doc.go` for the catalog. Extend uikit when a pattern appears in ≥ 2 TUIs.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **New TUI apps** go under `internal/tui/<name>/` with an `app/` subpackage (model, update, view, keys, run) and a root-pkg entry point that calls `uikit.Run` / `uikit.RunModel`. Styles come from `uikit` — don't create a per-TUI `ui/` package.
- **Large subcommand trees** (e.g. `zot`) live in `internal/<pkg>/cli.Commands()` and are mounted under `sci` via `cmd/sci/<pkg>.go`. Small single-file subcommands (proj, db, etc.) are declared directly in `cmd/sci/<pkg>.go`. There is no standalone binary for sub-tools — everything is `sci <cmd>`.
- **Namespace parents reject unknown children automatically** via `cmdutil.WireNamespaceDefaults(root)` called once in `cmd/sci/root.go:buildRoot()`. Don't wire per-command; don't disable this unless you have a specific reason (and then add a test).

## Testing rules

- **teatest** for every bubbletea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- DB mutations verified by querying the store directly, not by inspecting model state.
- No `time.Sleep` in tests — use `teatest.WaitFor`.
- Golden file updates: `go test ./path -run TestName -update` (only place raw `go test` is acceptable; the `-update` flag isn't wired through `just`).

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1`.
- marimo export exits non-zero for `mo.md()` cells — assert on the produced file, not the exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- GitHub Classroom URL IDs are *org* IDs, not classroom IDs — `ResolveClassroomID` maps URL → API ID and caches in `cass.yaml` as `api_id`.
- `internal/brew/`: no `brew bundle` in hot paths (use direct `brew` + `brew.CollectSnapshot`); Brewfile is a *lockfile* — resolve via `brew.LocateBrewfile()`, don't hardcode the XDG default.
