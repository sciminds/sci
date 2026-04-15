# CLAUDE.md — dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite viewer/editor. Also installable standalone via `cmd/dbtui/`.

**Any new TUI work here must invoke the `bubbletea` skill** before designing layouts or adding mouse/keyboard handling. **Invoke the `lo` skill** before writing any slice/map/set transforms — see root `CLAUDE.md` § Modern Go style.

## Architecture

- Single backend: `data.Store` — raw `database/sql` + `modernc.org/sqlite`. **Must not import `pocketbase/dbx` or anything that pulls it in** — `cmd/dbtui` is a standalone binary and dragging in pocketbase would bloat it. This is the entire reason for the raw-`database/sql` exception.
- SQLite uses implicit `rowid` for all edits.

## Conventions

- **Styles**: mode-specific cursor/header styles via `modeTUI` singleton in `app/mode_styles.go` (`CursorBlue`, `CursorOrange`, `CursorPink`, `SelectPink`, `HeaderGreenBg`, `CursorRaised`). Shared styles via `uikit.TUI`.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `IsSafeIdentifier` before interpolation. Cache invalidation goes through `tab.invalidateVP()`, not direct `cachedVP = nil`.

## Testing

See `app/TESTING.md` for the full teatest protocol, checklist, and file placement guide.

- DB mutations verified by querying the store directly, not just inspecting model state.
- `test.db` is a committed fixture — tests depend on exact row counts. Don't modify without updating tests.
- Mutation tests use `copyFixture` to copy fixtures to temp dirs.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.
