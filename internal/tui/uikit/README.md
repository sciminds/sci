# tui/uikit — shared visual foundation

The single import for styles, layout, and components across all three
binaries (`sci`, `dbtui`, `zot`). Zero project-specific dependencies
(no pocketbase, no urfave/cli) so standalone binaries stay lean.

## Layers

### Colors — `palette.go`, `styles.go`, `icons.go`

Wong colorblind-safe palette resolved for light/dark terminals at init.
`TUI` is the package-level `*Styles` singleton — ~70 pre-built lipgloss
styles behind named accessors:

```go
uikit.TUI.TextBlue().Render("highlighted")
uikit.TUI.Pass().Render(uikit.IconPass)
p := uikit.TUI.Palette()  // raw colors when you need them
```

`SurfaceRaised` is the palette slot for elevated backgrounds (used by
dbtui's cursor highlight vs the default `Surface`).

### Input — `keys.go`, `keymap.go`

Key constants (`KeyQ`, `KeyEnter`, `KeyEsc`, …) replace bare string
literals in Bubbletea switch cases. Shared bindings (`BindQuit`,
`BindUp`, `BindDown`, `BindEnter`, `BindHelp`) compose into per-TUI
KeyMaps.

```go
case uikit.KeyQ, uikit.KeyEsc:
    return m, tea.Quit
```

### Layout — `layout.go`, `compose.go`

Dimension constants (`MinUsableWidth`, `FallbackHeight`,
`PageChromeLines`, `OverlayMargin`, …), clamping helpers
(`ClampWidth`, `ClampHeight`, `ContentWidth`), and declarative
composition utilities:

```go
uikit.Spread(width, left, right)     // left + right in fixed width
uikit.Fit(text, width, lipgloss.Left) // truncate + pad
uikit.Center(width, text)
```

### Components

| Primitive | File | What it does |
|-----------|------|-------------|
| `Chrome` | `chrome.go` | Title / body / status sandwich — body gets leftover height |
| `Overlay` | `overlay.go` | Scrollable modal panel + compositing (`CenterOverlay`, `DimBackground`, `Compose`) |
| `OverlayBox` | `overlaybox.go` | Styled modal with title, body, and hint footer |
| `ListPicker` | `listpicker.go` | Pre-styled filterable list, one-line construction |
| `Grid2D` | `grid2d.go` | 2-D cursor with move, clamp, and wrap |
| `Screen` / `Router` | `screen.go` | Dispatch table — kills 4-way switch statements |
| `AsyncCmd` | `async.go` | Generic `tea.Cmd` → `Result[T]` |

**Chrome + Router** work together naturally:

```go
chrome := uikit.Chrome{
    Title:  func(w int) string { return m.router.Title(m.screen, m, w) },
    Status: func(w int) string { return m.router.Help(m.screen) },
    Body:   func(w, h int) string { return m.router.View(m.screen, m, w, h) },
}
return chrome.Render(m.width, m.height)
```

### Runtime — `run.go`, `drain.go`

`Run` / `RunModel` launch a Bubbletea program and drain stdin
afterwards (absorbs stale DECRQM terminal responses). `DrainStdin`
is also exported for callers that manage their own `tea.Program`.

## Testing

All component types are plain structs — test `Grid2D`, `Chrome`, layout
helpers without teatest:

```go
g := uikit.Grid2D{Col: 0, Row: -1}
g.Move(0, 1, 3, func(col int) int { return 5 })
assert(g.Row == 0)

out := uikit.Chrome{...}.Render(80, 24)
assert(lipgloss.Height(out) == 24)
```

For full TUI integration tests, components compose inside your Model
normally — existing teatest patterns work unchanged.

## Architecture

```
sci (cmd/sci)          ──┐
dbtui (cmd/dbtui)      ──┤── all import ──▶  internal/tui/uikit/
zot (cmd/zot)          ──┘

internal/ui/           ── CLI-specific layer (spinner, help, huh theme)
                          imports uikit for styles/palette

internal/tui/dbtui/ui/ ── dbtui-specific styles (own TUI singleton)
                          imports uikit for shared palette + layout

internal/tui/board/ui/ ── board-specific styles
                          imports uikit for shared palette
```
