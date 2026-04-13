---
name: lipgloss
description: Style, measure, and lay out terminal UI content with charmbracelet/lipgloss v2. Covers the render pipeline, layout composition (Join/Place), sizing discipline (Width vs MaxWidth, frame-size accounting), borders, alignment, color system, table/tree/list sub-packages, canvas/compositor, and common measurement pitfalls. Use whenever styling TUI output, debugging layout/overflow bugs, building tables or trees, or compositing layered content.
---

# Lipgloss v2 — Terminal Styling & Layout

Lipgloss turns strings into styled, measured, composited terminal blocks. Everything is a **string in, string out** pipeline — no widget tree, no retained state. Style is a value type: every setter returns a new copy.

```go
import "charm.land/lipgloss/v2"
```

## When to Use This Skill

- Styling or rendering any TUI output (colors, borders, padding)
- Building layouts with `JoinHorizontal` / `JoinVertical` / `Place`
- Debugging alignment, overflow, or sizing bugs
- Measuring rendered content width/height
- Creating tables, trees, or lists with the sub-packages
- Compositing layered content with Canvas/Compositor
- Choosing between `Width` vs `MaxWidth`, padding vs margin, `Place` vs `Join`

## Core Mental Model: The Render Pipeline

When you call `style.Render(text)`, lipgloss applies rules in this exact order:

```
text
 │
 ├── 1. Transform function (strings.ToUpper, etc.)
 ├── 2. Tab → spaces conversion
 ├── 3. Strip newlines (if Inline)
 ├── 4. Word-wrap to Width (minus horizontal padding)
 ├── 5. ANSI formatting (bold, italic, colors)
 ├── 6. Padding (inside border)
 ├── 7. Vertical alignment (pad to Height)
 ├── 8. Horizontal alignment (pad to Width)
 ├── 9. Borders
 ├── 10. Margins (outside border)
 ├── 11. MaxWidth truncation (post-render hard clip)
 └── 12. MaxHeight truncation (post-render hard clip)
```

**Key insight:** `Width` wraps text *before* borders. `MaxWidth` truncates *after* everything. They are not interchangeable.

## Sizing Discipline

This is the single most important section. Most lipgloss layout bugs come from incorrect size accounting.

### Width vs MaxWidth

| Property | When applied | What it does | Includes borders? |
|---|---|---|---|
| `Width(n)` | Step 4+8 | Wraps text, then pads all lines to exactly `n` cells | Yes — borders are subtracted internally before wrapping |
| `MaxWidth(n)` | Step 11 | Hard-truncates the final rendered output | Yes — applied to the complete output |

**Rule:** Use `Width` for layout sizing. Use `MaxWidth` only as a safety clip.

### Height vs MaxHeight

Same relationship: `Height` pads content vertically to fill; `MaxHeight` truncates the final output.

### Frame Size — The Essential Calculation

Every style has a "frame" — the combined size of borders + padding + margins. Use it to calculate content width:

```go
contentWidth := availableWidth - style.GetHorizontalFrameSize()
contentHeight := availableHeight - style.GetVerticalFrameSize()
```

Available getters for fine-grained control:

| Method | Returns |
|---|---|
| `GetFrameSize()` | `(horizontal, vertical int)` |
| `GetHorizontalFrameSize()` | borders + padding + margins (left+right) |
| `GetVerticalFrameSize()` | borders + padding + margins (top+bottom) |
| `GetHorizontalPadding()` | left + right padding |
| `GetVerticalPadding()` | top + bottom padding |
| `GetHorizontalMargins()` | left + right margins |
| `GetVerticalMargins()` | top + bottom margins |
| `GetHorizontalBorderSize()` | left + right border widths |
| `GetVerticalBorderSize()` | top + bottom border widths |

### Measuring Rendered Content

**Never use `len(s)`** for terminal width. ANSI escapes are invisible; CJK/emoji are 2 cells wide.

```go
w := lipgloss.Width(rendered)   // widest line in cells (ANSI-aware)
h := lipgloss.Height(rendered)  // line count
w, h := lipgloss.Size(rendered) // both
```

### Common Sizing Mistake

