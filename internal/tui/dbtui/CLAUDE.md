# CLAUDE.md — dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite + DuckDB viewer/editor. Mounted under `sci view <file>` and `sci db view <file>`.

## Architecture

- The data layer lives at `internal/store/` (interface) plus `internal/store/sqlite/` (SQLite, raw `database/sql` + modernc.org/sqlite) and `internal/store/duck/` (native DuckDB, a long-running `duckdb -jsonlines` subprocess). `store.DataStore` is the interface dbtui programs against — the same model drives either backend.
- SQLite uses implicit `rowid` for all edits.
- **duckdb files** speak the same `store.DataStore`; row-edit/PK rules, read-only tabs (`store.RowEditabilityChecker`), DDL, and import are documented in `internal/store/duck/` godoc and `internal/db/CLAUDE.md` (dual-backend dispatch).

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
