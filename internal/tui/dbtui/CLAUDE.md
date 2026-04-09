# CLAUDE.md -- dbtui (internal/tui/dbtui/)

VisiData-inspired SQLite viewer/editor. Also installable standalone via `cmd/dbtui/`.

## Architecture

- Single backend: `data.Store` (raw `database/sql` + `modernc.org/sqlite`, no CGO). Intentionally separate from sci-go's `pocketbase/dbx`-based stores.
- SQLite uses implicit `rowid` for all edits.
- Bubble Tea MVU with a single `Model`. `data.DataStore` interface is the integration contract.

## Conventions

- **Keys**: all key strings are constants in `keys.go`. Never use bare string literals in key dispatch.
- **Styles**: use `ui.TUI` singleton. Never inline `lipgloss.NewStyle()`.
- **Zones**: all clickable elements must be zone-marked. IDs: `tab-N`, `col-N`, `row-N`, `hint-ID`.
- **SQL safety**: always validate identifiers with `IsSafeIdentifier` before interpolation.
- **Cache invalidation**: use `tab.invalidateVP()` (not direct `cachedVP = nil`).
- **errcheck**: `defer func() { _ = foo.Close() }()`, not bare `defer foo.Close()`.

## Testing

- **Teatest integration tests** (`teatest_*.go`): full message loop. Golden files in `testdata/*.golden`, update with `go test ./internal/tui/dbtui/app/ -run TestTeatest -update`.
- Helpers (`startTeatest`, `sendKey`, `finalModel`, etc.) in `teatest_test.go`. See `app/TESTING.md` for checklist.
- DB mutations verified by querying the store directly, not just inspecting model state.

## Gotchas

- `test.db` is a committed fixture — tests depend on exact row counts. Don't modify without updating tests.
- Mutation tests use `copyFixture` to copy fixtures to temp dirs.
- `ReadOnlyQuery` caps at 200 rows with 10s timeout.