```go
// BAD: Width(20) with a border means 18 chars of content, not 20
style := lipgloss.NewStyle().Width(20).Border(lipgloss.RoundedBorder())

// GOOD: Set width to account for what you actually want
contentWidth := 20
style := lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    Width(contentWidth + style.GetHorizontalBorderSize())

// BETTER: Calculate from available space
style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
style = style.Width(availableWidth) // lipgloss handles border subtraction internally
```

## Layout Composition

### JoinHorizontal — Side-by-Side Blocks

```go
result := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
//                                 ^^^ vertical alignment of shorter blocks
```

Position controls how shorter blocks align vertically:
- `Top` (0.0) — align to top
- `Center` (0.5) — center vertically
- `Bottom` (1.0) — align to bottom
- Fractional values work: `0.2` = 20% from top

**Behavior:** All blocks are padded to equal height. All lines padded to equal width with spaces.

**Pitfall:** The space-padding can bleed background colors. If blocks have different backgrounds, render each with explicit `Width` first.

### JoinVertical — Stacked Blocks

```go
result := lipgloss.JoinVertical(lipgloss.Left, top, bottom)
//                               ^^^ horizontal alignment of narrower blocks
```

Position controls how narrower blocks align horizontally:
- `Left` (0.0), `Center` (0.5), `Right` (1.0)

**Behavior:** All lines padded to the width of the widest block.

### Place — Position in a Whitespace Box

```go
// Center text in a 80x24 box
result := lipgloss.Place(80, 24, lipgloss.Center, lipgloss.Center, content)

// With styled whitespace fill
result := lipgloss.Place(80, 24, lipgloss.Center, lipgloss.Center, content,
    lipgloss.WithWhitespaceStyle(dimStyle),
    lipgloss.WithWhitespaceChars("·"),
)
```

**`PlaceHorizontal`** — horizontal-only placement (no height change):
```go
result := lipgloss.PlaceHorizontal(80, lipgloss.Center, content)
```

**`PlaceVertical`** — vertical-only placement (no width change):
```go
result := lipgloss.PlaceVertical(24, lipgloss.Center, content)
```

**Pitfall:** Place is a no-op when content already exceeds the given dimensions. It only adds whitespace, never truncates.

### Decision Tree: Which Layout Function?

```
Two rendered blocks, side by side?
  → JoinHorizontal(verticalAlignment, left, right)

Two rendered blocks, stacked?
  → JoinVertical(horizontalAlignment, top, bottom)

Center/position content in available space?
  → Place(w, h, hPos, vPos, content)
  → PlaceHorizontal(w, hPos, content)  (height unchanged)
  → PlaceVertical(h, vPos, content)    (width unchanged)

Right-align something in a fixed width?
  → PlaceHorizontal(width, lipgloss.Right, content)
  → OR: style.Width(width).Align(lipgloss.Right).Render(content)

Left + right in a status bar?
  → PlaceHorizontal(totalWidth, lipgloss.Left, left + gap + right)
  → OR: render left and right with explicit widths, JoinHorizontal
```

## Padding & Margins

### Padding (inside border)

Uses NBSP by default (preserved on copy/paste). CSS shorthand:

```go
style.Padding(1)           // all sides
style.Padding(1, 2)        // vertical=1, horizontal=2
style.Padding(1, 2, 3)     // top=1, horizontal=2, bottom=3
style.Padding(1, 2, 3, 4)  // top, right, bottom, left (clockwise)

// Individual sides
style.PaddingTop(1).PaddingRight(2).PaddingBottom(1).PaddingLeft(2)

// Custom fill character
style.PaddingChar('·')
```

### Margins (outside border)

Uses regular space. Same CSS shorthand as padding:

```go
style.Margin(1, 2)             // vertical=1, horizontal=2
style.MarginBackground(color)  // background color for margin area
style.MarginChar('·')          // custom fill character
```

**Key difference:** Padding is inside the border, margins are outside. Margins are NOT inherited via `Inherit()`.

## Borders

### Predefined Borders

| Constructor | Visual |
|---|---|
| `RoundedBorder()` | Rounded corners (most common) |
| `NormalBorder()` | Standard 90-degree corners |
| `ThickBorder()` | Heavy/bold lines |
| `DoubleBorder()` | Double-stroke lines |
| `HiddenBorder()` | Invisible (spaces) — preserves layout sizing |
| `BlockBorder()` | Full block characters |
| `OuterHalfBlockBorder()` | Half-block, outer |
| `InnerHalfBlockBorder()` | Half-block, inner |
| `ASCIIBorder()` | `+--+` ASCII |
| `MarkdownBorder()` | Pipe/dash markdown style |

