# teatest — Integration Testing for Bubble Tea

## Overview

`teatest` is Charm's helper library for integration-testing Bubble Tea (`tea.Model`) programs. It spins up a real `tea.Program` against an in-memory input buffer and a thread-safe output buffer, lets you drive the program with typed keys and arbitrary `tea.Msg` values, and then lets you assert against either the cumulative output stream or the final `tea.Model` state. It's essentially "drive the full Init/Update/View loop end-to-end, without a terminal."

- **Import path (v1, still the canonical one used almost everywhere):**
  `github.com/charmbracelet/x/exp/teatest`
- **v2 variant** (tracks Bubble Tea v2 / `charm.land/bubbletea/v2`):
  `github.com/charmbracelet/x/exp/teatest/v2`
- **Stability:** experimental — lives under `exp/`, zero compatibility guarantees. The package doc comment is literally:
  > `// Package teatest provides helper functions to test tea.Model's.`
  Carlos Becker's launch post is explicit: "We don't promise any compatibility guarantees just yet." Expect minor signature drift (e.g. the v2 variant already renamed `tea.KeyMsg` → `tea.KeyPressMsg`, defaults the terminal size to 80×24, and adds `WithProgramOptions`).
- **Module go.mod** (`exp/teatest/go.mod`):
  - `go 1.24.0`
  - `github.com/charmbracelet/bubbletea v1.3.5`
  - `github.com/charmbracelet/x/exp/golden v0.0.0-20240806155701-69247e0abc2a`
  No semver tag — consumers pin a pseudo-version. Recent relevant commits:
  - `f235fab0` (2025-08-21) `fix(teatest): fix race condition` (#366)
  - `b6045cb4` (2025-10-02) `fix(teatest): final model may be nil` (#539) — `FinalModel` now guards against nil on the `modelCh` drain.
  - `c83711a1` (2026-03-11) `feat: update teatest v2 to use charm.land` (#637)
- **Version inspected:** `charmbracelet/x` `main` @ `c83711a1` (2026-03-11).

## How it works internally

`NewTestModel` builds a `tea.Program` with:

```go
tea.NewProgram(
    m,
    tea.WithInput(tm.in),        // an in-memory *bytes.Buffer
    tea.WithOutput(tm.out),      // an RWMutex-wrapped *bytes.Buffer
    tea.WithoutSignals(),        // no SIGINT handling from the program
    tea.WithANSICompressor(),    // reduces drift between runs
)
```

It then:
1. Installs its own `SIGINT` handler in a goroutine that calls `program.Kill()` (so Ctrl-C in `go test` still tears the program down, even with `tea.WithoutSignals()`).
2. `go program.Run()` in another goroutine, pushing the final `tea.Model` to a 1-buffered channel and signalling `doneCh`.
3. If `WithInitialTermSize` was supplied, sends a `tea.WindowSizeMsg` into the program after starting.

Output is read via a `safeReadWriter` — a `bytes.Buffer` gated by a `sync.RWMutex`. This is what lets `tm.Output()` be polled from a test goroutine (`WaitFor`) while the program is concurrently writing frames to it. Important: because it's a `bytes.Buffer`, **reads consume**. Every call to `tm.Output()` returns the same underlying reader, so successive `io.ReadAll`s only see bytes produced since the last read.

### v1 vs v2 differences

- v2 always prepends `WithInitialTermSize(80, 24)` before your options apply.
- v2 adds `WithProgramOptions(options ...tea.ProgramOption)` so you can forward `tea.WithColorProfile`, `tea.WithAltScreen`, `tea.WithMouseCellMotion`, etc.
- v2 builds the program with `tea.WithWindowSize(w, h)` in addition to sending `tea.WindowSizeMsg`.
- v2 `Type` emits `tea.KeyPressMsg{Code: c, Text: string(c)}` per **rune**; v1 emits `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(c)}}` per **byte** — see Gotchas.
- v2 `RequireEqualOutput` calls `golden.RequireEqualEscape(tb, out, true)`; v1 calls `golden.RequireEqual(tb, out)`.
- v2 `FinalModel` does **not** nil-guard. v1 (post #539) keeps the previous non-nil model if the channel delivers nil.

## Core API

### Types

```go
// Narrow interface satisfied by *tea.Program. Lets you wire test models
// that send messages to each other without depending on the full program type.
type Program interface {
    Send(tea.Msg)
}

type TestModel struct { /* unexported */ }

type TestModelOptions struct { /* unexported */ }
type TestOption     func(*TestModelOptions)

type WaitingForContext struct {
    Duration      time.Duration
    CheckInterval time.Duration
}
type WaitForOption func(*WaitingForContext)

type FinalOpts struct { /* unexported */ }
type FinalOpt  func(*FinalOpts)
```

### Constructor

```go
func NewTestModel(tb testing.TB, m tea.Model, options ...TestOption) *TestModel
```

- Starts the program in a goroutine immediately. The program is already running by the time `NewTestModel` returns.
- Does **not** automatically call `tm.Quit()`. Add a `t.Cleanup(func() { _ = tm.Quit() })` if your program might otherwise hang (e.g. waits for a key you never send).

### TestOptions

| Option | Effect |
| --- | --- |
| `WithInitialTermSize(x, y int)` | Sends an initial `tea.WindowSizeMsg{Width:x, Height:y}` after the program boots. In v1, if you don't call this your program **never receives a size msg** — models using `bubbles/viewport` or `lipgloss/list` render degenerate frames. |
| `WithProgramOptions(...tea.ProgramOption)` | **v2 only.** Forwards raw Bubble Tea program options. Use for `tea.WithColorProfile`, `tea.WithAltScreen`, `tea.WithMouseCellMotion`, etc. |

### TestModel methods

```go
func (tm *TestModel) Send(m tea.Msg)
func (tm *TestModel) Type(s string)
func (tm *TestModel) Quit() error
func (tm *TestModel) Output() io.Reader
func (tm *TestModel) FinalOutput(tb testing.TB, opts ...FinalOpt) io.Reader
func (tm *TestModel) FinalModel(tb testing.TB, opts ...FinalOpt) tea.Model
func (tm *TestModel) WaitFinished(tb testing.TB, opts ...FinalOpt)
func (tm *TestModel) GetProgram() *tea.Program
```

- **`Send`** — forwards a raw `tea.Msg` (any type) into the program's message loop. Use for everything that isn't a literal keypress: `tea.WindowSizeMsg`, custom app messages, etc.
- **`Type(s)`** — synthesizes key events per element of `s`. **v1 iterates bytes**: a multi-byte rune becomes multiple one-byte "key" messages. **v2 iterates runes** with `KeyPressMsg{Code, Text}`. For non-ASCII in v1 use `tm.Send(tea.KeyMsg{...})`.
- **`Quit`** — calls `tm.program.Quit()` (sends a quit msg; does not Kill). Safe to call multiple times. Always returns `nil`.
- **`Output`** — returns the live output reader. The buffer is mutex-guarded; polling from a test goroutine is safe, but **reads drain**.
- **`FinalOutput`** — blocks until the program finishes (or the optional final-timeout fires), then returns the output reader.
- **`FinalModel`** — same blocking semantics, returns the last `tea.Model` from `program.Run()`. Cast to your concrete type to assert on internal state. v1 (post #539) remembers the last non-nil model — safe to call multiple times.
- **`WaitFinished`** — blocks until the program finishes or times out; returns nothing.
- **`GetProgram`** — escape hatch to grab `*tea.Program`. Usually unnecessary.

### FinalOpts (apply to `FinalModel`, `FinalOutput`, `WaitFinished`)

```go
func WithFinalTimeout(d time.Duration) FinalOpt
func WithTimeoutFn(fn func(tb testing.TB)) FinalOpt
```

- `WithFinalTimeout` — how long to wait for `program.Run()` to return. If omitted, `waitDone` blocks **forever**. Always set it for tests relying on interactive quit.
- `WithTimeoutFn` — optional callback invoked when the timeout fires instead of failing the test. Without it, a timeout calls `tb.Fatalf("timeout after %s", …)`.

The whole wait path is wrapped in a `sync.Once`, so calling `FinalModel` and `FinalOutput` in sequence is fine.

### WaitFor

```go
func WaitFor(
    tb testing.TB,
    r io.Reader,
    condition func(bts []byte) bool,
    options ...WaitForOption,
)

func WithCheckInterval(d time.Duration) WaitForOption
func WithDuration(d time.Duration) WaitForOption
```

- Defaults: `Duration = 1s`, `CheckInterval = 50ms`.
- Tight loop: reads current bytes from `r` into a cumulative buffer via `io.TeeReader`, calls `condition`, sleeps, fails with a dump of buffered output if the condition isn't met inside `Duration`.
- Because it reads on every tick, **once `WaitFor` has seen a byte, subsequent `tm.Output()` calls will not see it**.

### Golden files: RequireEqualOutput

```go
func RequireEqualOutput(tb testing.TB, out []byte)
```

Thin wrapper around `github.com/charmbracelet/x/exp/golden.RequireEqual`:

1. Golden path is always `testdata/<tb.Name()>.golden` — keyed on `t.Name()`, so `t.Run` subtests automatically get separate goldens under `testdata/TestOuter/name.golden`.
2. `-update` flag (registered by `exp/golden`, inherited transitively) rewrites the file with raw bytes.
3. On read, golden is normalized `\r\n → \n` (Windows), then both sides are `strconv.Quote`d line-by-line so ANSI sequences appear as `\x1b[?25l` etc. in diffs.
4. Diff via `github.com/aymanbagabas/go-udiff` — **no system `diff` binary required** (the package doc comment still says otherwise; stale).
5. Update with: `go test ./path/to/pkg -run TestName -update`.

Example raw golden bytes:
```
\x1b[?25l\x1b[?2004hHi. This program will exit in 10 seconds...
\x1b[K\x1b[70D\x1b[AHi. This program will exit in 9 seconds...
```
Cursor moves, clear-line, bracketed-paste toggles are literally in the bytes — this is why goldens are fragile across terminal sizes and renderer versions.

## Patterns

### 1. Minimal: assert on final state

```go
func TestCountdown(t *testing.T) {
    tm := teatest.NewTestModel(t, initialModel(time.Second),
        teatest.WithInitialTermSize(80, 24))
    t.Cleanup(func() { _ = tm.Quit() })

    fm := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(model)
    if fm.duration != time.Second {
        t.Errorf("duration = %v, want 1s", fm.duration)
    }
}
```

Cast `FinalModel` to your concrete type and assert fields. More robust than output-based assertions.

### 2. Full-output golden test

```go
func TestFullOutput(t *testing.T) {
    tm := teatest.NewTestModel(t, initialModel(time.Second),
        teatest.WithInitialTermSize(300, 100))

    out, err := io.ReadAll(tm.FinalOutput(t))
    if err != nil { t.Fatal(err) }
    teatest.RequireEqualOutput(t, out)
}
```

Generate with `go test -run TestFullOutput -update`.

### 3. Polling for intermediate state with WaitFor

```go
tm := teatest.NewTestModel(t, model(10), teatest.WithInitialTermSize(70, 30))

teatest.WaitFor(t, tm.Output(),
    func(out []byte) bool {
        return bytes.Contains(out, []byte("This program will exit in 7 seconds"))
    },
    teatest.WithDuration(5*time.Second),
    teatest.WithCheckInterval(10*time.Millisecond),
)

tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
_ = tm.Quit()

fm := tm.FinalModel(t).(model)
```

Rule of thumb: **never `time.Sleep`** in teatest tests — `WaitFor` blocks on an output condition, then fire the next input.

### 4. Special keys vs runes

```go
// Rune-typing (ASCII): Type is convenient.
tm.Type("hello")

// Non-rune keys: construct a KeyMsg yourself.
tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ñ'}}) // multi-byte in v1

// v2 equivalent:
tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
```

`tm.Type("ñ")` in v1 sends two one-byte `KeyMsg`s because the loop iterates bytes — garbage. Use explicit `Send` for non-ASCII.

### 5. Resize mid-test

```go
tm.Send(tea.WindowSizeMsg{Width: 40, Height: 20})
```

Your model's `Update` receives it like any real resize. Useful for asserting reflow.

### 6. Dual-pane / cross-program wiring

Two `TestModel`s exchanging custom messages — each holds a `[]teatest.Program` slice so they can cross-fire during `Update`:

```go
tm1 := teatest.NewTestModel(t, m1, teatest.WithInitialTermSize(70, 30))
t.Cleanup(func() { _ = tm1.Quit() })
tm2 := teatest.NewTestModel(t, m2, teatest.WithInitialTermSize(70, 30))
t.Cleanup(func() { _ = tm2.Quit() })

m1.programs = append(m1.programs, tm2)
m2.programs = append(m2.programs, tm1)

tm1.Type("pp"); tm2.Type("pppp")
tm1.Type("q");  tm2.Type("q")
```

The `Program` interface exists precisely to make this seam cheap.

## Gotchas

1. **`tm.Output()` is a drained reader.** Every poll — including `WaitFor` — consumes bytes. If you `WaitFor` and then `RequireEqualOutput` against `FinalOutput`, the golden will only contain the tail. Workarounds: (a) do all assertions via `WaitFor`, (b) read `Output()` into your own `bytes.Buffer` on every tick, or (c) skip `WaitFor` and let `FinalOutput` grab everything post-exit.

2. **No default terminal size in v1.** Without `WithInitialTermSize`, no `WindowSizeMsg` is sent and viewport/list-based models render degenerate frames. v2 defaults to 80×24.

3. **Goldens bake in terminal size, renderer version, and color profile.** Any of: changing `WithInitialTermSize`, upgrading Bubble Tea, or a different `TERM`/color profile on CI flips the bytes. Mitigations:
   - Pin the color profile once:
     ```go
     func init() { lipgloss.SetColorProfile(termenv.Ascii) }
     ```
     (v2: use `teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.Ascii))`.)
   - Add `*.golden -text` to `.gitattributes` so Git stops CRLF-normalizing on Windows checkouts.

4. **Ctrl+C is special.** `NewTestModel` passes `tea.WithoutSignals()` but installs its own `SIGINT` handler that calls `program.Kill()`. Ctrl-C in `go test` still tears the program down — but sending `tea.KeyMsg{Type: tea.KeyCtrlC}` to a model that doesn't explicitly map it will not auto-quit (default SIGINT→quit path is disabled).

5. **Parallel tests.** Each `NewTestModel` registers a `signal.Notify` goroutine. These self-clean on Ctrl-C, not normal exit. No current leak in practice, but worth knowing.

6. **Race fix in `f235fab0` (Aug 2025).** Pin a pseudo-version newer than this under `-race`.

7. **`FinalModel` returning nil.** Pre-`b6045cb4` v1 could return nil on fast paths, panicking on cast. Current v1 remembers the last non-nil model; v2 does not. Guard your cast or pin post-#539.

8. **`Type` iterates bytes in v1.** Use `Send(tea.KeyMsg{...})` for non-ASCII.

9. **No automatic Quit / cleanup.** Forgetting `t.Cleanup(func(){ _ = tm.Quit() })` leaks a goroutine and hangs `FinalModel` without a final timeout. Pair `NewTestModel` with `Cleanup` unconditionally.

10. **`RequireEqualOutput` doc comment is stale** — says "system `diff` tool"; reality: `go-udiff`. No external binary needed.

11. **Goldens under `t.Run`.** `tb.Name()` produces `testdata/TestOuter/subname.golden`. Filesystem-unsafe chars (slashes, colons) in subtest names break creation — keep subtest names simple.

## Minimal example

```go
type counter int

func (c counter) Init() tea.Cmd { return nil }
func (c counter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if m, ok := msg.(tea.KeyMsg); ok {
        switch m.String() {
        case "+": return c + 1, nil
        case "q": return c, tea.Quit
        }
    }
    return c, nil
}
func (c counter) View() string { return fmt.Sprintf("count: %d\n", int(c)) }

func TestCounter(t *testing.T) {
    tm := teatest.NewTestModel(t, counter(0), teatest.WithInitialTermSize(80, 24))
    t.Cleanup(func() { _ = tm.Quit() })

    tm.Type("+++")
    tm.Type("q")

    fm := tm.FinalModel(t, teatest.WithFinalTimeout(time.Second)).(counter)
    if fm != 3 {
        t.Errorf("counter = %d, want 3", fm)
    }
}
```

## Realistic example: dual-pane with async load

```go
func TestDualPane(t *testing.T) {
    tm := teatest.NewTestModel(t, newApp(), teatest.WithInitialTermSize(100, 30))
    t.Cleanup(func() { _ = tm.Quit() })

    // 1. Wait for initial list render.
    teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
        return bytes.Contains(out, []byte("README.md"))
    }, teatest.WithDuration(2*time.Second))

    // 2. Move down and select.
    tm.Send(tea.KeyMsg{Type: tea.KeyDown})
    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

    // 3. Wait for async preview load.
    teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
        return bytes.Contains(out, []byte("# Project"))
    }, teatest.WithDuration(3*time.Second))

    // 4. Resize mid-test.
    tm.Send(tea.WindowSizeMsg{Width: 60, Height: 20})

    // 5. Quit and assert on state.
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

    final := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second)).(*appModel)
    if final.selected != "README.md" {
        t.Errorf("selected = %q", final.selected)
    }
    if !final.loaded {
        t.Error("preview never loaded")
    }
}
```

Note: because `WaitFor` drained bytes, if you also want a golden snapshot, either read `Output()` into your own buffer on each tick or accept that `FinalOutput` only contains the tail.

## Version notes

- **API is under `exp/`** — signatures can change between commits.
- **Pin a pseudo-version** ≥ `v0.0.0-20251002...-b6045cb4` (post-#539) for `FinalModel` nil-safety under `-race`.
- **v2 migration**: `tea.KeyMsg{Type: ...}` → `tea.KeyPressMsg{Code: ...}`; `Type()` now iterates runes; forward color profile via `WithProgramOptions(tea.WithColorProfile(...))`.

## Citations

- Source: `charmbracelet/x` @ `c83711a1`:
  - `exp/teatest/teatest.go` — package source, all exports
  - `exp/teatest/app_test.go` — `TestApp`, `TestAppInteractive`
  - `exp/teatest/send_test.go` — dual-program `TestAppSendToOtherProgram`
  - `exp/teatest/teatest_test.go` — `TestWaitForErrorReader`, `TestWaitForTimeout`, `TestWaitFinishedWithTimeoutFn`
  - `exp/teatest/v2/teatest.go` — v2 variant
  - `exp/teatest/testdata/TestApp.golden` — raw ANSI golden example
  - `exp/golden/golden.go` — `-update` flag, path computation, udiff, escape-sequence quoting
- Commits: `f235fab0` (race fix #366), `b6045cb4` (nil FinalModel #539), `c83711a1` (v2 charm.land #637)
- Blog: https://carlosbecker.com/posts/teatest/ (Carlos Becker, 2023-05-08)
- pkg.go.dev: https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest
