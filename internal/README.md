# internal/ — Package Guide

All packages live under `internal/` so they cannot be imported by external projects.
The three binaries (`cmd/sci`, `cmd/dbtui`, `cmd/markdb`) are the only public entry points.

## Package Map

| Package | What it does |
|---------|-------------|
| `brew/` | Wraps `brew bundle` for Homebrew package management |
| `cloud/` | Cloudflare R2 (S3-compatible) client and credential storage |
| `cmdutil/` | Shared CLI helpers: `Result` interface, JSON/human output routing, prompts |
| `db/` | Database manager — create, reset, import, sync, rename, view |
| `db/data/` | `SQLiteStore` (pocketbase/dbx) and `FileViewStore` (in-memory SQLite for flat files) |
| `doctor/` | System health checks (Homebrew, Xcode CLT, git, auth) |
| `guide/` | Interactive guide TUI with embedded asciicast playback |
| `markdb/` | Markdown → SQLite ingestion with frontmatter columns, link graph, FTS5 |
| `proj/` | Detects Python project managers (uv/pixi) and doc systems (Quarto/MyST) |
| `proj/new/` | Scaffolds new Python projects from templates |
| `proj/new/tui/` | Bubble Tea wizard for `sci new` |
| `py/` | Launches Python REPLs and environments |
| `py/convert/` | Converts notebooks between Marimo, MyST, and Quarto formats |
| `py/tutorials/` | Downloads tutorial notebooks from SciMinds |
| `selfupdate/` | Checks GitHub releases and applies binary updates |
| `share/` | Uploads/downloads datasets to Cloudflare R2 |
| `tui/dbtui/` | Interactive database browser (see [dbtui CLAUDE.md](tui/dbtui/CLAUDE.md)) |
| `ui/` | Centralized terminal styling via lipgloss (`ui.TUI` singleton) |
| `version/` | Build-time version metadata injected via ldflags |
| `vid/` | Wraps ffmpeg for video/audio editing |

## Key Patterns

- **`cmdutil.Result`** — every command returns `JSON() any` + `Human() string`, emitted via `cmdutil.Output(cmd, result)`.
- **SQLite** — pure Go (`modernc.org/sqlite`), no CGO. The `db/data/` package uses pocketbase/dbx query builder; `tui/dbtui/data/` and `markdb/` use raw `database/sql`.
- **TUI** — all terminal UIs use Bubble Tea v2 with the Model-View-Update pattern. Styles are centralized in `ui/` (CLI) and `tui/dbtui/ui/` (database browser).
- **Process-replacing exec** — external tools (Python REPL, marimo) use `syscall.Exec` to replace the process, not `exec.Command`.
