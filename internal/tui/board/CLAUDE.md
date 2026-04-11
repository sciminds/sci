# CLAUDE.md — internal/tui/board/

Bubbletea TUI for shared kanban boards. Sits on top of the headless
`internal/board` sync engine (see that package's CLAUDE.md for data
model, event log, and Store API).

This is a **scaffold**: three screens, keyboard nav, optimistic update
plumbing, teatest coverage. **No edit UX yet** — adding/patching/moving
cards is the next step. Everything is arranged so styles, spacing, and
new interactions can be adjusted without restructuring.

## Package layout

```
internal/tui/board/
  doc.go, run.go           root package — thin entry point: Run(store, initialBoard)
  ui/
    styles.go              ALL lipgloss styles + layout constants (one file, edit here)
  app/
    doc.go                 architecture overview
    types.go               screen enum, msg types, cursor, statusLine
    model.go               Model struct, NewModel, Init, cardsByColumn, focusedCard
    update.go              top-level msg dispatcher (window, loads, polls, keys)
    keys.go                per-screen key handlers
    cmds.go                listBoardsCmd, loadBoardCmd, AppendCmd, pollCmd
    view.go                chrome composition (title + body + status) + truncate
    view_picker.go         board list body
    view_grid.go           columns × cards body
    view_detail.go         card detail body
    teatest_test.go        shared fake ObjectStore + test helpers
    teatest_picker_test.go picker screen tests
    teatest_grid_test.go   grid navigation + enter/esc tests
```

Mirrors the `internal/tui/dbtui/app/` file split intentionally — if you know
dbtui, you know where to look here.

## Screens

Three screens, selected via `Model.screen` (`screenPicker`, `screenGrid`,
`screenDetail`):

| Screen      | Shows                                        | Keys                             |
|-------------|----------------------------------------------|----------------------------------|
| Picker      | board IDs from `Store.ListBoards`            | `j/k` move, `↵/l` open, `r` reload, `q` quit |
| Grid        | columns × cards for the loaded board         | `hjkl` move, `↵` detail, `r` reload, `esc/q` back |
| Detail      | read-only card: desc, labels, checklist, etc. | `esc/h` back, `q` to grid        |

`Ctrl+C` always quits globally regardless of screen. `q` from the picker
quits; from grid/detail it pops back one level.

## Where to edit what

**Visuals (colors, borders, padding, spacing):** `ui/styles.go` — single
file. Layout constants (`ColumnWidth`, `ColumnGap`, `CardPaddingX/Y`,
`CardGap`) live at the top of the same file. No inline `lipgloss.NewStyle`
in view code — styles are always looked up via `ui.TUI`.

**Layout math** (column width distribution, body/chrome split):
`app/view_grid.go` `viewGrid` and `app/view.go` `renderBody`.

**Keybindings:** `app/keys.go` — one handler per screen, plus the global
block at the top. Add new bindings here first.

**New interactions that mutate state:** wire via `AppendCmd` in
`app/cmds.go`. It's exported so the first edit flow can call it
directly. The flow is:
1. Handler in `keys.go` builds the payload.
2. Apply the mutation to `m.current` *immediately* (optimistic).
3. Return `AppendCmd(m.store, m.current.ID, op, payload)` alongside `m`.
4. `appendDoneMsg` in `update.go` flashes a status toast on error but
   does NOT roll back — the event stays in `events_pending` and
   `Store.FlushPending` will retry.

**Poll cadence:** `pollInterval` in `app/cmds.go` is a `var` (30s default)
so tests can shorten it. `pollCmd` re-schedules itself on every tick via
`tea.Tick`.

## Entry point

`board.Run(store *engine.Store, initialBoard string) error`. The root
package (`internal/tui/board`) owns this — `app/` is deliberately
internal to keep the consumer surface tiny.

A `nil` or empty `initialBoard` starts on the picker. A non-empty value
auto-dispatches a `loadBoardCmd` from `Init()` so the grid pops up as
soon as the board is fetched.

`cmd/board.go` is the place to wire this up (not yet built). It should
call `cloud.SetupBoard()` → `board.NewCloudAdapter` →
`board.OpenLocalCache` → `board.NewStore` → `tui.board.Run`.

## Testing

Uses teatest, same pattern as dbtui. Key conventions:

- `setupStore(t)` builds a real `engine.Store` backed by an in-memory
  `fakeObjectStore` + a temp SQLite `LocalCache`, pre-populated with a
  fixture board (`alpha`) with two columns and three cards. Use this
  for any new test.
- `startTeatest(t)` returns a `*teatest.TestModel` already waited on
  "alpha" appearing — i.e. picker is rendered and boards loaded.
- `sendKey` / `sendSpecial` / `finalModel` helpers match dbtui's
  shapes. `sendSpecial(tm, tea.KeyEnter)` for Enter, not a rune.
- `Ctrl+C` must always reach `tea.Quit` — otherwise `finalModel` and
  `FinalOutput` hang and the test times out at 3s. This was the bug
  behind the first failing run; the global handler in `keys.go` now
  handles `"ctrl+c"` independently of the `q` cases.

### Gotcha: consecutive `waitForOutput` calls drain the buffer

`teatest.WaitFor` allocates a fresh local byte buffer on each call and
reads from `tm.Output()`. A prior `waitForOutput("alpha")` drains bytes
from the underlying `bytes.Buffer`, so a second `waitForOutput("Write
tests")` immediately after only sees bytes written *after* the first
returned. If the string you want was already written (e.g. both appear
in the same render pass), the second wait will time out with an empty
"Last output" even though `FinalOutput` would show everything.

Fix: wait on exactly one substring per screen transition, ideally the
last one that will be written, or use `tm.FinalOutput` + `bytes.Contains`
when you need to assert on multiple things.

## Intentional non-goals (for v1)

Everything below is deferred and should NOT be added without a direct
user request:

- Edit UX (textinput/textarea/huh forms, date picker, chip input)
- Card drag/drop or inline move via keyboard
- Mouse routing + `bubblezone` clickable cards
- Overlay system (help, confirm dialogs, command palette)
- Auto-snapshot trigger when event count past the latest snap exceeds N
- History viewer / audit log
- Cross-board search
- Attachments or file uploads
- Permissions UI (v1 assumes all org members can edit everything)
- CLI subcommands under `cmd/board.go`
