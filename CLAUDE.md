# CLAUDE.md — sci CLI (Go)

## Workflow

- **`just ok` is the gate.** Run after every change. Never invoke `go build` / `go test` / `gofmt` directly — always go through `justfile` recipes (`just test`, `just run …`, `just lint`, etc.). If you need a recipe that doesn't exist, add it.
- **TDD by default** for new features and bug fixes: write the failing test first, then make it pass. Skip TDD only for trivial edits (typos, doc tweaks, one-line refactors).
- **Bubbletea work → invoke the `bubbletea` skill** before designing layouts, fixing rendering bugs, or adding mouse/keyboard handling. Its `references/golden-rules.md` prevents the most common border/overflow bugs. Required for any new TUI screen.
- **All work on `main`.** Linear project: `sciminds-cli` (team EJO).

## Test recipes

```
just ok              # gate: fmt + vet + lint + test + build
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

`ARCHITECTURE.md` and `internal/README.md` are sketches and may be stale — trust the code.

## Cross-cutting design rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`.
- **CLI framework:** urfave/cli v3. All flags use `Local: true`.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. Default to `pocketbase/dbx` via `internal/db/data/`. Documented exceptions that use raw `database/sql`: `internal/tui/dbtui/data/`, `internal/markdb/`, `internal/zot/local/`, `internal/board/` LocalCache. The reason in every case is "this package is reusable standalone and must not pull in pocketbase".
- **Bubbletea v2 + bubbles v2** everywhere. No v1 imports.
- **No inline `lipgloss.NewStyle()`** outside `internal/ui/` or `internal/tui/*/ui/`. Access via the `ui.TUI` singleton. `huh` forms use `ui.HuhTheme()` + `ui.HuhKeyMap()`.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **Reuse shared infra** (`cmdutil`, `ui`, `netutil`) — don't re-implement spinners, confirms, or styling per-package.
- **New TUI apps** go under `internal/tui/<name>/` and follow the dbtui split (`app/`, `ui/`, root-pkg `Run` entry).
- **Two-surface CLIs** (e.g. `zot`): full command tree lives in `internal/<pkg>/cli.Commands()`; both `cmd/<pkg>/main.go` and `cmd/sci/<pkg>.go` import it. Never duplicate wiring.

## Testing rules

- **teatest** for every bubbletea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- DB mutations verified by querying the store directly, not by inspecting model state.
- No `time.Sleep` in tests — use `teatest.WaitFor`.
- Golden file updates: `go test ./path -run TestName -update` (only place raw `go test` is acceptable; the `-update` flag isn't wired through `just`).

## Audience

Collaborators are beginner/intermediate Go devs — keep code clear, avoid clever patterns, don't sacrifice efficiency for pedagogy.

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1`.
- marimo export exits non-zero for `mo.md()` cells — assert on the produced file, not the exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- CI uses a rolling `latest` release tag (delete + recreate on push to main).
- GitHub Classroom URL IDs are *org* IDs, not classroom IDs — `ResolveClassroomID` maps URL → API ID and caches in `cass.yaml` as `api_id`.
- `brew bundle check` exits non-zero when deps are missing (normal). `isBundleCheckOutput` in `brew.go` distinguishes that from a real failure.
- Brewfile is a *lockfile*, not a manifest. `brew.Sync` reconciles bidirectionally with the actual brew/uv state. Resolve its path via `brew.LocateBrewfile()` — never hardcode the XDG default.
