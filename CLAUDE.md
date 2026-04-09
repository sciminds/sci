# CLAUDE.md — sci CLI (Go)

## Build & Test

```bash
just ok              # gate: fmt + vet + lint + test + build — run after every change
just test-slow       # proj/new integration tests (~4 min, needs SLOW=1, pixi/uv/quarto/marimo/typst/node)
```

## Design Rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`. Emit via `cmdutil.Output(cmd, result)`.
- **Process-replacing exec:** external tools (REPL, marimo) use `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for testing.
- **SQLite access:** pure Go, no CGO (`modernc.org/sqlite`). Use `pocketbase/dbx` query builder in `internal/db/data/`. Exceptions: `internal/tui/dbtui/data/` and `internal/markdb/` use raw `database/sql` (intentional — dbtui avoids the pocketbase dependency).
- **UI centralization:** no inline `lipgloss.NewStyle()` outside `internal/ui/` (CLI) or `internal/tui/dbtui/ui/` (TUI). Access via `ui.TUI` singleton.
- **CLI framework:** urfave/cli v3 (not Cobra). All flags use `Local: true`.

## Conventions

- `defer func() { _ = f.Close() }()` — errcheck requires the blank identifier.
- All work on `main`. Linear project: `sciminds-cli` (team EJO).

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1` is set. Teatests run unconditionally in `just ok`.
- marimo export exits non-zero for `mo.md()` cells — expected. Assert on produced file, not exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- CI uses a rolling `latest` release tag (delete + recreate on push to main).
- `cass` integration tests require `CANVAS_TOKEN` in `.env` and `gh auth login`. Run via `just test-canvas`.
- GitHub Classroom URL IDs are org IDs, not classroom IDs. `ResolveClassroomID` maps URL → API ID via `GET /classrooms`. The resolved ID is cached in `cass.yaml` as `api_id`.
