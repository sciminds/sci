# Lipgloss Pitfalls — Complete Catalog

Every pitfall includes what goes wrong, why, and the fix.

## Measurement Pitfalls

### P1: Using `len()` for terminal width

**What goes wrong:** ANSI escape sequences inflate `len()`. CJK characters and emoji are 1 rune but 2 cells wide. Layout math breaks silently.

```go
// BAD
if len(rendered) > maxWidth { truncate... }

// GOOD
if lipgloss.Width(rendered) > maxWidth { truncate... }
```

**Rule:** Always use `lipgloss.Width()`, `lipgloss.Height()`, or `lipgloss.Size()` for any rendered string measurement.

### P2: Measuring unstyled text for styled layout

**What goes wrong:** You calculate width from raw text, then render with padding/borders. The rendered output is wider than expected.

```go
// BAD: measures raw text, ignores frame
w := lipgloss.Width(rawText)
style := lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.RoundedBorder())
result := style.Render(rawText)  // result is w + 4 cells wide

// GOOD: measure after render, or account for frame
finalWidth := lipgloss.Width(rawText) + style.GetHorizontalFrameSize()
```

### P3: Measuring before render vs after render

`lipgloss.Width()` on an unstyled string gives you text width. On a styled/rendered string it gives you visual width including padding, borders, margins. Be clear about which you need.

## Width & MaxWidth Pitfalls

### P4: Width includes borders (intentional but surprising)

When you set `Width(20)` on a bordered style, the content wraps at `20 - borderWidth`. This is by design — `Width` sets the *total* block width.

```go
style := lipgloss.NewStyle().Width(20).Border(lipgloss.RoundedBorder())
// Content area: 18 chars (20 - 2 border chars)
// Total rendered width: 20

// If you want 20 chars of content:
style := lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder())
style = style.Width(20 + style.GetHorizontalBorderSize())
```

### P5: MaxWidth truncates borders and margins

`MaxWidth` is applied last in the pipeline. If MaxWidth is too small, it will cut through your border characters, producing garbled output.

```go
// BAD: MaxWidth smaller than frame
style := lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    Padding(0, 2).
    MaxWidth(3)  // cuts through the border!

// GOOD: ensure MaxWidth >= frame size
minWidth := style.GetHorizontalFrameSize()
style = style.MaxWidth(max(desired, minWidth))
```

### P6: Width(0) doesn't mean "no width"

`Width(0)` forces the block to 0 width (everything truncated). Use `UnsetWidth()` to remove the width constraint entirely.

```go
// BAD: trying to remove width constraint
style = style.Width(0)  // everything disappears

// GOOD
style = style.UnsetWidth()
```

## Layout Pitfalls

### P7: JoinHorizontal pads all lines to equal width

After joining, every line in every block is padded with spaces to match the widest line in that block. This can cause unexpected background color bleed.

```go
// SURPRISE: right block gets space-padded, and those spaces
// inherit whatever background the terminal has
left := leftStyle.Render("short")
right := rightStyle.Render("very long content\nshort")
joined := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
// "short" in right block is padded to "very long content" width
```

**Fix:** If you need clean backgrounds, explicitly set `Width` on each block before joining.

### P8: Place is a no-op when content exceeds dimensions

`Place(10, 5, ...)` does nothing if content is already wider than 10 or taller than 5. It only adds whitespace, never truncates.

```go
// This does NOT truncate — content stays at whatever width it is
result := lipgloss.PlaceHorizontal(40, lipgloss.Center, wideContent)
// If wideContent is 60 chars, result is still 60 chars

// To actually constrain: use Width or MaxWidth first
constrained := lipgloss.NewStyle().MaxWidth(40).Render(wideContent)
result := lipgloss.PlaceHorizontal(40, lipgloss.Center, constrained)
```

### P9: JoinVertical pads to widest block

All lines become the width of the widest block. If you join a 20-char header with an 80-char body, the header line gets 60 trailing spaces.

**Fix:** Set explicit `Width` on all blocks before joining if you want controlled widths.

## Border Pitfalls

### P10: BorderStyle enables all sides by default

`BorderStyle(RoundedBorder())` renders all 4 sides. The moment you explicitly call `BorderTop(true)`, only explicitly-enabled sides render.

```go
// All 4 sides (implicit default)
style := lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder())

// ONLY top (others default to false once any side is explicit)
style := lipgloss.NewStyle().
    BorderStyle(lipgloss.RoundedBorder()).
    BorderTop(true)

// To be explicit about all 4:
style := lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder(), true, true, true, true)
```

### P11: Toggling borders changes block size

Adding/removing a border changes the block dimensions by the border width (typically 1 cell per side). This causes layout jitter.

**Fix:** Use `HiddenBorder()` for the "off" state to maintain consistent sizing.

### P12: Border colors use CSS shorthand

`BorderForeground(c1, c2, c3, c4)` is top, right, bottom, left (clockwise, like CSS). Not top, bottom, left, right.

## Style Pitfalls

### P13: Inherit doesn't copy padding or margins

`Inherit()` copies text formatting, colors, alignment, and borders — but NOT padding or margins. Set those explicitly.

```go
base := lipgloss.NewStyle().Padding(1, 2).Bold(true)
derived := lipgloss.NewStyle().Inherit(base)
// derived is Bold but has NO padding
```

### P14: Style is a value type — methods return new copies

Every setter returns a new `Style`. Forgetting to capture the return value is a silent no-op.

```go
// BAD: result discarded
style := lipgloss.NewStyle()
style.Bold(true)          // return value lost!
style.Padding(1, 2)       // return value lost!

// GOOD: chain or reassign
style := lipgloss.NewStyle().Bold(true).Padding(1, 2)
// or
style = style.Bold(true)
style = style.Padding(1, 2)
```

### P15: Render joins args with spaces

`style.Render("hello", "world")` produces `"hello world"` (space-joined). If you want newline-joined, pass a single string with `\n`.

```go
// Produces "line1 line2" (one line, space-separated)
style.Render("line1", "line2")

// Produces two lines
style.Render("line1\nline2")
// or
style.Render(strings.Join(lines, "\n"))
```

## Tab & Whitespace Pitfalls

### P16: Tabs are auto-converted to 4 spaces

Lipgloss converts tabs to spaces (default 4) before rendering. This can change column alignment if you're relying on tab stops.

```go
// Disable tab conversion
style.TabWidth(lipgloss.NoTabConversion)  // -1

// Remove tabs entirely
style.TabWidth(0)
```

### P17: Padding uses NBSP, margins use regular space

Padding characters are non-breaking spaces (preserved on copy/paste). Margin characters are regular spaces. This matters for copy/paste behavior.

## Output Pitfalls

### P18: fmt.Println doesn't downsample colors

If you print styled output through `fmt.Println`, colors aren't downsampled for terminals that only support 256 or 16 colors.

```go
// BAD: truecolor escapes on a 256-color terminal
fmt.Println(style.Render("hello"))

// GOOD: auto-downsamples for terminal capability
lipgloss.Println(style.Render("hello"))
```

## Compositing Pitfalls (v2)

### P19: Compositor.Refresh() after layer mutations

If you change layer `X`, `Y`, or `Z` after creating the compositor, you must call `Refresh()` to re-flatten the layer hierarchy.

### P20: Canvas coordinates are 0-indexed

`SetCell(0, 0, cell)` is the top-left corner. `SetCell(width, height, cell)` is out of bounds.

### P21: WrapWriter must be Close()d

`lipgloss.NewWrapWriter(buf)` tracks ANSI state. Failing to call `Close()` leaves dangling escape sequences.
