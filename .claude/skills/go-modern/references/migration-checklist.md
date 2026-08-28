# Migration & self-review checklist

Use this to modernize an existing file, or as a final pass before declaring a
change done. The goal is to write modern Go *the first time* — but this is the
backstop.

## `go fix` modernizers

`go fix` (rebuilt on the `go/analysis` framework in 1.26) hosts Go's
"modernizers" — behavior-preserving rewrites that apply many patterns in this
skill mechanically:

- `interface{}` → `any`
- if/else min/max chains → `min`/`max` builtins (`minmax`)
- `sort.Slice` on basic ordered types → `slices.Sort` (`slicessort` — it never
  emits `SortFunc`; comparator sorts you rewrite by hand)
- `x := x` loop-var copies → removed (`forvar`, post-1.22)
- `for i := 0; i < n; i++` → `for i := range n` (`rangeint`)
- nested literals for embedded fields → direct promoted-field keys (`embedlit`, 1.27)
- raw `atomic.AddInt64`-style calls → typed `atomic.Int64` etc. (`atomictypes`, 1.27)
- manual reverse-iteration loops → `slices.Backward` (`slicesbackward`, 1.27)
- raw unsafe pointer arithmetic → `unsafe.Add` and friends (`unsafefuncs`, 1.27)
- `for b.N` benchmark loops → `b.Loop()` (`bloop` — **off by default**, enable explicitly)
- `append([]T(nil), s...)` → `slices.Clone` (`appendclipped` — **off by default**, enable explicitly)

Nothing rewrites `%v` → `%w` — changing wrapping behavior isn't
behavior-preserving, so `go fix` never will; audit error wrapping yourself.
Naming drift to watch in older docs: the `sync.WaitGroup` analyzer is
`waitgroupgo` (not `waitgroup`), and `fmtappendf` no longer exists.

Run it as a first sweep, then review the diff — it's designed to be safe, but
read what it changed. Writing modern Go up front still beats relying on it.

