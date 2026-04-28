# Effects & Animation in Bubble Tea v2

Recipes for animation loops, color cycling, spring physics, layered compositing, and a handful of "showpiece" effects (rainbow, wave, metaballs). All examples assume **Bubble Tea v2** (`charm.land/bubbletea/v2`) and **Lip Gloss v2** (`charm.land/lipgloss/v2`) — the only versions allowed by `CLAUDE.md`.

> Bubble Tea has no built-in `FrameMsg` or game-loop. Animation is just a `tea.Tick` (or `tea.Every`) that schedules a custom message, and you re-arm it each tick to keep ticking. Everything in this file is built from that one primitive.

---

## 1. The animation primitive: `tea.Tick`

`tea.Tick` returns a `Cmd` that fires once after a duration, producing whatever message your callback returns. To loop, return another `tea.Tick` from `Update` when you receive the message.

```go
import (
    "time"
    tea "charm.land/bubbletea/v2"
)

type frameMsg time.Time

const fps = 60

func tick() tea.Cmd {
    return tea.Tick(time.Second/fps, func(t time.Time) tea.Msg {
        return frameMsg(t)
    })
}

func (m model) Init() tea.Cmd { return tick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg.(type) {
    case frameMsg:
        m.advance()         // mutate state for one frame
        return m, tick()    // re-arm for the next frame
    }
    return m, nil
}
```

### `Tick` vs `Every`

| | `tea.Tick(d, fn)` | `tea.Every(d, fn)` |
|---|---|---|
| Aligns with system clock | No — fires `d` after invocation | Yes — fires when wall-clock crosses next multiple of `d` |
| Use for | Animation frames | Wall-clock sync (pulse on the second, hourly refresh) |
| Drift over many frames | Accumulates if `Update` is slow | Self-correcting |

Animation always wants `Tick` — clock-aligned ticks coalesce after a missed frame and look jankier than just running 1 frame late.

### Stopping an animation

There's no "cancel" for an in-flight `tea.Tick`. To stop, **stop returning the next `tick()`**. The pending tick will arrive once more; ignore it (or gate it on a flag):

```go
case frameMsg:
    if !m.animating {
        return m, nil      // swallow the trailing tick
    }
    m.advance()
    return m, tick()
```

### Animation gotchas

1. **Don't fire two `tea.Tick`s on the same `frameMsg` type.** Each becomes an independent loop and they multiply on every frame. If you need parallel animations, use distinct message types (e.g. `cursorBlinkMsg`, `spinnerMsg`).
2. **Don't put `time.Sleep` in `Update`.** It blocks the entire program — the runtime is single-goroutine for `Update`. Use `tea.Tick` instead.
3. **Re-arm the tick from `Update`, not from inside the callback.** The callback runs in a goroutine and returning a `tea.Cmd` from there does nothing.
4. **Cap your FPS.** 60 FPS is plenty; at 120+ you're paying for redraws nobody can see, and `Update` may not finish in 8ms on a slow terminal.

---

## 2. FPS, throttling, and dirty-frame skipping

A 60 FPS tick fires every 16.6ms. If your `Update` + `View` round-trip ever exceeds that, `tea.Tick` queues up and the program plays catch-up. Two fixes:

### Skip frames that didn't change

```go
type model struct {
    state animState
    dirty bool
}

case frameMsg:
    next := m.state.advance()
    if next == m.state {
        return m, tick()         // no visual change — re-arm but don't redraw
    }
    m.state = next
    m.dirty = true
    return m, tick()

func (m model) View() string {
    if !m.dirty {
        return m.lastFrame       // reuse cached output
    }
    m.lastFrame = m.render()
    m.dirty = false
    return m.lastFrame
}
```

Bubble Tea de-duplicates identical `View()` output internally, but caching the rendered string still saves the CPU of re-rendering.

### Adaptive FPS

If the user is idle, drop to 10 FPS; while they're interacting, run at 60.

