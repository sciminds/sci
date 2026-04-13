# Color System — Complete Reference

Lipgloss v2 has a rich color system with adaptive theming, gradients, and automatic terminal profile downsampling.

## Color Constructors

```go
lipgloss.Color("#FF5733")          // hex (most common)
lipgloss.Color("#F53")             // short hex
lipgloss.Color("201")              // ANSI 256 palette index
lipgloss.Color("5")                // ANSI 16 basic color
lipgloss.NoColor{}                 // absence of color (terminal default)
lipgloss.RGBColor{R: 255, G: 87, B: 51}  // direct RGB
```

## Named ANSI Constants

16 basic terminal colors:

```go
lipgloss.Black         // 0
lipgloss.Red           // 1
lipgloss.Green         // 2
lipgloss.Yellow        // 3
lipgloss.Blue          // 4
lipgloss.Magenta       // 5
lipgloss.Cyan          // 6
lipgloss.White         // 7
lipgloss.BrightBlack   // 8
lipgloss.BrightRed     // 9
lipgloss.BrightGreen   // 10
lipgloss.BrightYellow  // 11
lipgloss.BrightBlue    // 12
lipgloss.BrightMagenta // 13
lipgloss.BrightCyan    // 14
lipgloss.BrightWhite   // 15
```

## Applying Colors

```go
style.Foreground(lipgloss.Color("#FFF"))      // text color
style.Background(lipgloss.Color("#1a1a2e"))   // background color
style.MarginBackground(lipgloss.Color("#333")) // margin area background
```

## Adaptive Colors (Light/Dark Mode)

Detect terminal background and pick appropriate colors:

```go
isDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
ld := lipgloss.LightDark(isDark)

// ld(lightModeColor, darkModeColor)
fg := ld(lipgloss.Color("#333"), lipgloss.Color("#ccc"))
bg := ld(lipgloss.Color("#fff"), lipgloss.Color("#1a1a2e"))

style := lipgloss.NewStyle().
    Foreground(fg).
    Background(bg)
```

`HasDarkBackground` queries the terminal for its background color. Defaults to `true` on error (safe default — most terminals are dark).

For raw access:
```go
bgColor, err := lipgloss.BackgroundColor(os.Stdin, os.Stdout)
```

## Color Profile Adaptation

Automatically pick colors appropriate for the terminal's color capability:

```go
complete := lipgloss.Complete(profile)

// complete(ansi16Color, ansi256Color, trueColor)
fg := complete(
    lipgloss.Color("5"),           // 16-color terminal
    lipgloss.Color("201"),         // 256-color terminal
    lipgloss.Color("#FF00FF"),     // truecolor terminal
)
```

## Color Manipulation

```go
lipgloss.Darken(color, 0.2)         // darken by 20%
lipgloss.Lighten(color, 0.3)        // lighten by 30%
lipgloss.Alpha(color, 0.5)          // set alpha (0=transparent, 1=opaque)
lipgloss.Complementary(color)       // 180° hue rotation
```

## Gradients

### 1D Gradient (Linear)

Blends in CIELAB color space for perceptually uniform transitions.

```go
colors := lipgloss.Blend1D(10, startColor, midColor, endColor)
// Returns []color.Color with 10 stops

// Example: gradient text
text := "Hello, World!"
colors := lipgloss.Blend1D(len([]rune(text)), lipgloss.Color("#FF0000"), lipgloss.Color("#0000FF"))
var result strings.Builder
for i, ch := range text {
    s := lipgloss.NewStyle().Foreground(colors[i])
    result.WriteString(s.Render(string(ch)))
}
```

### 2D Gradient (Rotated)

```go
colors := lipgloss.Blend2D(width, height, angle, stops...)
// angle: 0=left→right, 90=top→bottom, 45=diagonal
// Returns row-major: [row0col0, row0col1, ..., row1col0, ...]

// Access: colors[y*width + x]
```

### Border Gradients

```go
style.BorderForegroundBlend(startColor, midColor, endColor)  // min 2 stops
style.BorderForegroundBlendOffset(5)                          // rotate start position
```

## Auto-Downsampling Output

Use lipgloss print functions instead of `fmt` to auto-downsample colors for the terminal:

```go
// These auto-detect terminal capability and downsample
lipgloss.Print(styled)
lipgloss.Println(styled)
lipgloss.Printf("Result: %s", styled)

// Write to specific writer
lipgloss.Fprint(w, styled)
lipgloss.Fprintln(w, styled)

// Return downsampled string (uses stdout's profile)
s := lipgloss.Sprint(styled)
```

`lipgloss.Writer` is the default `colorprofile.Writer` targeting stdout.

## Decision Tree: Which Color Type?

```
Simple static color?
  → lipgloss.Color("#hex") or lipgloss.Color("ansi256")

Need light/dark mode support?
  → lipgloss.LightDark(isDark)(lightColor, darkColor)

Need to support very old terminals?
  → lipgloss.Complete(profile)(ansi16, ansi256, truecolor)

Gradient effect?
  → lipgloss.Blend1D for linear, Blend2D for 2D
  → BorderForegroundBlend for border gradients

Programmatic color adjustment?
  → lipgloss.Darken / Lighten / Alpha / Complementary

No color (terminal default)?
  → lipgloss.NoColor{}
```
