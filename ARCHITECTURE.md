# Architecture

This is a tour of how `sci` is built, written for someone who's comfortable with web development (Svelte, React, that sort of thing) but new to Go and to this codebase. The goal is that after reading it you can (a) add a new `sci foo` command, (b) read a TUI package without getting lost, and (c) know which file to open when you want to change a color.

## What `sci` is

`sci` is a CLI toolkit for academic computing on macOS. It ships as a single static binary — no Python install, no Node, no `brew install` chain. Drop the binary on a machine and it works.

The repo currently produces three binaries:

- **`sci`** — the umbrella CLI (`sci db …`, `sci proj …`, `sci cass …`, etc.)
- **`dbtui`** — a standalone SQLite browser (also reachable as `sci view`)
- **`zot`** — experimental Zotero CLI (also reachable as `sci zot`)

We picked Go for three reasons. First, it's typed and compiled, which means TDD with an LLM is reliable — the compiler catches the dumb stuff before tests even run. Second, single static binaries cross-compile from anything to anything; distribution is "upload to a GitHub release". Third, Eshin wanted to learn Go. All three reasons are still load-bearing.

## The shape of a command

Every command in `sci` follows the same three-layer shape. If you understand one, you understand all of them. Let's trace `sci db info`:

1. **`cmd/sci/db.go`** registers the `db` command tree using urfave/cli v3 (think of it as Express's router but for argv). The `info` action is a small function that parses flags and calls into the `db` package.
2. **`internal/db/`** does the actual work — opening the database, listing tables, computing stats. It returns a value that satisfies the `cmdutil.Result` interface:

   ```go
   type Result interface {
       JSON() any        // for --json output
       Human() string    // for terminal output
   }
   ```

3. **`cmdutil.Output(cmd, result)`** picks the right one based on the `--json` flag and writes to stdout.

That's it. Every command in the codebase follows this shape. If you're lost, the navigation rule is: find the file in `cmd/sci/`, follow it into `internal/`, look at the `Result` type. Coming from Svelte: think of `cmd/sci/<area>.go` as `+page.server.ts` (the route handler), `internal/<area>/` as your actual business logic, and `cmdutil.Result` as the equivalent of returning JSON vs HTML based on the `Accept` header — one source of truth, two output formats.

The CLI framework is **urfave/cli v3**, not Cobra. All flags are declared `Local: true` so they don't leak between subcommands.

## Two SQLite worlds

Almost everything in `sci` that persists state uses SQLite, via `modernc.org/sqlite` — a pure-Go port. **No CGO ever.** This is what lets us cross-compile to any OS/arch combination from anyone's laptop without a C toolchain.

There are two ways we talk to SQLite, and the split matters:

- **`pocketbase/dbx`** in `internal/db/data/`. A typed query builder, ergonomic for the database-manager commands (create, import, rename, etc.).
- **Raw `database/sql`** in `internal/tui/dbtui/data/`, `internal/zot/local/`, and `internal/board/` (LocalCache).

The raw-SQL packages exist for two reasons. Either they need dynamic SQL the query builder can't express cleanly (FTS5, virtual tables, user-supplied queries), or they ship as standalone binaries (`dbtui`, `zot`) and we don't want to drag the entire pocketbase dependency into a 20MB tool. Both reasons are fine. Pick the right one when you add a new package.

## Bubbletea, gently

The interesting part of the codebase is the TUI work — `dbtui`, `internal/tui/board/`, the wizards inside `proj new`, the help browser. They're all built on **Bubble Tea v2**, which is a Go port of The Elm Architecture (TEA, hence the name).

If you're coming from Svelte, here's the mental translation:

- A **Model** is your component state. One big struct.
- **`Update(msg, model) -> (model, cmd)`** is the only place state changes. It's a pure function: same inputs, same outputs, no side effects. Think of it as the union of every `on:click`, `$effect`, and store-set in your Svelte component, collapsed into one switch statement.
- **`View(model) -> string`** renders the current state to a string of terminal output. Pure function. No DOM, no diffing — every keystroke re-renders the entire screen and the runtime figures out what to actually paint.
- **`Cmd`** is how you do async work. You don't `await fetch()` inside `Update`; you return a `Cmd` that the runtime executes off the main loop, and when it finishes the result comes back as a new message that flows through `Update`. It's like Redux Thunks if you used those, or `tea.Cmd` if you didn't.

The whole thing is single-threaded and synchronous from the model's perspective. No race conditions. No "is this state stale?" No `useEffect` dependency arrays. The tradeoff is verbosity — you write a switch statement for every key — but in exchange you get a UI you can drive from a test without ever opening a terminal.

That last part is critical: we use **`teatest`** for every TUI in the repo. A teatest test sends keystrokes into the model, lets the runtime spin, and asserts on the output. No real terminal, no flakiness, runs in milliseconds. See `internal/tui/dbtui/app/TESTING.md` for the protocol.

### How a TUI package is laid out

`internal/tui/dbtui/` is the canonical example. Every TUI in the repo follows this shape:

```
app/
  types.go      # message types — the "what can happen" enum
  model.go      # the Model struct + Init
  update.go     # top-level Update — dispatches to per-screen handlers
  keys.go       # per-screen key handlers
  cmds.go       # async commands (DB queries, network calls, ticks)
  view.go       # top-level View composition
  view_*.go     # one render function per screen
ui/
  styles.go     # every lipgloss style and layout constant, in one file
```

`internal/tui/board/` mirrors this exactly. New TUIs should too. The split isn't rigid — small apps can fold things together — but the principle is "one concern per file, mechanical to navigate".

### When you need to actually build a TUI

There's a `bubbletea` skill in `.claude/skills/bubbletea/` with production templates, layout patterns, and (most importantly) a `references/golden-rules.md` that documents the four bugs that bite everyone the first time they build a bubbletea app — borders eating padding, auto-wrap inside bordered panels, mouse coordinates not matching layout, fixed pixel sizes that break on resize. Read it before you start. Don't try to learn lipgloss layout by trial and error; we've already paid that tax.

## Styling, in one place

Every `lipgloss.NewStyle()` in the codebase lives in either `internal/uikit/` (shared styling foundation), `internal/cliui/` (CLI-specific helpers — spinners, prompts, headers), or `internal/tui/<app>/ui/` (for a specific TUI). Code asks for styles via the `uikit.TUI` singleton:

```go
fmt.Println(uikit.TUI.Header("hello"))
```

The reason is uniformity — every command should look like every other command — and the ability to retheme everything in one place. Inline `lipgloss.NewStyle()` calls outside `uikit/` and `*/ui/` packages are a lint error. `huh` forms (we use them for interactive prompts) plug into `cliui.HuhTheme()` and `cliui.HuhKeyMap()` for the same reason.

## Process-replacing exec

Some commands launch interactive tools — `sci py repl` opens IPython, `sci py marimo` opens marimo, `sci proj preview` opens Quarto's dev server. For these, we don't use `exec.Command` (which would leave the Go process sitting in the middle, forwarding stdin/stdout). We use `syscall.Exec`, which **replaces** the current process with the child. Go evaporates, the child takes over the terminal, signals work correctly, no weirdness.

The catch: once you `syscall.Exec`, your test can't observe what happened. So every package that does this exports `Build*Args` helpers — pure functions that compute the argv slice — and tests assert on those instead of running the real thing.

## Two-surface CLIs

`dbtui` and `zot` exist as both standalone binaries (`cmd/dbtui/`, `cmd/zot/`) and as `sci` subcommands (`sci view`, `sci zot`). To avoid duplicating wiring, the full urfave/cli command tree lives in `internal/<pkg>/cli.Commands()`, and both entry points import it. Add a subcommand in one place, it shows up in both surfaces. If you're tempted to write the same flag declaration twice, stop and find the shared `cli.Commands()`.

## The odd one out: `internal/board/`

Most of `sci` is "function takes input, returns Result". `internal/board/` is the exception — it's a shared kanban sync engine that doesn't fit the request/response shape, so it's worth flagging.

Multiple users (everyone in the lab) edit the same boards from their own machines. Instead of running a server, we use Cloudflare R2 as a dumb object store. Every edit becomes an immutable JSON event written under `boards/{id}/events/{eventID}.json`. To compute the current state of a board, you list all the event files, sort them by ID, and fold them through a pure `Apply` function. The ID format is `{nanoseconds}-{random hex}`, which means sorting by ID is the same as sorting by wall clock — and that's the property that makes concurrent edits from different clients converge without any conflict resolution code.

It's a CRDT, basically, but a very small one. Read `internal/board/CLAUDE.md` if you need to touch this code; the design has subtle invariants and the gotchas there are real.

## Finding your way around

- **A command's wiring** — start at `cmd/sci/<area>.go`, follow it into `internal/<area>/`.
- **A TUI's behavior** — start at `internal/tui/<name>/app/update.go` and follow message types.
- **Styles** — `internal/uikit/` for shared primitives, `internal/cliui/` for CLI helpers, `internal/tui/<name>/ui/` for that specific TUI.
- **Cross-cutting rules and the workflow gate** — repo-root `CLAUDE.md`.
- **Non-obvious package decisions** — the `CLAUDE.md` inside that package. They exist for `board/`, `tui/board/`, `tui/dbtui/`, and `zot/`.
- **The current package set** — `ls internal/`. Each package has a doc comment on its `package` declaration. We don't maintain a hand-curated table because they rot.

## Testing layers

Three layers, each with a different cost/coverage tradeoff:

- **Unit tests** — pure logic, no I/O. Milliseconds. The bulk of the suite.
- **Teatest** — full bubbletea message loop with no real terminal. Runs unconditionally on every `just ok`. This is how every TUI is tested.
- **Integration tests** — shell out to real tools (`pixi`, `uv`, `quarto`, `marimo`, the Canvas API, real R2). Gated behind environment variables — `SLOW=1` for `proj/new`, `CANVAS_TOKEN` for `cass`, `BOARD_LIVE=1` for `board`, `ZOT_REAL_DB` for `zot`. They live in the same files as the unit tests but skip when the env var is missing.

The gate is `just ok` — fmt, vet, lint, test, build. Run it after every change. It also runs as a pre-commit hook. If `just ok` is green, you're free to push.

For the full list of test recipes and the workflow rules around them, see the repo-root `CLAUDE.md`.
