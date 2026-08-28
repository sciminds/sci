# Language features (Go 1.21–1.27)

Language-level changes that remove boilerplate. Each notes the minimum `go`
directive required (features only activate when `go.mod` declares that version or
higher).

## `min` / `max` / `clear` builtins (1.21)

No import. `min`/`max` take one or more arguments of any ordered type and return
the same type — no `math.Max` float-only casting.

```go
hi := max(a, b)
clamped := max(lo, min(v, hi))          // clamp idiom
longest := max(len(a), len(b), len(c))  // any number of args
```

`clear` empties a map or zeroes a slice:
```go
clear(cache)   // map: removes all entries
clear(buf)     // slice: sets every element to the zero value — len is UNCHANGED
```
Gotchas: `clear(slice)` is **not** `s = s[:0]` (length stays). With floats,
`max` propagates NaN and treats `-0.0 < +0.0` per the spec.

## Per-iteration loop variables (1.22)

Each iteration of a `for` loop gets a **fresh** copy of the loop variable. The
classic closure-capture bug — and the `x := x` shadow that worked around it — are
gone.

```go
// Before 1.22 (mandatory shadow, or all goroutines saw the last value):
for _, v := range items {
    v := v
    go func() { handle(v) }()
}

// 1.22+ — delete the shadow:
for _, v := range items {
    go func() { handle(v) }()   // each goroutine captures its own v
}
```

Notes:
- Active only when `go.mod` declares `go 1.22` or later (this repo is well past).
- `vet`'s `loopclosure` check is retired; leftover `x := x` lines are harmless
  no-ops — delete them (`go fix` does).
- Rare behavior change: code that *relied* on sharing one variable across
  iterations now sees per-iteration copies. Almost always that code was buggy.

## Range over integers (1.22)

```go
for i := range n { ... }   // i = 0 .. n-1
for range n { ... }        // "do this n times", no index variable
```
`n` must be an integer; a non-positive `n` iterates zero times. Replaces the
C-style `for i := 0; i < n; i++` for the common counting case.

## `any` over `interface{}` (1.18)

`any` is an exact alias for `interface{}` — same type, more readable. Use it
everywhere in hand-written code; this repo bans `interface{}` outside generated
files. `gofmt` does not auto-rewrite, so write `any` from the start (`go fix`
can rewrite an existing file).

## Generics — when to write your own

`samber/lo` covers the common transforms (see the `lo` skill), so you rarely need
to write generic functions. Reach for your own generic helper when:

- The logic isn't a transform `lo` already provides (e.g. a typed tree walk, a
  generic cache, a `Result[T]`-style wrapper).
- You'd otherwise copy-paste the same function for `int`, `string`, `float64`.

```go
// A small generic helper — constrain to exactly what you use.
func keysOf[K comparable, V any](m map[K]V) []K {
    return slices.Collect(maps.Keys(m))
}

// Constraints: cmp.Ordered for "< works"; comparable for "== works / map key";
// any for "no constraint". Define a custom interface constraint only when you
// need specific methods.
func clampOrdered[T cmp.Ordered](v, lo, hi T) T { return max(lo, min(v, hi)) }
```

Keep constraints as narrow as the body actually requires — `comparable` if you
only compare, `cmp.Ordered` if you order, a method interface if you call methods.
And if the helper's first parameter is one concrete type, it probably wants to
be a **generic method** on that type instead (1.27, below).

### Generic type aliases (1.24)

