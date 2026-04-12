# CLAUDE.md — internal/tui/board/

Bubbletea TUI for shared kanban boards. Sits on top of the headless `internal/board` sync engine — read that package's CLAUDE.md for the data model and Store API.

**This is a scaffold:** three screens (picker / grid / detail), keyboard nav, optimistic-update plumbing, teatest coverage. **No edit UX yet.** Everything is arranged so styles and interactions can be added without restructuring.

Mirrors the `internal/tui/dbtui/app/` file split — if you know dbtui, you know where to look.

**Any new TUI work here must invoke the `bubbletea` skill** before designing layouts or adding mouse/keyboard handling. **Invoke the `lo` skill** before writing any slice/map/set transforms — see root `CLAUDE.md` § Modern Go style.

## Optimistic write flow (the load-bearing wiring)

When the first edit feature lands, follow this exact pattern via `AppendCmd` in `app/cmds.go`:

1. Key handler in `keys.go` builds the payload.
2. Apply the mutation to `m.current` *immediately* (optimistic).
3. Return `AppendCmd(m.store, m.current.ID, op, payload)` alongside `m`.
4. `appendDoneMsg` in `update.go` flashes a status toast on error but does **NOT** roll back — the event stays in `events_pending` and `Store.FlushPending` will retry.

The non-rollback behavior is deliberate: the local cache is the source of truth for the user's intent, and reapplying pending events on every `Load` is what makes offline edits survive restarts.

## Entry point

`board.Run(store *engine.Store, initialBoard string) error` is the only public surface — `app/` is internal so consumers can't reach into the model. A non-empty `initialBoard` auto-dispatches `loadBoardCmd` from `Init()` so the grid pops up as soon as the board fetches.

## Conventions

- **Visuals.** All lipgloss styles + layout constants (`ColumnWidth`, `ColumnGap`, `CardPaddingX/Y`, `CardGap`) live in `ui/styles.go`. No inline `lipgloss.NewStyle` in view code; styles are looked up via `ui.TUI`.
- **Poll cadence.** `pollInterval` in `app/cmds.go` is a `var` (30s default) so tests can shorten it. `pollCmd` re-schedules on every tick.

## Testing

Uses teatest, same pattern as dbtui.

- **`setupStore(t)`** builds a real `engine.Store` backed by an in-memory `fakeObjectStore` + temp SQLite `LocalCache`, pre-populated with fixture board `alpha` (two columns, three cards). Use this for any new test.
- **`startTeatest(t)`** returns a `*teatest.TestModel` already waited-on for `"alpha"` — picker is rendered and boards loaded.
- **`sendKey` / `sendSpecial` / `finalModel`** match dbtui's shapes. Use `sendSpecial(tm, tea.KeyEnter)` for Enter, not a rune.

### Gotchas

- **`Ctrl+C` must always reach `tea.Quit`** regardless of screen. Otherwise `finalModel`/`FinalOutput` hang and the test times out at 3s. The global handler in `keys.go` handles `"ctrl+c"` independently of `q` cases.
- **Consecutive `waitForOutput` calls drain the buffer.** `teatest.WaitFor` reads from `tm.Output()` (a `bytes.Buffer`), so a prior `waitForOutput("alpha")` consumes those bytes. A second wait for a string already written in the same render pass will time out with empty "Last output" even though `FinalOutput` shows it. Fix: wait on exactly one substring per screen transition (the last one written), or use `tm.FinalOutput` + `bytes.Contains` for multi-assertion.

## Intentional non-goals (v1)

Do not add without a direct user request: edit UX (textinput / textarea / huh / date picker / chip input), card drag-drop, mouse routing + bubblezone, overlay system (help / confirm / palette), auto-snapshot trigger, history viewer, cross-board search, attachments, permissions UI, `cmd/board.go` subcommands.
