# CLAUDE.md — sci CLI (Go)

## Workflow

- **`just ok` is the gate.** Run after every change. Never invoke `go build` / `go test` / `gofmt` directly — always go through `justfile` recipes (`just test`, `just run …`, `just lint`, etc.). If you need a recipe that doesn't exist, add it.
- **TDD by default** for new features and bug fixes: write the failing test first, then make it pass. Skip TDD only for trivial edits (typos, doc tweaks, one-line refactors).
- **Bubbletea work → invoke the `bubbletea` skill** before designing layouts, fixing rendering bugs, or adding mouse/keyboard handling. Its `references/golden-rules.md` prevents the most common border/overflow bugs. Required for any new TUI screen.
- **All work on `main`.** Linear project: `sciminds-cli` (team EJO).

## Test recipes

```
just ok              # gate: fmt + vet + lint + test + build
just test-slow       # proj/new integration (~4 min, SLOW=1, needs pixi/uv/quarto/marimo/typst/node)
just test-canvas     # cass integration (needs CANVAS_TOKEN in .env + gh auth login)
just test-board-live # R2 round-trip + privacy assertion (BOARD_LIVE=1)
just test-zot-real   # opt-in real-Zotero-DB smoke (reads ./zotero.sqlite)
```

## Sub-CLAUDE pointers (read before editing these packages)

| When you touch… | Read first |
|---|---|
| `internal/board/` (sync engine, R2 event log) | `internal/board/CLAUDE.md` |
| `internal/tui/board/` (kanban TUI) | `internal/tui/board/CLAUDE.md` |
| `internal/tui/dbtui/` (SQLite browser) | `internal/tui/dbtui/CLAUDE.md` + `app/TESTING.md` |
| `internal/zot/` (Zotero CLI + hygiene) | `internal/zot/CLAUDE.md` |
| `internal/tui/kit/` (shared TUI primitives) | `kit/doc.go` + `kit/README.md` |

`ARCHITECTURE.md` and `internal/README.md` are sketches and may be stale — trust the code.

## Modern Go style

Collaborators come from Python/JS backgrounds. Prefer expressive, low-boilerplate Go over verbose manual loops.

- **`samber/lo` for transforms.** Use `lo.Map`, `lo.Filter`, `lo.FilterMap`, `lo.Find`, `lo.Reduce`, `lo.GroupBy`, `lo.KeyBy`, `lo.FlatMap`, `lo.Uniq`/`lo.UniqBy`, `lo.Contains`, `lo.SliceToMap`, `lo.Intersect`, `lo.Difference`, etc. instead of hand-rolled `for`+`append` loops. Go's stdlib lacks generic Map/Filter/GroupBy — `lo` fills that gap and reads like the Python/JS equivalents collaborators already know.
- **Additional `lo` helpers to prefer over manual loops:**
  - **Chunking:** `lo.Chunk(slice, n)` — replaces `for i := 0; i < len(s); i += n` step-loops.
  - **Reject:** `lo.Reject` — inverse of Filter (negated condition). Replaces `if !cond { append }`.
  - **Set building:** `lo.Keyify(slice)` → `map[T]struct{}`. Replaces `m := map[T]bool{}; for { m[v]=true }`.
  - **Flatten:** `lo.Flatten(nested)` — replaces `for { out = append(out, sub...) }`.
  - **Compact:** `lo.Compact(slice)` — removes zero-value elements (empty strings, nils, 0s).
  - **CountValues:** `lo.CountValues(slice)` — frequency map. Replaces `m[v]++` loops.
  - **`*Err` variants:** `lo.MapErr`, `lo.FilterErr`, `lo.ReduceErr`, `lo.GroupByErr`, etc. — callbacks return `(T, error)` and short-circuit on first error. Use when transforms touch I/O.
  - **Ternary:** `lo.Ternary(cond, a, b)` — for simple one-liner `if/else` value assignments.
- **stdlib `slices`/`maps`/`cmp` when they suffice.** Use `slices.Sort`, `slices.SortFunc`, `slices.Clone`, `slices.Concat`, `slices.Contains`, `slices.Sorted(maps.Keys(m))`, `bytes.Clone`, `cmp.Compare`, `cmp.Or`. These cover sorting, cloning, and simple lookups without an external dep. Use `maps.Copy(dst, src)` instead of manual map-merge loops.
- **No legacy `sort` package.** `sort.Strings`, `sort.Slice`, `sort.SliceStable`, `sort.Search` are banned by lint-guard rule 9. Use `slices.Sort` / `slices.SortFunc` / `slices.SortStableFunc` / `slices.BinarySearch` instead.
- **Rule of thumb:** if stdlib has it, use stdlib. If it doesn't (Map, Filter, GroupBy, KeyBy, Find, Reduce, Chunk, set ops), use `lo`. Never hand-roll what either provides.
- **Semgrep enforces this.** `.semgrep/go-modern.yml` has 20 rules (136 current hits) that flag manual loops replaceable by `lo` or stdlib. Run via `just lint-style`. When adding new code, prefer `lo`/stdlib from the start — don't create new semgrep debt.
- **`lo` skill is required.** Before writing any code that transforms slices, maps, or sets, **invoke the `lo` skill** to look up the right function. The skill includes a decision framework, Python/JS → Go translations, and `*Err` variant tables. Don't guess from memory — consult the skill.

