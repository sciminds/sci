# Architecture

`sci` is a CLI toolkit for managing Python-based scientific computing projects.
It is written in Go and distributed as a single static binary.

## Binaries

```
cmd/sci/       Main CLI — project management, notebooks, databases, sharing, video
cmd/dbtui/     Standalone database browser (also available as `sci view`)
cmd/markdb/    Standalone Markdown-to-SQLite tool (also available as `sci markdb`)
```

## Data Flow

```
User
 |
 v
cmd/sci/root.go          CLI entry point (urfave/cli/v3)
 |
 +---> internal/proj/     Python project detection + scaffolding
 +---> internal/py/       Python REPL, notebook conversion
 +---> internal/db/       Database create/reset/import/rename/delete/view
 |       |
 |       +---> internal/db/data/         SQLiteStore (pocketbase/dbx)
 |       +---> internal/tui/dbtui/app/   Interactive database browser
 |                |
 |                +---> internal/tui/dbtui/data/   DataStore interface (database/sql)
 |                +---> internal/tui/dbtui/ui/     TUI design system
 |
 +---> internal/markdb/   Markdown ingestion (database/sql, FTS5)
 +---> internal/share/    Upload/download datasets (Cloudflare R2)
 +---> internal/cass/     Canvas LMS & GitHub Classroom sync
 +---> internal/brew/     Homebrew/uv package management
 +---> internal/vid/      Video/audio editing (ffmpeg wrapper)
 +---> internal/lab/      SFTP lab storage access
 +---> internal/guide/    Interactive asciicast tutorials
 +---> internal/helptui/  Interactive help TUI with demo overlays
 +---> internal/mdview/   Embeddable markdown viewer TUI
 +---> internal/doctor/   System health checks + setup flow
 +---> internal/selfupdate/   Binary self-update from GitHub
 |
 +---> internal/cmdutil/  Result interface, JSON/human output routing
 +---> internal/ui/       Centralized CLI styling (lipgloss)
 +---> internal/netutil/  Network error helpers
 +---> internal/version/  Build-time version metadata
```

## Key Design Decisions

### Pure Go SQLite (no CGO)
We use `modernc.org/sqlite` so the binary is fully static and cross-compiles
without a C toolchain. This means no `database/sql` drivers that require CGO.

### Two SQLite Access Layers
- **`internal/db/data/`** uses `pocketbase/dbx` — a typed query builder for the
  database manager commands (create, import, sync).
- **`internal/tui/dbtui/data/`** and **`internal/markdb/`** use raw `database/sql` —
  these need more dynamic SQL (FTS5, virtual tables, user-defined queries).

This is intentional: the dbtui database browser is designed to be reusable
outside sci-go, so it avoids the pocketbase dependency.

### Process-Replacing Exec
Commands that launch interactive tools (Python REPL, marimo, Quarto) use
`syscall.Exec` to replace the Go process entirely. This gives the child process
full control of the terminal. We export `Build*Args` helper functions so tests
can verify argument construction without actually exec'ing.

### Bubble Tea v2
All terminal UIs use the Bubble Tea Model-View-Update (MVU) pattern:
1. **Model** — a struct holding all application state
2. **Update** — a pure function: `(Model, Msg) -> (Model, Cmd)`
3. **View** — a pure function: `Model -> string` (the rendered UI)

Messages flow through Update; side effects are returned as `Cmd` functions.
This makes the UI testable without a real terminal — see `teatest` integration
tests across TUI packages.

### Centralized Styling
CLI styles live in `internal/ui/` and are accessed via `ui.TUI.StyleName()`.
The database browser has its own design system in `internal/tui/dbtui/ui/`.
No inline `lipgloss.NewStyle()` calls outside these packages.

## Testing

```bash
just ok           # Fast gate: fmt + vet + lint + test + build
just test-slow    # Integration tests (SLOW=1, needs pixi/uv/quarto/marimo/typst/node)
just test-canvas  # Canvas/GitHub Classroom integration (needs CANVAS_TOKEN + gh auth)
```

- Unit tests: pure logic, no I/O, fast
- Teatest: full Bubble Tea message loop, runs unconditionally
- Integration tests: shell out to real tools, gated behind `SLOW=1`
