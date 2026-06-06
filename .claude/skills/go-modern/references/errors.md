# Error handling (modern Go)

The codebase already does this well (`errors.Is`/`As` and `%w` wrapping are
near-universal). This is the reference for getting the shape right and for the
two newer additions (`errors.Join` 1.20, `errors.AsType` 1.26).

## Wrap with `%w` to preserve the chain

`fmt.Errorf` with `%w` keeps the underlying error reachable by `Is`/`As`. Use
`%v` when you want to *flatten* (lose) the chain, `%w` when you want to *keep* it.
Add context at each layer.

```go
return fmt.Errorf("load config %q: %w", path, err)
```

Rules:
- **One `%w` per `Errorf`** in the common case. (Multiple `%w` is legal since
  1.20 and produces a multi-error, but if you mean "combine several errors,"
  `errors.Join` is clearer.)
- Wrap to add *context the caller doesn't have* (which file, which key). Don't
  wrap just to restate the operation.

## `errors.Is` — sentinel identity (1.13)

Test whether a known sentinel is anywhere in the chain. Define sentinels with
`errors.New` at package scope.

```go
var ErrNotAuthenticated = errors.New("not authenticated")

if errors.Is(err, ErrNotAuthenticated) { ... }
if errors.Is(err, fs.ErrNotExist) { ... }
if errors.Is(err, context.Canceled) { ... }
```

Never compare error strings (`err.Error() == "..."`) — fragile and unwrappable.

## `errors.As` / `errors.AsType` — typed errors

When you need *fields* off a concrete error type, extract it.

```go
// errors.As (1.13) — needs a pointer to a target variable:
var perr *fs.PathError
if errors.As(err, &perr) {
    log.Print(perr.Path, perr.Op)
}

// errors.AsType[E] (1.26) — generic, returns the value directly. Cleaner:
if perr, ok := errors.AsType[*fs.PathError](err); ok {
    log.Print(perr.Path, perr.Op)
}
```

Prefer `errors.AsType` in new code (1.26+) — no pre-declared target variable, and
the type is explicit at the call site.

### Defining a typed error

Implement `error`; add an `Unwrap()` if it wraps a cause so `Is`/`As` keep
traversing.

```go
type VersionConflictError struct {
    Key      string
    Expected int
    Got      int
}

func (e *VersionConflictError) Error() string {
    return fmt.Sprintf("version conflict on %s: expected %d, got %d", e.Key, e.Expected, e.Got)
}
```

To make a sentinel-style match work on a typed error, give it an `Is` method, or
just match by type with `As`/`AsType`.

## `errors.Join` — accumulate multiple failures (1.20)

For "do all of these, report everything that failed" (validation, batch
processing, cleanup). `Join` is nil-safe — it drops nil arguments and returns nil
if everything was nil.

```go
var errs error
for _, f := range files {
    errs = errors.Join(errs, process(f))   // each nil result is skipped
}
return errs   // nil iff every process(f) succeeded
```

The joined error's message is each error on its own line. `errors.Is` / `As` /
`AsType` traverse into a joined error (it implements `Unwrap() []error` — note
the *slice* return, distinct from single-error `Unwrap() error`).

```go
err := errors.Join(errClosed, fmt.Errorf("flush: %w", errDisk))
errors.Is(err, errClosed)   // true — Is walks all joined branches
```

## Choosing the tool

| Situation | Use |
|---|---|
| Add context, keep the cause reachable | `fmt.Errorf("...: %w", err)` |
| Is this a known sentinel? | `errors.Is(err, ErrFoo)` |
| Need fields off a concrete error type | `errors.AsType[*T](err)` (1.26) or `errors.As` |
| Collect failures from a loop/batch | `errors.Join` |
| Define your own | a type implementing `error` (+ `Unwrap` if it wraps) |

## Anti-patterns

- `if err.Error() == "not found"` → use a sentinel + `errors.Is`.
- `fmt.Errorf("...: %v", err)` when the caller needs to match the cause → use
  `%w`.
- Returning a bare `err` after you learned useful context → wrap it once with
  what you now know.
- Logging *and* returning the same error at every layer → wrap and return; log
  once at the top (or, in this CLI, surface via `cmdutil.Result`).
