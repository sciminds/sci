# CLAUDE.md — sci CLI (Go)

## Build & Test

```bash
just ok              # gate: fmt + vet + lint + test + build — run after every change
just test-slow       # proj/new integration tests (~4 min, needs SLOW=1, pixi/uv/quarto/marimo/typst/node)
just test-canvas     # cass integration tests (needs CANVAS_TOKEN in .env + gh auth login)
```

## Design Rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`. Emit via `cmdutil.Output(cmd, result)`.
- **CLI framework:** urfave/cli v3 (not Cobra). All flags use `Local: true`.
- **SQLite:** pure Go, no CGO (`modernc.org/sqlite`). Use `pocketbase/dbx` in `internal/db/data/`. Exceptions: `internal/tui/dbtui/data/` and `internal/markdb/` use raw `database/sql` (intentional — dbtui avoids the pocketbase dependency).
- **UI centralization:** no inline `lipgloss.NewStyle()` outside `internal/ui/` or `internal/tui/dbtui/ui/`. Access via `ui.TUI` singleton. All `huh` forms use `ui.HuhTheme()` and `ui.HuhKeyMap()`.
- **Bubbletea v2:** all TUI code uses bubbletea v2 and bubbles v2. No v1 imports.
- **Process-replacing exec:** external tools (REPL, marimo) use `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for testing.

## Testing

- **teatest** for all bubbletea models — full message loop (key → Update → state → View). See `internal/tui/dbtui/app/TESTING.md` for protocol.
- DB mutations verified by querying the store directly, not just inspecting model state.
- No `time.Sleep` in tests — use `WaitFor` for async operations.
- Golden file updates: `go test ./path/to/pkg/ -run TestName -update`.

## Conventions

- `defer func() { _ = f.Close() }()` — errcheck requires the blank identifier.
- All work on `main`. Linear project: `sciminds-cli` (team EJO).
- Reuse shared infra (`cmdutil`, `ui`, `netutil`) — don't duplicate spinners, confirm prompts, or styling logic in feature packages.
- Every package gets a doc comment on the `package` declaration.
- New TUI apps go under `internal/tui/<appname>/` following dbtui's pattern.

## Audience

Collaborators are beginner/intermediate Go developers. Keep code clear and straightforward — avoid overly clever patterns. But don't sacrifice efficiency for pedagogy.

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1` is set. Teatests run unconditionally in `just ok`.
- marimo export exits non-zero for `mo.md()` cells — expected. Assert on produced file, not exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- CI uses a rolling `latest` release tag (delete + recreate on push to main).
- GitHub Classroom URL IDs are org IDs, not classroom IDs. `ResolveClassroomID` maps URL → API ID via `GET /classrooms`. The resolved ID is cached in `cass.yaml` as `api_id`.