```go
const (
    fpsActive = 60
    fpsIdle   = 10
)

func (m model) tickInterval() time.Duration {
    if time.Since(m.lastInput) > time.Second {
        return time.Second / fpsIdle
    }
    return time.Second / fpsActive
}
```

---

## 3. Color cycling (rainbows, pulses, gradients)

In v2, `lipgloss.Color` returns `image/color.Color`. That means you can synthesize colors at runtime with any function returning the same — including HSL→RGB conversion, which is the right tool for rainbow text.

### Rainbow text via HSL

```go
import (
    "image/color"
    "math"
    "charm.land/lipgloss/v2"
)

// hslToRGB converts HSL (hue 0-1, sat 0-1, light 0-1) to image/color.RGBA.
func hslToRGB(h, s, l float64) color.Color {
    if s == 0 {
        c := uint8(l * 255)
        return color.RGBA{c, c, c, 255}
    }
    var q float64
    if l < 0.5 {
        q = l * (1 + s)
    } else {
        q = l + s - l*s
    }
    p := 2*l - q
    hue2rgb := func(t float64) float64 {
        t = math.Mod(t+1, 1)
        switch {
        case t < 1.0/6: return p + (q-p)*6*t
        case t < 0.5:   return q
        case t < 2.0/3: return p + (q-p)*(2.0/3-t)*6
        default:        return p
        }
    }
    return color.RGBA{
        R: uint8(hue2rgb(h+1.0/3) * 255),
        G: uint8(hue2rgb(h) * 255),
        B: uint8(hue2rgb(h-1.0/3) * 255),
        A: 255,
    }
}

// rainbowText applies a hue gradient across the runes of s, offset by phase ∈ [0,1).
func rainbowText(s string, phase float64) string {
    runes := []rune(s)
    var b strings.Builder
    for i, r := range runes {
        hue := math.Mod(phase+float64(i)/float64(len(runes)), 1)
        st := lipgloss.NewStyle().Foreground(hslToRGB(hue, 0.7, 0.6))
        b.WriteString(st.Render(string(r)))
    }
    return b.String()
}
```

In the model, `phase` is just a counter:

```go
type model struct{ phase float64 }

case frameMsg:
    m.phase = math.Mod(m.phase + 0.01, 1)
    return m, tick()

func (m model) View() string {
    return rainbowText("RAINBOW TEXT", m.phase)
}
```

> `rainbowText` allocates one `Style` per rune per frame. For long strings, pre-build a slice of styles and index into it. For one-line headers it's fine.

### Two-color pulse

Lerp between two colors instead of going around the hue wheel:

```go
func lerpColor(a, b color.RGBA, t float64) color.RGBA {
    return color.RGBA{
        R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
        G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
        B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
        A: 255,
    }
}

// Pulse between two colors using a triangle wave.
t := math.Abs(math.Sin(m.phase * math.Pi))
c := lerpColor(coolBlue, hotPink, t)
```

### Use Lip Gloss's built-in blend when you can

v2 ships `BorderForegroundBlend` for borders and `progress.WithColors(...)` for progress bars — both interpolate gradients across the rendered shape without you computing per-cell colors. Reach for these before hand-rolling.

---

## 4. Spring physics with Harmonica

`charmbracelet/harmonica` gives you smooth, natural motion (slide-in panels, bouncy cursors, settling values) without writing easing functions. It's the same library Bubbles' `progress` uses internally.

```go
import "github.com/charmbracelet/harmonica"

const (
    fps       = 60
    frequency = 7.0   // angular frequency — higher = faster
    damping   = 0.15  // < 1 bouncy, 1 critical, > 1 sluggish
)

type model struct {
    spring     harmonica.Spring
    pos, vel   float64
    target     float64
}

func newModel() model {
    return model{
        spring: harmonica.NewSpring(harmonica.FPS(fps), frequency, damping),
    }
}

case frameMsg:
    m.pos, m.vel = m.spring.Update(m.pos, m.vel, m.target)
    if math.Abs(m.pos-m.target) < 0.01 && math.Abs(m.vel) < 0.01 {
        return m, nil          // settled — stop animating
    }
    return m, tick()
```

