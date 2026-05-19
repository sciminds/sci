# CLAUDE.md — dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite viewer/editor. Also installable standalone via `cmd/dbtui/`.

**Any new TUI work here must invoke the `bubbletea` skill** before designing layouts or adding mouse/keyboard handling. **Invoke the `lo` skill** before writing any slice/map/set transforms — see root `CLAUDE.md` § Modern Go style.

## Architecture

- The data layer lives at `internal/store/` (interface) and `internal/store/sqlite/` (SQLite impl, raw `database/sql` + modernc.org/sqlite). DataStore is the interface dbtui programs against; `*sqlite.Store` is the concrete type for SQLite paths. A native DuckDB backend will land alongside as `internal/store/duck/`.
- SQLite uses implicit `rowid` for all edits.
- **duckdb files** are still opened via a tempfile SQLite mirror today — `sci view foo.duckdb` calls `duck.BuildSQLiteMirror` and opens that with `WithReadOnly()`. Native DuckDB dispatch will replace this when `internal/store/duck/` lands. See `internal/db/CLAUDE.md` for the current dual-backend dispatch.

## Conventions

- **Styles**: mode-specific cursor/header styles via `modeTUI` singleton in `app/mode_styles.go` (`CursorBlue`, `CursorOrange`, `CursorPink`, `SelectPink`, `HeaderGreenBg`, `CursorRaised`). Shared styles via `uikit.TUI`.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `store.IsSafeIdentifier` before interpolation. Cache invalidation goes through `tab.invalidateVP()`, not direct `cachedVP = nil`.

## Testing

See `app/TESTING.md` for the full teatest protocol, checklist, and file placement guide.

- DB mutations verified by querying the store directly, not just inspecting model state.
- The canonical `test.db` fixture lives in `internal/store/sqlite/testdata/test.db`; SQLite store tests reference it from there. dbtui's own teatest models spin up their own per-test SQLite files via `sqlite.Open(t.TempDir() + …)`.
- Mutation tests use `copyFixture` to copy fixtures to temp dirs.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.
