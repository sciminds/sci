# Migration & self-review checklist

Use this to modernize an existing file, or as a final pass before declaring a
change done. The goal is to write modern Go *the first time* — but this is the
backstop.

## `go fix` modernizers (Go 1.26)

`go fix` was rebuilt on the `go/analysis` framework in Go 1.26 and now hosts
Go's "modernizers" — behavior-preserving rewrites that apply many patterns in
this skill mechanically:

- `interface{}` → `any`
- manual min/max loops → `min`/`max` builtins
- `sort.Slice` → `slices.SortFunc`
- `x := x` loop-var copies → removed (post-1.22)
- `for i := 0; i < n; i++` → `for i := range n`
- `for b.N` benchmark loops → `b.Loop()`
- `fmt.Errorf` without `%w` where wrapping was intended → `%w`
- `append([]T(nil), s...)` → `slices.Clone`

Run it as a first sweep, then review the diff — it's designed to be safe, but
read what it changed. Writing modern Go up front still beats relying on it.

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
- (Rules 1–8, 12–15 cover charm.land v2 imports, `Local: true` flags, `huh`
  routing, package doc comments, etc. — not modern-Go-style, but part of `just ok`.)

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
- [ ] `time.Time`/struct JSON fields that should drop when empty use `omitzero`.
- [ ] Transforms (`Map`/`Filter`/`GroupBy`/predicate search) use `lo`, not hand-rolled loops — see the `lo` skill.
- [ ] `just ok` passes.
