# huh v2 — Spinner (`charm.land/huh/v2/spinner`)

A standalone spinner for background activity — typically shown after a form submits while you do work. Independent of the form API.

```go
import "charm.land/huh/v2/spinner"
```

## Action style (run a func, block until done)

```go
err := spinner.New().
    Title("Making your burger…").
    Type(spinner.Dots).
    Action(makeBurger).   // func() — spinner stops when it returns
    Run()                 // standalone; in sci-go prefer a uikit wrapper

fmt.Println("Order up!")
```

## Context style (cancel-aware / external goroutine)

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

go makeBurger()

err := spinner.New().
    Type(spinner.Line).
    Title("Making your burger…").
    Context(ctx).         // spinner stops when ctx is done
    Run()
```

`ActionWithErr(func(context.Context) error)` combines both — run cancel-aware work and propagate its error through `Run()`:

```go
err := spinner.New().
    Title("Fetching…").
    ActionWithErr(func(ctx context.Context) error {
        return fetch(ctx)
    }).
    Run()
```

## Builder methods

| Method | Purpose |
|---|---|
| `.Title(string)` | Label beside the spinner |
| `.Type(spinner.Type)` | Animation style (below) |
| `.Action(func())` | Synchronous work; stops on return |
| `.ActionWithErr(func(ctx) error)` | Cancel-aware work returning an error |
| `.Context(ctx)` | Stop when context is canceled/done |
| `.WithTheme(Theme)` / `.WithAccessible(bool)` | Styling / screen-reader mode |
| `.WithOutput(w)` / `.WithInput(r)` | Redirect IO |
| `.WithViewHook(hook)` | Mutate the `tea.View` before render |

## Spinner types

```
spinner.Line   spinner.Dots   spinner.MiniDot  spinner.Jump
spinner.Points spinner.Pulse  spinner.Globe    spinner.Moon
spinner.Monkey spinner.Meter  spinner.Hamburger spinner.Ellipsis
```

## Notes

- The spinner builds its own Bubble Tea v2 program on `.Run()`; don't call it inside another running `tea.Program` — instead drive a `bubbles/v2/spinner` directly in your model (see the `bubbletea` skill).
- Accessible mode prints a static "working…" message instead of animating.
- Spinner docs: <https://pkg.go.dev/charm.land/huh/v2/spinner>
