# CLAUDE.md — dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite + DuckDB viewer/editor. Mounted under `sci view <file>` and `sci db view <file>`.

**Any new TUI work here must invoke the `bubbletea` skill** before designing layouts or adding mouse/keyboard handling. **Invoke the `lo` skill** before writing any slice/map/set transforms — see root `CLAUDE.md` § Modern Go style.

## Architecture

- The data layer lives at `internal/store/` (interface) plus `internal/store/sqlite/` (SQLite impl, raw `database/sql` + modernc.org/sqlite) and `internal/store/duck/` (native DuckDB impl, backed by a long-running `duckdb -jsonlines` subprocess). DataStore is the interface dbtui programs against.
- SQLite uses implicit `rowid` for all edits.
- **duckdb files** open natively via `internal/store/duck/` and speak the same `store.DataStore` interface as the SQLite store. UpdateCell / DeleteRows / InsertRows work on tables with a PRIMARY KEY; PK-less tables surface as `tab.ReadOnly = true` via the optional `store.RowEditabilityChecker` interface. DDL (RenameTable, DropTable, CreateEmptyTable) and import (ImportCSV, AppendCSV, ImportFile) all work against the native backend. See `internal/db/CLAUDE.md` for the dual-backend dispatch.

## Conventions

- **Styles**: all styles via `uikit.TUI`, including modal-editor cell styles (`CursorBlue`, `CursorOrange`, `CursorPink`, `SelectPink`, `HeaderGreenBg`, `CursorRaised`). No package-local style files.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `store.IsSafeIdentifier` before interpolation. Cache invalidation goes through `tab.invalidateVP()`, not direct `cachedVP = nil`.

## Testing

See `app/TESTING.md` for the full teatest protocol, checklist, and file placement guide.

- DB mutations verified by querying the store directly, not just inspecting model state.
- The canonical `test.db` fixture lives in `internal/store/sqlite/testdata/test.db`; SQLite store tests reference it from there. dbtui's own teatest models spin up their own per-test SQLite files via `sqlite.Open(t.TempDir() + …)`.
- Mutation tests use `copyFixture` to copy fixtures to temp dirs.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.