Damping cheat sheet:

| Damping | Behavior | Use for |
|---|---|---|
| `0.15` | Bouncy, oscillates 3–5 times | Playful UI (cursor, attention pull) |
| `0.5` | Slight bounce, settles fast | Panel slide-in |
| `1.0` | Critically damped, fastest stop without overshoot | Scroll snap, value settle |
| `1.5` | Sluggish, monotonic | Loading indicators that ease in |

---

## 5. Layered rendering: `lipgloss.NewLayer` + `NewCompositor`

v2 introduced a cell-based compositor — exactly what you want for "thing on top of thing" effects (modal over UI, particle over background, popover, ASCII sprite). It handles ANSI styles, transparency (whitespace passes through), and Z-ordering for you.

```go
import "charm.land/lipgloss/v2"

func (m model) View() string {
    background := m.renderBackground()        // your normal UI
    sprite     := m.renderSprite()            // small block of text

    layers := []*lipgloss.Layer{
        lipgloss.NewLayer(background),
        lipgloss.NewLayer(sprite).
            X(m.spriteX).
            Y(m.spriteY).
            Z(1),                              // above background
    }

    return lipgloss.NewCompositor(layers...).Render()
}
```

### Nesting layers

A parent layer can hold children that are positioned **relative to the parent's origin**:

```go
panel := lipgloss.NewLayer(panelText).X(5).Y(2)
panel.AddLayers(
    lipgloss.NewLayer(badge).X(10).Y(0).Z(1),  // badge sits at (15, 2) globally
)
```

Use this for "panel + decorations on the panel" — you can move the panel and the decorations follow.

### Why use the compositor (instead of building strings manually)

- **ANSI-aware:** layers preserve foreground/background/style attributes from the underlying content; you don't have to strip and re-style by hand.
- **Whitespace is transparent:** spaces in an upper layer let lower layers show through, so you can build sprite-like cutouts naturally.
- **Sub-cell-accurate placement:** `X`/`Y` are character cells, but the compositor accounts for double-width runes and ANSI escapes during measurement.

### Compositor gotchas

- `NewCompositor` is the v2 entry point. Older posts may say `NewCanvas` — both have appeared during the v2 beta; check what your pinned `lipgloss/v2` version exports and use that.
- Layers are stored as pointers (`*lipgloss.Layer`). Mutating one after adding it to a compositor is fine and is the intended way to animate position.
- Position is always relative to the compositor's `(0,0)`, **not** the terminal. If you need terminal-relative placement, you typically include a base layer of the right size at `(0,0)`.

---

## 6. Effect: scrolling sine wave

A horizontal sine wave that scrolls. Single line, cheap, looks great in a status bar.

```go
import (
    "math"
    "strings"
)

// renderWave returns a 1-line wave of width `w`, with phase ∈ ℝ.
// `amp` is amplitude in cells, `freq` is cycles per width.
func renderWave(w int, phase, amp, freq float64) string {
    rows := int(2*amp + 1)
    grid := make([][]rune, rows)
    for i := range grid {
        grid[i] = []rune(strings.Repeat(" ", w))
    }
    mid := int(amp)
    for x := 0; x < w; x++ {
        y := mid - int(math.Round(amp*math.Sin(2*math.Pi*freq*float64(x)/float64(w)+phase)))
        if y >= 0 && y < rows {
            grid[y][x] = '~'
        }
    }
    var b strings.Builder
    for i, row := range grid {
        if i > 0 {
            b.WriteByte('\n')
        }
        b.WriteString(string(row))
    }
    return b.String()
}

// In Update:
case frameMsg:
    m.phase += 0.1
    return m, tick()

// In View:
return renderWave(m.width, m.phase, 3, 2)
```

