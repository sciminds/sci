# CLAUDE.md — dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite + DuckDB viewer/editor. Mounted under `sci view <file>` and `sci db view <file>`.

## Architecture

- The data layer lives at `internal/store/` (interface) plus `internal/store/sqlite/` (SQLite, raw `database/sql` + modernc.org/sqlite) and `internal/store/duck/` (native DuckDB, a long-running `duckdb -jsonlines` subprocess). `store.DataStore` is the interface dbtui programs against — the same model drives either backend.
- SQLite uses implicit `rowid` for all edits.
- **duckdb files** speak the same `store.DataStore`; row-edit/PK rules, read-only tabs (`store.RowEditabilityChecker`), DDL, and import are documented in `internal/store/duck/` godoc and `internal/db/CLAUDE.md` (dual-backend dispatch).
- Which store a path gets (duckdb / parquet / viewable flat file / sqlite) is decided by `db.RunTUI`'s file-type switch in `internal/db/commands.go` — dbtui never sniffs files itself.

## Conventions

- **Styles**: all styles via `uikit.TUI`, including modal-editor cell styles (`CursorBlue`, `CursorOrange`, `CursorPink`, `SelectPink`, `HeaderGreenBg`, `CursorRaised`). No package-local style files.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `store.IsSafeIdentifier` before interpolation. Viewport-cache invalidation goes through `(*tabstate.Tab).InvalidateVP()`, never by nilling `Tab.CachedVP` directly.
- **Clipboard**: every system-clipboard write goes through `uikit.Copy` (`y`/`Y` in `app/yank.go`, visual mode's `Y`/`C`) — never a package-local `pbcopy`/`xclip` shell-out. Same rule as styles: the platform dispatch lives in `uikit`.

## Testing

See `app/TESTING.md` for the full teatest protocol, checklist, and file placement guide.

- DB mutations verified by querying the store directly, not just inspecting model state.
- The canonical `test.db` fixture lives in `internal/store/sqlite/testdata/test.db`; SQLite store tests reference it from there. dbtui's own teatest models spin up their own per-test SQLite files via `sqlite.Open(t.TempDir() + …)`.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.
- **Debugging the overlay stack:** the six nil-checked overlay states make "which message opened/closed which overlay?" hard to eyeball. Run `SCI_TUI_DEBUG=/tmp/dbtui.log sci view <file>` and `tail -f` the log to watch every `tea.Msg` live (see root `CLAUDE.md` → "Debugging a live TUI").