Type aliases can take type parameters. Use them to shorten a verbose
instantiation **without** creating a new defined type (an alias keeps the
underlying type's identity and method set; a defined type starts fresh).

```go
type Set[T comparable]        = map[T]struct{}     // alias — interchangeable with map[T]struct{}
type Result[T any]            = func() (T, error)
```

Contrast with a *defined* generic type, which you use when you want to attach
methods or prevent accidental interchange:
```go
type Stack[T any] []T          // defined type — can have methods, not an alias
func (s *Stack[T]) Push(v T) { *s = append(*s, v) }
```

## `new(expr)` (1.26)

`new` now accepts an expression and returns a pointer to a fresh variable
initialized to that value. This retires the `ptr[T any](v T) *T { return &v }`
helper that codebases grow for optional/nullable fields.

```go
// Before — the ubiquitous helper:
func ptr[T any](v T) *T { return &v }
cfg := Config{Retries: ptr(3), Timeout: ptr(30 * time.Second)}

// 1.26:
cfg := Config{Retries: new(3), Timeout: new(30 * time.Second)}
```

Use `new(expr)` to replace `ptr(scalarOrExpr)`. For a composite literal you
already have, `&T{...}` remains the idiom — `new(T{...})` works but reads no
better. Don't mechanically convert every `&x`.

## Generic methods (1.27)

Methods can now declare their own type parameters. Before 1.27, "a generic
operation on type `T`" had exactly two bad spellings: a family of per-type
methods, or a package-level generic function taking the receiver as an argument.
Both are boilerplate; both now collapse into a method.

```go
// Before 1.27 — pick your poison:
func (r *Rand) IntN(n int) int          // (a) one method per type…
func (r *Rand) Int32N(n int32) int32
func (r *Rand) Int64N(n int64) int64
func GetTyped[T any](c *Cache, k string) (T, bool)  // (b) …or a free function

// 1.27 — one generic method, in the type's namespace where it belongs:
func (r *Rand) N[Int intType](n Int) Int            // math/rand/v2 does exactly this
func (c *Cache) Get[T any](k string) (T, bool)
```

Calls infer the type argument the usual way (`r.N(int32(10))`), or take it
explicitly when the result type drives it (`c.Get[User]("id")`).

When to reach for one:
- A package-level generic helper whose first parameter is one concrete type —
  it's a method in disguise; move it onto the type.
- Typed accessors on containers/caches/registries that previously returned
  `any` and made the caller assert.
- API families that differ only in a parameter's type (`math/rand/v2.Rand.N`
  replacing the `IntN`/`Int32N`/`Int64N` trio is the canonical example).

Limits to know:
- **Interface methods cannot declare type parameters**, and a generic method
  **cannot implement an interface method**. Generic methods are for concrete
  types; if you need polymorphism over the *caller's* type through an
  interface, you still want a generic function with an interface constraint.
- The receiver's own type parameters (on a generic *type*) and the method's
  type parameters are independent lists; both can appear together.

## Embedded/nested fields as struct-literal keys (1.27)

A key in a struct literal may be any valid **field selector** for the struct
type — including fields promoted from embedded structs — not just top-level
field names. Composition via embedding no longer taxes every construction site
with the intermediate type name:

```go
type Habitat struct {
    Burrow string
}

type Gopher struct {
    Name    string
    Habitat // embedded
}

// Before 1.27 — the embedding leaks into the literal:
g := Gopher{Name: "Gopher", Habitat: Habitat{Burrow: "Burrow #42"}}

// 1.27 — initialize the promoted field directly:
g := Gopher{Name: "Gopher", Burrow: "Burrow #42"}
```

This pairs especially well with the options/config-struct pattern: shared
fields can live in an embedded `CommonOpts` without callers ever noticing.
Notes:
- The selector must be unambiguous, same as any promoted-field access — if two
  embedded structs both promote `Burrow`, name the path explicitly.
- Don't initialize the same memory twice (e.g. both `Habitat: ...` and
  `Burrow: ...` in one literal) — the compiler rejects duplicate keys.
- Prefer the direct form in new code; only spell out the inner literal when
  you're setting many fields of the embedded struct and the grouping helps.

## Type inference in all assignment contexts (1.27)

Function type inference is generalized: wherever a generic function is assigned
to (or converted to) a matching concrete function type, the type arguments are
inferred. Previously this worked for plain assignments and function arguments
but not composite literals, conversions, or channel sends — which is why
explicit `[int]` instantiations littered callback tables.

```go
func GenericFormatter[T any](v T) string { return fmt.Sprintf("value: %v", v) }

type IntFormatter func(int) string

formatters := []IntFormatter{GenericFormatter}   // was: GenericFormatter[int]
fn := IntFormatter(GenericFormatter)             // conversion infers T = int
ch := make(chan IntFormatter, 1)
ch <- GenericFormatter                           // channel send infers too
```

Reflex: an explicit instantiation whose type arguments merely repeat what the
target type already says is now noise — drop it. Keep explicit type arguments
only where nothing constrains them (e.g. `slices.Collect[int]` in isolation).

## Self-referential generic constraints (1.26)

A generic type may now reference itself in its own type-parameter list — the
"curiously recurring" pattern, useful for fluent/builder APIs and algebraic
interfaces.

```go
type Adder[A Adder[A]] interface { Add(A) A }

func sum[A Adder[A]](xs ...A) (out A) {
    for _, x := range xs { out = out.Add(x) }
    return out
}
```
Niche — most code never needs it — but it unblocks designs that previously
required `any` plus runtime assertions.
