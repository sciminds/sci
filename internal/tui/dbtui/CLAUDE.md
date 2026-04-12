# CLAUDE.md — dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite viewer/editor. Also installable standalone via `cmd/dbtui/`.

**Any new TUI work here must invoke the `bubbletea` skill** before designing layouts or adding mouse/keyboard handling. **Invoke the `lo` skill** before writing any slice/map/set transforms — see root `CLAUDE.md` § Modern Go style.

## Architecture

- Single backend: `data.Store` — raw `database/sql` + `modernc.org/sqlite`. **Must not import `pocketbase/dbx` or anything that pulls it in** — `cmd/dbtui` is a standalone binary and dragging in pocketbase would bloat it. This is the entire reason for the raw-`database/sql` exception.
- SQLite uses implicit `rowid` for all edits.
- Bubble Tea v2 MVU with a single `Model`. `data.DataStore` interface is the integration contract.

## Conventions

- **Keys**: all key strings are constants in `keys.go`. Never use bare string literals in key dispatch.
- **Styles**: use `ui.TUI` singleton from `internal/tui/dbtui/ui/`. Never inline `lipgloss.NewStyle()`.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `IsSafeIdentifier` before interpolation.
- **Cache invalidation**: use `tab.invalidateVP()` (not direct `cachedVP = nil`).

## Testing

See `app/TESTING.md` for the full teatest protocol, checklist, and file placement guide.

- DB mutations verified by querying the store directly, not just inspecting model state.
- `test.db` is a committed fixture — tests depend on exact row counts. Don't modify without updating tests.
- Mutation tests use `copyFixture` to copy fixtures to temp dirs.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.