Adjacent 1.27 toolchain niceties: `go mod tidy` (with `go 1.27` in go.mod)
consolidates stray `require` blocks into the standard direct + indirect pair —
don't hand-tidy them; `go test` now runs the `stdversion` vet check by default,
flagging stdlib symbols newer than the module's `go` directive; and `go doc
pkg@version` answers "what does this API look like in the version we actually
depend on."

## Legacy → modern quick map

| If you see / are about to write… | Replace with | Ref |
|---|---|---|
| `sort.Slice` / `sort.Strings` / `sort.Sort` | `slices.Sort` / `slices.SortFunc` | slices-maps-cmp.md |
| `append([]T(nil), s...)` / `make+copy` | `slices.Clone` / `bytes.Clone` | slices-maps-cmp.md |
| nested `append(append(...))` | `slices.Concat` | slices-maps-cmp.md |
| `for k := range m { keys = append(keys, k) }` + sort | `slices.Sorted(maps.Keys(m))` | slices-maps-cmp.md |
| `for k, v := range src { dst[k] = v }` | `maps.Copy(dst, src)` | slices-maps-cmp.md |
| linear loop for membership | `slices.Contains` | slices-maps-cmp.md |
| `if-else` default chains | `cmp.Or(...)` | slices-maps-cmp.md |
| `math.Max`/`Min` + casts, or helper funcs | `max` / `min` builtins | language.md |
| `for i := 0; i < n; i++` | `for i := range n` | language.md |
| `x := x` before a closure/goroutine | delete it | language.md |
| `interface{}` | `any` | language.md |
| `func ptr[T](v T) *T` + `ptr(x)` | `new(x)` (1.26) | language.md |
| `strings.Split(s, "\n")` to iterate | `strings.Lines(s)` | iterators.md |
| `err.Error() == "..."` | sentinel + `errors.Is` | errors.md |
| `fmt.Errorf("...: %v", err)` (when caller must match) | `%w` | errors.md |
| `var t *T; errors.As(err, &t)` | `errors.AsType[*T](err)` (1.26) | errors.md |
| `time.Time` field + `omitempty` (never omits) | `omitzero` | stdlib-extras.md |
| `wg.Add(1); go func(){ defer wg.Done() … }()` | `wg.Go(func(){ … })` (1.25) | stdlib-extras.md |
| `runtime.SetFinalizer` | `runtime.AddCleanup` (1.24) | stdlib-extras.md |
| `rand.Seed(...)` + `rand.Intn` | `math/rand/v2` `rand.IntN` | stdlib-extras.md |
| package generic func taking one concrete type first | generic method on that type (1.27) | language.md |
| `Outer{Inner: Inner{Field: v}}` for an embedded field | `Outer{Field: v}` (1.27) | language.md |
| explicit `[T]` instantiation the target type already implies | drop it — inference covers it (1.27) | language.md |
| `github.com/google/uuid` (v4/v7/parse only) | stdlib `uuid` (1.27) | stdlib-extras.md |
| `strings.LastIndex` / `bytes.LastIndex` + slicing | `CutLast` (1.27) | stdlib-extras.md |
| manual `url.URL` / `url.Values` deep copy | `.Clone()` (1.27) | stdlib-extras.md |
| `httptest.NewServer` + `defer Close()` in tests | `httptest.NewTestServer(t, h)` (1.27) | stdlib-extras.md |
| manual transform loop (`for`+`append`) | `lo.Map`/`Filter`/etc. | **the `lo` skill** |

The last row is the dividing line: **transforms belong to the `lo` skill**, not
here. If the loop builds a new collection by mapping/filtering/grouping, that's
`lo`. Everything else above is this skill.

## What the project's linters enforce

You're writing ahead of these so they stay quiet, but know what trips them:

**`just lint-guard`** (`scripts/lint-guard.sh`, structural rules):
- Rule 9 — the legacy `sort` package is banned.
- Rule 10 — append-clone patterns banned (use `slices.Clone`/`Concat`).
- Rule 11 — manual byte cloning banned (use `bytes.Clone`).
- The other live rules are 1, 2, 4, 5, 7, 12, 13, 14, 15, 17, 18 and 19 — charm.land v2 imports, no `time.Sleep` in tests, `Local: true` on flags (and *not* on slice flags), no `exec.Command` in process-replacing files, `uikit.DrainStdin` after `tea.Program.Run()`, package doc comments, `huh` routing and scriptability parity, the `internal/zot/` net/http ban, and no `Destination` on a flag. Not modern-Go-style, but part of `just ok`.
- Rules 3, 6, 8 and 16 are retired and their numbers are never reused, so a rule number in the commit history keeps meaning what it said. Read `scripts/lint-guard.sh` for the current set rather than assuming the numbering is contiguous.

**`just lint-style`** (`.semgrep/go-modern.yml` + ast-grep): the `lo` rewrite
family — manual `for`+`append` → `lo.Map`/`Filter`/`FilterMap`/`GroupBy`/etc.,
plus `slices.Contains` / `slices.Sorted(maps.Keys(...))` / `maps.Copy` for the
non-transform cases. The `lo` skill documents the transform rules in detail.

**`just lint`** — golangci-lint (bugs, dupl). **`just ok`** — the full gate
(fmt + vet + lint + test + build); run after every change.

## Self-review checklist

Before calling a Go change done, scan for:

- [ ] No `sort.*` — sorting goes through `slices.Sort`/`SortFunc`.
- [ ] No `interface{}` in hand-written code — use `any`.
- [ ] No `append([]T(nil), …)` / `make+copy` clones — `slices.Clone` / `bytes.Clone`.
- [ ] No `x := x` loop-var shadows.
- [ ] Index loops that are really "count N times" use `for range n`.
- [ ] Map key extraction uses `slices.Sorted(maps.Keys(m))` / `slices.Collect`.
- [ ] Map merges use `maps.Copy`.
- [ ] Default/precedence chains use `cmp.Or`.
- [ ] Errors are wrapped with `%w` (one per `Errorf`); matched with `Is`/`As`/`AsType`, never string compare.
- [ ] `ptr()`-style helpers replaced with `new(expr)` (1.26).
- [ ] Generic helpers that take one concrete type as their first parameter are methods on that type (1.27).
- [ ] Struct literals set promoted fields directly instead of nesting the embedded type (1.27).
- [ ] No explicit `[T]` instantiations that the target type already determines (1.27).
- [ ] `time.Time`/struct JSON fields that should drop when empty use `omitzero`.
- [ ] New UUID needs use the stdlib `uuid` package, not `google/uuid` (1.27).
- [ ] No `LastIndex` + slicing — use `strings.CutLast`/`bytes.CutLast` (1.27).
- [ ] Transforms (`Map`/`Filter`/`GroupBy`/predicate search) use `lo`, not hand-rolled loops — see the `lo` skill.
- [ ] `just ok` passes.