Combine with `rainbowText` from §3 to get a flowing colored wave: render the wave, then re-color each cell by its `x` coordinate.

---

## 7. Effect: metaballs (lava-lamp blobs)

Metaballs are a scalar field summed from several "ball" sources, then **threshold-rendered** into ASCII. Each ball contributes `r²/((x-cx)² + (y-cy)²)` to the field; sum > 1 means "inside a blob."

```go
import "math"

type ball struct{ x, y, vx, vy, r float64 }

type metaballs struct {
    w, h  int
    balls []ball
}

func (m *metaballs) step(dt float64) {
    for i := range m.balls {
        b := &m.balls[i]
        b.x += b.vx * dt
        b.y += b.vy * dt
        if b.x < b.r || b.x > float64(m.w)-b.r {
            b.vx = -b.vx
        }
        if b.y < b.r || b.y > float64(m.h)-b.r {
            b.vy = -b.vy
        }
    }
}

func (m metaballs) field(x, y float64) float64 {
    var sum float64
    for _, b := range m.balls {
        dx, dy := x-b.x, y-b.y
        d2 := dx*dx + dy*dy
        if d2 < 1e-6 {
            return 1e6
        }
        sum += (b.r * b.r) / d2
    }
    return sum
}

// Render with shaded characters by field intensity.
func (m metaballs) render() string {
    const ramp = " .:-=+*#%@"  // dim → bright
    var b strings.Builder
    for y := 0; y < m.h; y++ {
        for x := 0; x < m.w; x++ {
            // Terminal cells are ~2:1 tall:wide — scale y to compensate.
            v := m.field(float64(x), float64(y)*2)
            switch {
            case v < 0.6:
                b.WriteByte(' ')
            default:
                idx := int(math.Min(float64(len(ramp)-1), (v-0.6)*5))
                b.WriteByte(ramp[idx])
            }
        }
        if y < m.h-1 {
            b.WriteByte('\n')
        }
    }
    return b.String()
}
```

Wire it up the usual way — `step(1.0/fps)` on each `frameMsg`, render in `View`. Add hue cycling per cell for the lava-lamp look.

> The "feel" of metaballs is in the constants. Start with 3–5 balls, radius 4–6, velocities 0.3–1.0 cells/frame at 30 FPS. Test in a 60×20 area before scaling up — `field()` is `O(n_balls × w × h)` and gets expensive fast.

