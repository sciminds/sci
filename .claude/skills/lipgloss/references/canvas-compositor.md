# Canvas & Compositor — Composited Layouts (v2)

Lipgloss v2 introduces a cell-buffer compositing system for overlapping, layered, and positioned content. Use when `JoinHorizontal`/`JoinVertical` aren't enough — overlays, floating panels, z-ordered content.

## When to Use

- Overlapping content (modals, popups, floating panels)
- Z-ordered rendering (content behind/in front of other content)
- Hit testing (which layer was clicked?)
- Complex multi-layer compositions where Join/Place would be unwieldy

## Canvas

A 2D cell buffer. Think of it as a terminal-sized bitmap.

```go
canvas := lipgloss.NewCanvas(80, 24)
canvas.Clear()

// Render styled content onto canvas via compositor
canvas.Compose(compositor)

// Output final string
output := canvas.Render()
```

### Canvas API

| Method | Signature | Description |
|---|---|---|
| `NewCanvas` | `(width, height int) *Canvas` | Create canvas |
| `Resize` | `(width, height int)` | Resize (clears content) |
| `Clear` | `()` | Clear all cells |
| `Compose` | `(uv.Drawable) *Canvas` | Draw a layer/compositor |
| `Render` | `() string` | Output styled string |
| `Width` / `Height` | `() int` | Dimensions |
| `CellAt` | `(x, y int) *uv.Cell` | Read cell at position |
| `SetCell` | `(x, y int, *uv.Cell)` | Write cell at position |

**Note:** Coordinates are 0-indexed. `(0,0)` is top-left.

## Layer

Positioned content with z-ordering and nesting.

```go
// Create a layer from rendered content
panel := lipgloss.NewLayer(
    panelStyle.Render("Hello, World!"),
).X(10).Y(5).Z(1)

// Layers can have IDs for hit testing
panel = panel.ID("main-panel")

// Nest child layers. X/Y are offsets from the PARENT's origin, so this
// overlay lands at (10+5, 5+2) = (15, 7) once `panel` is its parent.
overlay := lipgloss.NewLayer(
    modalStyle.Render("Are you sure?"),
).X(5).Y(2).Z(2)

panel.AddLayers(overlay)

parent := lipgloss.NewLayer(bgContent).
    AddLayers(panel)
```

### Layer API

| Method | Signature | Description |
|---|---|---|
| `NewLayer` | `(content string, layers ...*Layer) *Layer` | Create with content and optional children |
| `X` | `(int) *Layer` | Set X offset from the parent's origin |
| `Y` | `(int) *Layer` | Set Y offset from the parent's origin |
| `Z` | `(int) *Layer` | Set Z-index (higher = on top; compared globally after flattening, not per-parent) |
| `ID` | `(string) *Layer` | Set ID for hit testing |
| `AddLayers` | `(...*Layer) *Layer` | Add child layers |
| `GetLayer` | `(id string) *Layer` | Find descendant by ID |
| `GetX` / `GetY` / `GetZ` | `() int` | Read back the position |
| `GetID` / `GetContent` | `() string` | Read back ID / content |
| `Width` / `Height` | `() int` | Measured size (set when children are added) |
| `MaxZ` | `() int` | Highest Z in this subtree |

## Compositor

Flattens the layer hierarchy, sorts by z-index, and renders to a canvas.

```go
comp := lipgloss.NewCompositor(
    bgLayer,
    panelLayer,
    overlayLayer,
)

// Render directly (creates temporary canvas)
output := comp.Render()

// Or render to existing canvas
canvas := lipgloss.NewCanvas(80, 24)
comp.Draw(canvas, image.Rect(0, 0, 80, 24))
output := canvas.Render()
```

### Hit Testing

`LayerHit`'s accessors are methods, not fields:

```go
hit := comp.Hit(mouseX, mouseY)
if !hit.Empty() {
    fmt.Printf("Clicked on: %s at %v\n", hit.ID(), hit.Bounds())
    layer := hit.Layer() // *lipgloss.Layer, or nil when Empty()
}
```

Only layers with IDs are hit-testable. Returns the topmost (highest Z) named layer at the given coordinates.

### Compositor API

| Method | Signature | Description |
|---|---|---|
| `NewCompositor` | `(...*Layer) *Compositor` | Create with layers |
| `AddLayers` | `(...*Layer) *Compositor` | Add more layers |
| `Draw` | `(uv.Screen, image.Rectangle)` | Draw to screen/canvas |
| `Hit` | `(x, y int) LayerHit` | Hit-test at coordinates |
| `GetLayer` | `(id string) *Layer` | Find layer by ID |
| `Bounds` | `() image.Rectangle` | Union of every flattened layer's bounds |
| `Render` | `() string` | Convenience: canvas + render |
| `Refresh` | `()` | Re-flatten after modifying layer positions |

## Complete Example: Modal Over Content

```go
// Background content
bg := lipgloss.NewLayer(
    lipgloss.NewStyle().
        Width(80).Height(24).
        Background(lipgloss.Color("#1a1a2e")).
        Render("Main application content here..."),
).Z(0)

// Modal overlay
modalContent := lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color("#e94560")).
    Padding(1, 2).
    Width(40).
    Render("Are you sure you want to delete?\n\n[Yes]  [No]")

modal := lipgloss.NewLayer(modalContent).
    X(20).Y(8).Z(10).
    ID("confirm-modal")

// Compose
comp := lipgloss.NewCompositor(bg, modal)
output := comp.Render()
```

## Key Rules

1. **Refresh after mutations.** If you change layer X/Y/Z after creating the compositor, call `comp.Refresh()` — it re-flattens and re-sorts.
2. **Z-index determines paint order.** Higher Z paints over lower Z, compared across the whole flattened tree (a child's Z is not added to its parent's). Ties are sorted with `slices.SortFunc`, which is *not* stable — don't rely on insertion order between equal-Z layers; give them distinct Z values instead.
3. **Layers don't clip.** Content extending beyond the canvas boundary is simply not drawn.
4. **Hit returns topmost.** `Hit()` returns the highest-Z named layer at coordinates; check `hit.Empty()` before using it.
5. **Coordinates nest.** A child layer's X/Y are offsets from its parent's origin, and the compositor sums them down the tree into absolute positions. Top-level layers are therefore positioned relative to the compositor's `(0,0)`.
