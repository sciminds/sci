# CLAUDE.md — sci CLI (Go)

## Build & Test

```bash
just ok              # gate: fmt + vet + lint + test + build — run after every change
just test-slow       # integration tests (~4 min, needs SLOW=1, pixi/uv/quarto/marimo/typst/node)
```

## Design Rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`. Emit via `cmdutil.Output(cmd, result)`.
- **Process-replacing exec:** external tools (REPL, marimo) use `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for testing.
- **SQLite access:** use `pocketbase/dbx` query builder — never raw `database/sql` (exception: `internal/markdb/` uses `database/sql` directly). Pure Go, no CGO (`modernc.org/sqlite`). Prefer query builder methods in this order:
  1. `db.Select().From().Where().All(&rows)` — typed SELECT with struct scanning.
  2. `db.Update("table", dbx.Params{...}, dbx.HashExp{...})` — conditional updates via Params map (never build dynamic SET strings).
  3. `db.Delete("table", dbx.HashExp{...})` — simple deletes.
  4. `db.Insert("table", dbx.Params{...})` — simple inserts.
  5. `insertOrIgnore(db, "table", dbx.Params{...})` — conflict-safe inserts (shared helper in queries.go).
  6. `db.NewQuery(sql).Bind(params)` — raw SQL only when the builder can't express it (UPDATE OR IGNORE, COALESCE/NULLIF, CREATE VIEW, complex subqueries, search WHERE fragments).
  - Use `dbx.HashExp{}` for equality conditions, `dbx.And()`/`dbx.Or()`/`dbx.Not()` for composition, `dbx.NewExp()` for raw SQL fragments with `{:name}` params.
- **UI centralization:** no inline `lipgloss.NewStyle()` outside a `ui/` package. sci-go CLI styles live in `internal/ui/` (`ui.TUI.X().Render(...)`). dbtui has its own `internal/tui/dbtui/ui/` for TUI-specific styles. See `internal/tui/dbtui/CLAUDE.md` for dbtui-specific conventions.
- **TUI viewer:** the interactive database viewer lives in `internal/tui/dbtui/` and is also installable as a standalone `dbtui` binary via `cmd/dbtui/`. The `data.DataStore` interface lives in `internal/tui/dbtui/data/`; sci-go's `SQLiteStore` and `FileViewStore` in `internal/db/data/` implement it. Future TUI apps go under `internal/tui/`.
- **Database manager (`internal/db/`):** shared foundation for all database operations. `internal/db/data/` has `SQLiteStore` (pocketbase/dbx) and `FileViewStore` (in-memory SQLite for flat files). All subcommands require an explicit db file argument. Subcommands: `info`, `view`, `create`, `reset`, `add`, `delete`, `rename`, `sync`.
- **Markdown ingestion (`internal/markdb/`):** standalone `markdb` binary via `cmd/markdb/`, also available as `sci markdb` (Experimental). Ingests directories of `.md` files into SQLite with dynamic columns from YAML frontmatter, wikilink/markdown link tracking, and FTS5 full-text search. Uses `database/sql` directly (not pocketbase/dbx). Output `.db` files are compatible with dbtui. Subcommands: `ingest`, `search`, `info`, `diff`, `export`.

## Conventions

- `defer func() { _ = f.Close() }()` — errcheck requires the blank identifier.
- All work on `main`. Linear project: `sciminds-cli` (team EJO).

## Gotchas

- Integration tests skip unless `SLOW=1` is set.
- marimo export exits non-zero for `mo.md()` cells — expected. Assert on produced file, not exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- CI uses a rolling `latest` release tag (delete + recreate on push to main).