References for the math: [AngelJumbo/lavat](https://github.com/AngelJumbo/lavat) (C, but very readable) and the field formulation on Wikipedia's *Metaballs* article.

---

## 8. Effect: ASCII rain / typewriter / spinner

Three small recipes that appear in lots of TUIs.

### Typewriter reveal

```go
type typewriter struct {
    full    string
    revealed int
}

case frameMsg:
    if m.tw.revealed < len(m.tw.full) {
        m.tw.revealed++
        return m, tea.Tick(40*time.Millisecond, frameTick)
    }
    return m, nil  // done — let it sit
```

Pick the tick interval by characters-per-second, not FPS — 25 cps (40ms) reads naturally; faster looks frenetic.

### Custom spinner

`bubbles/spinner` covers the common cases. For something custom, just rotate a frame slice:

```go
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

case frameMsg:
    m.spinFrame = (m.spinFrame + 1) % len(frames)
    return m, tea.Tick(80*time.Millisecond, frameTick)

func (m model) View() string { return frames[m.spinFrame] + " loading…" }
```

### Matrix-style rain

Each column has its own falling head and trail length. Maintain `[]column{head, len, speed}`, advance heads each frame, render top-to-bottom. Pair with green-fading-to-dark colors for the cliché.

---

## 9. Performance: when animation gets slow

Symptoms of a slow animation: jittering, terminal flicker, sluggish keypress response. In order of likelihood:

1. **Update + View > frame budget.** Profile by logging `time.Since(start)` at the end of `Update`. If you're consistently > 16ms at 60 FPS, drop to 30 FPS or skip-frame (§2).
2. **String concatenation in render hot paths.** Use `strings.Builder`, not `+=`. Pre-size with `b.Grow(estimatedBytes)` if you know the rough output size.
3. **Per-cell `lipgloss.NewStyle()` calls.** Style construction is cheap-ish but not free. Cache styles you'll reuse (e.g. one `Style` per palette entry).
4. **Rendering offscreen content.** If only 20 of 1000 lines are visible, only render those 20 (see `references/troubleshooting.md` § Performance).
5. **The terminal itself.** Some terminals (especially over SSH or in containers) struggle with high-frequency redraws. Test in a fast local terminal first; if it's slow there too, the bottleneck is yours.

A `pprof` CPU profile of a few seconds of animation will name the function eating your budget. `runtime/pprof.StartCPUProfile` from `main` and `go tool pprof -http=:8080 cpu.pprof` is the fastest path to an answer.

---

## 10. Testing animations with teatest

`teatest` (see `references/teatest.md`) drives a real `tea.Program`, so animation runs. Two patterns:

### Snapshot a frame after N ticks

```go
tm := teatest.NewTestModel(t, initialModel(),
    teatest.WithInitialTermSize(80, 24))
t.Cleanup(func() { _ = tm.Quit() })

// Drive 30 frames manually so the test is deterministic.
for i := 0; i < 30; i++ {
    tm.Send(frameMsg(time.Now()))
}

// Now assert on output.
out := readAll(tm.Output())
if !bytes.Contains(out, []byte("expected substring")) {
    t.Fatalf("frame 30 missing expected content: %s", out)
}
```

> **Don't** let real wall-clock ticks drive the test — they're flaky. Send `frameMsg` synthetically and assert on the deterministic state.

### Assert spring settles

```go
fm := tm.FinalModel(t).(*springModel)
if math.Abs(fm.pos-fm.target) > 0.1 {
    t.Errorf("spring did not settle: pos=%v target=%v", fm.pos, fm.target)
}
```

For golden-file tests of animated frames: pin `lipgloss.SetColorProfile(termenv.Ascii)` so the goldens are deterministic, and snapshot a specific frame number rather than the final output.

---

## Quick reference

| You want… | Reach for… |
|---|---|
| Repeating frame loop | `tea.Tick(d, fn)` + re-arm in `Update` |
| Wall-clock-aligned tick (every minute) | `tea.Every` |
| Rainbow / hue cycling | HSL → `image/color.RGBA` → `lipgloss.NewStyle().Foreground(...)` |
| Two-color pulse | Lerp between RGBA values per frame |
| Smooth panel slide / settle | `harmonica.Spring` |
| Sprite over background | `lipgloss.NewLayer` + `NewCompositor`, set Z |
| Wave, metaballs, custom particle | Hand-rolled scalar field, sample per cell |
| Faster than 60 FPS | Don't — you're wasting cycles |
| Slow animation | Profile, then cache styles or skip dirty-frames |
| Animation tests | Synthetic `frameMsg` via `tm.Send`, never wall-clock |

---

## Further reading

- Bubble Tea API: [`pkg.go.dev/charm.land/bubbletea/v2`](https://pkg.go.dev/charm.land/bubbletea/v2)
- Lip Gloss v2 compositor: [`charm.land/blog/lipgloss-v2-beta-2`](https://charm.land/blog/lipgloss-v2-beta-2/) and [`examples/canvas/main.go`](https://github.com/charmbracelet/lipgloss/blob/main/examples/canvas/main.go)
- Harmonica springs: [`github.com/charmbracelet/harmonica`](https://github.com/charmbracelet/harmonica)
- Metaballs reference (C): [`github.com/AngelJumbo/lavat`](https://github.com/AngelJumbo/lavat)
