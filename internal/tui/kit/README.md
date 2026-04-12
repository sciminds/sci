# tui/kit — Bubbletea helpers for Svelte-brained devs

Lightweight primitives that sit **on top of** Bubbletea, not instead of it.
They target the three most common friction points when writing TUI code.

## Grid2D — 2D cursor with move/clamp/wrap

Replaces manual bounds-checking in key handlers. Horizontal movement
clamps, vertical movement wraps.  Row == -1 means "column focused, no
row selected" (like a kanban column header).

```go
// In your model:
cur kit.Grid2D  // {Col: 0, Row: -1}

// Build a rowsIn function from your data:
rowsIn := func(col int) int { return len(cardsByCol[col]) }

// Key handler — one line per direction:
case "h": m.cur.Move(-1, 0, len(cols), rowsIn)
case "l": m.cur.Move( 1, 0, len(cols), rowsIn)
case "j": m.cur.Move( 0, 1, len(cols), rowsIn)
case "k": m.cur.Move( 0,-1, len(cols), rowsIn)

// After data changes, reclamp so the cursor stays valid:
m.cur.Clamp(len(cols), rowsIn)
```

## Chrome — title/body/status layout

Handles the "sandwich" every TUI needs: fixed title bar, fixed status
bar, body fills the rest. You never compute `bodyH` yourself — Chrome
measures the title and status, gives the body what's left, and
pads/truncates to exactly `height` lines.

```go
chrome := kit.Chrome{
    Title:  func(w int) string { return style.Render("My App") },
    Status: func(w int) string { return style.Render("q quit") },
    Body:   func(w, h int) string { return renderContent(w, h) },
}
return chrome.Render(m.width, m.height)
```

`FitHeight(s, h)` is also exported if you need pad/truncate elsewhere
(e.g. inside a column frame).

## Screen + Router — kill the switch statements

In vanilla Bubbletea, every screen enum gets switched on 3-4 times:
`View()`, `handleKey()`, `renderTitle()`, `renderHelpHint()`. The Router
collapses that into one registration table.

**Step 1 — Define your screens:**

```go
type screen int
const (
    screenPicker screen = iota
    screenGrid
    screenDetail
)
```

**Step 2 — Register once in your constructor:**

```go
func buildRouter() kit.Router[screen, *Model] {
    return kit.NewRouter(map[screen]kit.Screen[*Model]{
        screenPicker: {
            View:  (*Model).viewPicker,
            Keys:  (*Model).handlePickerKey,
            Title: func(_ *Model, _ int) string { return "Pick a board" },
            Help:  "j/k move  enter open  q quit",
        },
        screenGrid: {
            View:  (*Model).viewGrid,
            Keys:  (*Model).handleGridKey,
            Title: func(m *Model, _ int) string { return m.current.Title },
            Help:  "hjkl move  enter detail  esc back",
        },
    })
}
```

**Step 3 — Dispatch in one call:**

```go
// In View — replaces a 3-way switch:
body := m.router.View(m.screen, m, w, h)

// In Update — replaces per-screen key dispatch:
return m.router.Keys(m.screen, m, msg)

// Title and help follow the same pattern:
title := m.router.Title(m.screen, m, w)
hint  := m.router.Help(m.screen)
```

Adding a new screen is now one struct literal in `buildRouter()` — no
hunting through 4 switch statements.

## Composing all three

Chrome + Router work together naturally in `buildView()`:

```go
chrome := kit.Chrome{
    Title: func(w int) string {
        return style.Render(m.router.Title(m.screen, m, w))
    },
    Status: func(w int) string {
        return style.Render(m.router.Help(m.screen))
    },
    Body: func(w, h int) string {
        return m.router.View(m.screen, m, w, h)
    },
}
return chrome.Render(m.width, m.height)
```

## Testing

All primitives are plain structs — no `tea.Model` dependency for unit
tests. Test Grid2D and Chrome without teatest:

```go
g := kit.Grid2D{Col: 0, Row: -1}
g.Move(0, 1, 3, func(col int) int { return 5 })
assert(g.Row == 0) // entered from -1

out := kit.Chrome{...}.Render(80, 24)
assert(lipgloss.Height(out) == 24) // always exact
```

For integration tests, they compose inside your Model the same as any
other field, so existing teatest patterns (send keys, wait for output,
assert on `finalModel`) work unchanged.

## See it in practice

`internal/tui/board/app/` uses all three. Start there.
