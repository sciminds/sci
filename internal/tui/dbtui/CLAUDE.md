# CLAUDE.md -- dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite viewer/editor. Also installable standalone via `cmd/dbtui/`.

## Architecture

- Single backend: `data.Store` (raw `database/sql` + `modernc.org/sqlite`, no CGO). This is intentionally separate from sci-go's `pocketbase/dbx`-based `SQLiteStore` in `internal/db/data/`.
- SQLite uses implicit `rowid` for all edits.
- TUI follows Bubble Tea **Model-View-Update** with a single `Model`.
- `data.DataStore` interface is the integration contract: sci-go's `SQLiteStore` and `FileViewStore` implement it.

### Package layout

| Package | Purpose |
|---|---|
| `app/` | TUI application (single tea.Model) |
| `data/` | Database backend (DataStore interface, SQLite impl) |
| `tabstate/` | Tab state types + pure operations (no Bubble Tea deps) |
| `match/` | Fuzzy/substring text matching |
| `ui/` | Design system: styles, colors, layout, overlay primitives (separate from sci-go's `internal/ui/`) |

## Conventions

- **Keys**: all key strings are constants in `keys.go`. Never use bare string literals in key dispatch.
- **Styles**: use `ui.TUI` singleton accessors. Never inline `lipgloss.NewStyle()`.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `IsSafeIdentifier` before interpolation.
- **Cache invalidation**: use `tab.invalidateVP()` (not direct `cachedVP = nil`) when mutating tab state.
- **Overlay width**: use `ui.OverlayWidth(termW, minW, maxW)` for consistent overlay sizing.
- **errcheck**: use `defer func() { _ = foo.Close() }()`, not bare `defer foo.Close()`.

## Testing

- **Unit tests** (`app_test.go`, `visual_test.go`, `tabstate_test.go`): pure logic (sort, filter, selection math). No Bubble Tea runtime.
- **Teatest integration tests** (`teatest_*.go`): full message loop via [teatest](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest).
- **Golden files** (`testdata/*.golden`): snapshot comparison. Update with `go test ./internal/tui/dbtui/app/ -run TestTeatest -update`.
- New features must include teatest coverage. See `app/TESTING.md` for checklist, helpers, and file placement.
- Teatest helpers (`startTeatest`, `sendKey`, `finalModel`, etc.) live in `teatest_test.go`.
- DB mutations in tests are verified by querying the store directly, not just inspecting model state.

## Gotchas

- `test.db` is a committed fixture — tests depend on exact row counts. Don't modify it without updating tests.
- Mutation tests use `copyFixture` to copy fixtures to temp dirs, avoiding write contention.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.

## Product scope

Local data workbench replacing direct `sqlite3` CLI usage:
- **Import**: type-inferred import into SQLite. Formats: CSV, TSV, JSON, JSONL.
- **Export**: CSV export; export selected rows from visual mode.
- **Transform**: column rename, column drop, dedup, select/project columns into new tables.
- **Derived tables/views**: `CREATE TABLE ... AS SELECT` and `CREATE VIEW` from query results.
- **No cloud**: no login, push, pull, sync, or share features.
- **CLI mode**: deferred until TUI is complete.