### Border Methods

```go
// Set border style + which sides (CSS shorthand for bools)
style.Border(lipgloss.RoundedBorder())              // all 4 sides
style.Border(lipgloss.RoundedBorder(), true, false)  // top+bottom only

// Toggle individual sides
style.BorderTop(true).BorderBottom(true).BorderLeft(false).BorderRight(false)

// Color borders
style.BorderForeground(lipgloss.Color("#7D56F4"))           // all sides
style.BorderForeground(topColor, rightColor, bottomColor, leftColor) // CSS shorthand

// Gradient borders (v2)
style.BorderForegroundBlend(startColor, endColor)           // min 2 stops
style.BorderForegroundBlendOffset(5)                        // rotate gradient start
```

### HiddenBorder for Layout Stability

Use `HiddenBorder()` when toggling between bordered/unbordered states to maintain consistent sizing:

```go
var border lipgloss.Border
if selected {
    border = lipgloss.RoundedBorder()
} else {
    border = lipgloss.HiddenBorder() // same size, invisible
}
style := lipgloss.NewStyle().Border(border)
```

### Border Side Default Behavior

**Pitfall:** `BorderStyle(RoundedBorder())` alone renders ALL 4 sides. The moment you explicitly set ANY side (e.g., `BorderTop(true)`), only explicitly-enabled sides render — others default to false.

## Alignment

```go
style.Align(lipgloss.Center)                      // horizontal only
style.Align(lipgloss.Center, lipgloss.Center)      // horizontal + vertical
style.AlignHorizontal(lipgloss.Right)
style.AlignVertical(lipgloss.Bottom)
```

Alignment only takes visible effect when `Width` is set (horizontal) or `Height` is set (vertical), or when there are multiple lines of different lengths.

`Position` is a float: `Left`/`Top` = 0.0, `Center` = 0.5, `Right`/`Bottom` = 1.0. Fractional values are valid.

## Colors (v2)

### Constructors

```go
lipgloss.Color("#FF5733")   // hex
lipgloss.Color("201")       // ANSI 256
lipgloss.Color("5")         // ANSI 16
lipgloss.NoColor{}          // no color (terminal default)
lipgloss.RGBColor{R: 255, G: 87, B: 51}

// Named ANSI constants
lipgloss.Red, lipgloss.Green, lipgloss.Blue  // ... through BrightWhite (16 total)
```

### Adaptive Colors (light/dark terminal)

```go
isDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
ld := lipgloss.LightDark(isDark)

fg := ld(lipgloss.Color("#333"), lipgloss.Color("#ccc"))  // light, dark
style := lipgloss.NewStyle().Foreground(fg)
```

### Color Manipulation (v2)

```go
lipgloss.Darken(color, 0.2)        // darken by 20%
lipgloss.Lighten(color, 0.2)       // lighten by 20%
lipgloss.Alpha(color, 0.5)         // set alpha
lipgloss.Complementary(color)      // 180-degree hue rotation
```

### Gradients (v2)

```go
// 1D gradient (e.g., for text coloring)
colors := lipgloss.Blend1D(10, startColor, midColor, endColor)

// 2D gradient (e.g., for canvas backgrounds)
colors := lipgloss.Blend2D(width, height, 45.0, topLeft, topRight, bottomLeft, bottomRight)
// Returns row-major order: [row1..., row2..., ...]
```

## Style Inheritance

```go
base := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF")).Bold(true)
derived := lipgloss.NewStyle().Italic(true).Inherit(base)
// derived is bold + italic + white foreground
```

`Inherit` copies rules from `base` that aren't already set on `derived`. **Margins and padding are NOT inherited.** Background color IS inherited to margin background if neither style has explicitly set one.

## Text Formatting