## Cross-cutting design rules

- **`cmdutil.Result`:** every command returns `JSON() any` + `Human() string`; emit via `cmdutil.Output(cmd, result)`.
- **CLI framework:** urfave/cli v3. All flags use `Local: true`.
- **SQLite:** pure Go (`modernc.org/sqlite`), no CGO. Default to `pocketbase/dbx` via `internal/db/data/`. Documented exceptions that use raw `database/sql`: `internal/tui/dbtui/data/`, `internal/markdb/`, `internal/zot/local/`, `internal/board/` LocalCache. The reason in every case is "this package is reusable standalone and must not pull in pocketbase".
- **Bubbletea v2 + bubbles v2** everywhere. No v1 imports.
- **No inline `lipgloss.NewStyle()`** outside `internal/ui/` or `internal/tui/*/ui/`. Access via the `ui.TUI` singleton. `huh` forms use `ui.HuhTheme()` + `ui.HuhKeyMap()`.
- **`kit` first for TUI components.** `internal/tui/kit/` provides pre-styled, low-boilerplate primitives that sit on top of Bubbletea. Prefer kit over hand-wiring bubbles directly:
  - **Filterable lists →** `kit.NewListPicker(title, kit.Items(slice), hints…)` — replaces the 10-line `list.New` + delegate + styling + filtering-enable boilerplate. Use `lp.IsFiltering()` instead of `FilterState() != list.Filtering`.
  - **Modal overlays →** `kit.OverlayBox{Title, Body, Hints}.Render(termW)` — replaces manual HeaderSection + HeaderHint + OverlayBox assembly. Use for simple title+body+hints overlays; keep manual rendering only when the footer has conditional/mixed-style content.
  - **Async tea.Cmd →** `kit.AsyncCmd(fn)` / `kit.AsyncCmdCtx(ctx, timeout, fn)` returning `kit.Result[T]` — replaces the repeated `func() tea.Msg { ctx, cancel := …; defer cancel(); … }` pattern. Type-switch on `kit.Result[ConcreteType]` in Update.
  - **Screen routing →** `kit.Screen` / `kit.Router` — dispatch table replacing switch-on-screen in View/Update/Keys.
  - **Chrome layout →** `kit.Chrome{Title, Status, Body}.Render(w, h)` — three-part vertical layout with automatic height math.
  - **2-D cursor →** `kit.Grid2D` — reusable cursor with move/clamp/wrap for grids.
  - **When to extend kit:** if a Bubbletea pattern appears in ≥ 2 TUIs (or is clearly headed that way), abstract it into kit rather than copy-pasting. Kit types should be plain structs testable without teatest; they compose inside a Bubbletea Model but don't replace it.
- **Process-replacing exec** (REPL, marimo, quarto) via `syscall.Exec`, not `exec.Command`. Export `Build*Args` helpers for tests.
- **Reuse shared infra** (`cmdutil`, `ui`, `kit`, `netutil`) — don't re-implement spinners, confirms, lists, or styling per-package.
- **New TUI apps** go under `internal/tui/<name>/` and follow the dbtui split (`app/`, `ui/`, root-pkg `Run` entry).
- **Two-surface CLIs** (e.g. `zot`): full command tree lives in `internal/<pkg>/cli.Commands()`; both `cmd/<pkg>/main.go` and `cmd/sci/<pkg>.go` import it. Never duplicate wiring.

## Testing rules

- **teatest** for every bubbletea model — full key→Update→View loop. Protocol: `internal/tui/dbtui/app/TESTING.md`.
- DB mutations verified by querying the store directly, not by inspecting model state.
- No `time.Sleep` in tests — use `teatest.WaitFor`.
- Golden file updates: `go test ./path -run TestName -update` (only place raw `go test` is acceptable; the `-update` flag isn't wired through `just`).

## Audience

Collaborators are beginner/intermediate Go devs — keep code clear, avoid clever patterns, don't sacrifice efficiency for pedagogy.

## Gotchas

- `proj/new` integration tests skip unless `SLOW=1`.
- marimo export exits non-zero for `mo.md()` cells — assert on the produced file, not the exit code.
- `install.sh` must be POSIX sh (runs on bare Macs).
- CI uses a rolling `latest` release tag (delete + recreate on push to main).
- GitHub Classroom URL IDs are *org* IDs, not classroom IDs — `ResolveClassroomID` maps URL → API ID and caches in `cass.yaml` as `api_id`.
- `brew bundle check` exits non-zero when deps are missing (normal). `isBundleCheckOutput` in `brew.go` distinguishes that from a real failure.
- Brewfile is a *lockfile*, not a manifest. `brew.Sync` reconciles bidirectionally with the actual brew/uv state. Resolve its path via `brew.LocateBrewfile()` — never hardcode the XDG default.
