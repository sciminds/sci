# CLAUDE.md — sci CLI (Go)

## Workflow

- **Audience: Python/JS developers learning Go.** Prefer `lo` over hand-rolled loops, explicit over clever, readable over terse.
- **`just ok` is the gate.** Run after every change. Never invoke `go build` / `go test` / `gofmt` directly — always go through `justfile` recipes (`just test`, `just run …`, `just lint`, etc.). If you need a recipe that doesn't exist, add it.
- **TDD by default** for new features and bug fixes: write the failing test first, then make it pass. Skip TDD only for trivial edits (typos, doc tweaks, one-line refactors).
- **Bubbletea work → invoke the `bubbletea` skill** before designing layouts, fixing rendering bugs, or adding mouse/keyboard handling. Its `references/golden-rules.md` prevents the most common border/overflow bugs. Required for any new TUI screen.
- **All work on `main`.** Linear project: `sciminds-cli` (team EJO).

## Test recipes

```
just ok              # gate: fmt + vet + lint + test + build
just test-pkg PKG    # single-package tests (fast TDD loop): just test-pkg ./internal/zot
just test-slow       # proj/new integration (~4 min, SLOW=1, needs pixi/uv/quarto/marimo/typst/node)
just test-canvas     # cass integration (needs CANVAS_TOKEN in .env + gh auth login)
just test-board-live # R2 round-trip + privacy assertion (BOARD_LIVE=1)
just test-zot-real   # opt-in real-Zotero-DB smoke (reads ./zotero.sqlite)
```

## Sub-CLAUDE pointers (read before editing these packages)

| When you touch… | Read first |
|---|---|
| `internal/board/` (sync engine, R2 event log) | `internal/board/CLAUDE.md` |
| `internal/tui/board/` (kanban TUI) | `internal/tui/board/CLAUDE.md` |
| `internal/tui/dbtui/` (SQLite browser) | `internal/tui/dbtui/CLAUDE.md` + `app/TESTING.md` |
| `internal/zot/` (Zotero CLI + hygiene) | `internal/zot/CLAUDE.md` |
| `internal/uikit/` (shared TUI + styling foundation) | `internal/uikit/doc.go` |

`ARCHITECTURE.md` and `internal/README.md` are sketches and may be stale — trust the code.

## Modern Go style

- **`samber/lo` for transforms, stdlib when it suffices.** Never hand-roll `for`+`append` when `lo` or `slices`/`maps`/`cmp` provides it. **Invoke the `lo` skill** before writing any slice/map/set transform — it has the full function catalog, decision framework, and `*Err` variant tables.
- **No legacy `sort` package.** Banned by lint-guard rule 9. Use `slices.Sort` / `slices.SortFunc` / `slices.SortStableFunc` / `slices.BinarySearch`.
- **Semgrep enforces this.** `.semgrep/go-modern.yml` flags manual loops replaceable by `lo` or stdlib. Run via `just lint-style`. Don't create new semgrep debt.

## Cross-cutting design rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`.
- **CLI framework:** urfave/cli v3. All flags use `Local: true`.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. Default to `pocketbase/dbx` via `internal/db/data/`. Documented exceptions that use raw `database/sql`: `internal/tui/dbtui/data/`, `internal/zot/local/`, `internal/board/` LocalCache. The reason in every case is "this package is reusable standalone and must not pull in pocketbase".
- **Bubbletea v2 + bubbles v2** everywhere. No v1 imports.
- **No inline `lipgloss.NewStyle()`** outside `internal/uikit/` or `internal/tui/*/ui/`. Access via the `uikit.TUI` singleton. `huh` forms use `cmdutil.HuhTheme()` + `cmdutil.HuhKeyMap()`.
- **`uikit` first for TUI components.** Read `internal/uikit/doc.go` for the full catalog: colors (Palette, Styles, symbols, OK/Hint/Header/NextStep), input (keys, keymaps), layout (dims, Spread/Fit/Pad/PageLayout/SummaryLine), components (Chrome, Overlay, OverlayBox, ListPicker, SelectList, Grid2D, Screen/Router, RunWithSpinner/RunWithProgress), and runtime (Run/RunModel, DrainStdin, AsyncCmd, IsQuiet). Prefer uikit over hand-wiring bubbles directly. Extend uikit when a pattern appears in ≥ 2 TUIs.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **Reuse shared infra** (`cmdutil`, `uikit`, `netutil`) — don't re-implement spinners, confirms, lists, or styling per-package.
- **New TUI apps** go under `internal/tui/<name>/` and follow the dbtui split (`app/`, `ui/`, root-pkg `Run` entry).
- **Two-surface CLIs** (e.g. `zot`): full command tree lives in `internal/<pkg>/cli.Commands()`; both `cmd/<pkg>/main.go` and `cmd/sci/<pkg>.go` import it. Never duplicate wiring.

## Testing rules

- **teatest** for every bubbletea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- DB mutations verified by querying the store directly, not by inspecting model state.
- No `time.Sleep` in tests — use `teatest.WaitFor`.
- Golden file updates: `go test ./path -run TestName -update` (only place raw `go test` is acceptable; the `-update` flag isn't wired through `just`).

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1`.
- marimo export exits non-zero for `mo.md()` cells — assert on the produced file, not the exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- CI uses a rolling `latest` release tag (delete + recreate on push to main).
- GitHub Classroom URL IDs are *org* IDs, not classroom IDs — `ResolveClassroomID` maps URL → API ID and caches in `cass.yaml` as `api_id`.
- No `brew bundle` in hot paths. All install/check/list operations use direct `brew` commands and `brew.CollectSnapshot` for reliable name matching. `brew.Sync` reconciles bidirectionally with the actual brew/uv state.
- Brewfile is a *lockfile*, not a manifest. Resolve its path via `brew.LocateBrewfile()` — never hardcode the XDG default.