```go
style.Bold(true)
style.Italic(true)
style.Underline(true)                          // shorthand for UnderlineSingle
style.UnderlineStyle(lipgloss.UnderlineCurly)  // Single, Double, Curly, Dotted, Dashed
style.UnderlineColor(lipgloss.Color("#F00"))
style.Strikethrough(true)
style.Faint(true)
style.Reverse(true)
style.Blink(true)

// Hyperlinks (v2, terminal support varies)
style.Hyperlink("https://example.com")

// Transform function (applied at render time)
style.Transform(strings.ToUpper)

// Control space formatting
style.UnderlineSpaces(false)       // don't underline whitespace
style.StrikethroughSpaces(false)   // don't strike whitespace
```

## ANSI-Aware Text Wrapping

```go
// Wrap text to width, preserving ANSI styles across line breaks
wrapped := lipgloss.Wrap(styledText, 60, "")
// Third arg: additional breakpoint characters (empty = default word boundaries)

// Streaming wrapper (for writers)
w := lipgloss.NewWrapWriter(buf)
defer w.Close()  // MUST close to reset ANSI state
io.WriteString(w, styledText)
```

## Common Mistakes

See `references/pitfalls.md` for the complete list with fixes.

### 1. Using `len()` instead of `lipgloss.Width()`

```go
// BAD: len counts bytes, not terminal cells
if len(rendered) > maxWidth { ... }

// GOOD: Width is ANSI-aware and handles wide chars
if lipgloss.Width(rendered) > maxWidth { ... }
```

### 2. Confusing Width and MaxWidth

```go
// BAD: MaxWidth as layout tool — causes hard truncation of borders
style.MaxWidth(40).Border(lipgloss.RoundedBorder())

// GOOD: Width for layout, MaxWidth only as safety clip
style.Width(40).Border(lipgloss.RoundedBorder())
```

### 3. Forgetting frame size in width calculations

```go
// BAD: content gets squeezed by border + padding
panel := lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    Padding(0, 1).
    Width(availableWidth)
// Content area is actually availableWidth - 4 (2 border + 2 padding)
// But this is correct IF you want the panel to BE availableWidth wide

// BAD: trying to make content exactly N chars wide
content := renderContent(availableWidth) // renders at full width
panel.Render(content)                     // panel is wider than availableWidth!

// GOOD: subtract frame, render content to fit
contentWidth := availableWidth - panel.GetHorizontalFrameSize()
content := renderContent(contentWidth)
panel.Render(content)
```

### 4. Manual gap calculation instead of Place

```go
// BAD: manual string math for right-alignment
gap := width - lipgloss.Width(left) - lipgloss.Width(right)
result := left + strings.Repeat(" ", max(0, gap)) + right

// GOOD: let lipgloss handle it
leftRendered := leftStyle.Width(lipgloss.Width(left)).Render(left)
rightRendered := rightStyle.Width(lipgloss.Width(right)).Render(right)
// Option A: PlaceHorizontal
result := lipgloss.PlaceHorizontal(width, lipgloss.Left,
    lipgloss.JoinHorizontal(lipgloss.Top, leftRendered,
        lipgloss.PlaceHorizontal(width-lipgloss.Width(left), lipgloss.Right, right)))
// Option B: Width + Align for status bars
result := lipgloss.NewStyle().Width(width).Render(
    lipgloss.JoinHorizontal(lipgloss.Top, left,
        lipgloss.NewStyle().
            Width(width-lipgloss.Width(left)).
            Align(lipgloss.Right).
            Render(right)))
```

### 5. Setting Height on bordered styles

```go
// BAD: Height + border can cause misaligned panels
style := lipgloss.NewStyle().Border(border).Height(h)

// GOOD: fill content to exact line count, let border wrap naturally
for len(lines) < innerHeight {
    lines = append(lines, "")
}
style := lipgloss.NewStyle().Border(border)
result := style.Render(strings.Join(lines, "\n"))
```

### 6. Inline style construction outside ui/ package

Per project convention, never construct styles inline. Access via the `ui.TUI` singleton or the per-TUI `ui/styles.go`.

## Reference Files

- `references/pitfalls.md` — Complete pitfall catalog with before/after fixes
- `references/table-tree-list.md` — Table, Tree, and List sub-package API reference
- `references/canvas-compositor.md` — Canvas, Layer, and Compositor API for composited layouts
- `references/color-system.md` — Full color constructor catalog, adaptive colors, gradients, blending
