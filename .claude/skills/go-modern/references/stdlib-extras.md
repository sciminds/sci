# Modern stdlib extras

Smaller, more situational stdlib wins. Each notes its version and when to reach
for it. None are "use everywhere" — they're "know they exist so you don't
hand-roll them."

## `log/slog` — structured logging (1.21)

Structured, leveled logging in the stdlib. Levels: `Debug < Info < Warn < Error`.

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
slog.SetDefault(logger)

slog.Info("login", slog.String("user", id), slog.Int("attempts", n))  // typed attrs (vet-checked, no boxing)
slog.Info("login", "user", id, "attempts", n)                          // loose pairs (also valid)

reqLog := logger.With(slog.String("request_id", rid))                  // bind fields once
reqLog.Warn("slow query", slog.Duration("took", d))
```

Handlers: `NewTextHandler` (human/dev), `NewJSONHandler` (machine ingestion).
1.26 adds `slog.NewMultiHandler(...)` to fan out to several handlers.

> **This codebase doesn't log.** CLI output flows through `cmdutil.Result` (JSON
> + human rendering), not a logger — that's deliberate. Reach for `slog` only if
> something changes that (e.g. a long-running `sci ts` daemon). For one-off
> command output, use the `cmdutil.Result` path, not `slog`.

## JSON: `omitzero` (1.24) and json/v2 status

`omitempty` famously fails to omit zero structs — a zero `time.Time` is not
"empty," so `omitempty` always serializes `"0001-01-01T00:00:00Z"`. `omitzero`
fixes this: it omits when the value is the type's zero (using `IsZero()` if the
type defines it).

```go
type Event struct {
    Name string    `json:"name"`
    At   time.Time `json:"at,omitzero"`   // omitted when At.IsZero(); omitempty would NOT omit it
}
```

Guidance: `omitzero` for time/struct/custom-type fields; `omitempty` is still
fine for scalars, slices, and maps. You can combine `omitempty,omitzero`.

> **`encoding/json/v2` is experimental through Go 1.26** (`GOEXPERIMENT=jsonv2`,
> outside the Go 1 compatibility promise, API still changing). Do not depend on
> it in shipping code. Use stable `encoding/json` + `omitzero`. Mention v2 only
> as "coming, not yet stable."

## `os.Root` — sandboxed filesystem access (1.24, expanded 1.25)

Confines all operations to a directory and resists `../` traversal and symlink
escape. The modern answer to "I'm handed an untrusted relative path" (archive
extraction, uploads, user-supplied paths).

```go
root, err := os.OpenRoot(baseDir)
if err != nil { return err }
defer root.Close()

f, err := root.Open(userPath)   // cannot escape baseDir, even via a symlink
```

1.24 methods include `Open`, `Create`, `Mkdir`, `Stat`, `Remove`, `Rename`,
`ReadDir`. 1.25 added `MkdirAll`, `ReadFile`, `WriteFile`, `RemoveAll`,
`Symlink`, `Readlink`, `Chmod`, and `Root.FS()`.

## `math/rand/v2` (1.22)

Auto-seeded (no `rand.Seed`), better algorithms, and a generic `rand.N`.

```go
n := rand.IntN(100)               // was rand.Intn
d := rand.N(5 * time.Minute)      // generic over int-like types, incl. Duration
```
Renames: `Intn→IntN`, `Int31→Int32`, `Int63→Int64`. For cryptographic
randomness, still use `crypto/rand`.

## `unique` — interning (1.23)

Deduplicate repeated comparable values to cut memory and make equality a pointer
compare. Good for high-cardinality-but-repetitive keys (labels, enum-like
strings); pointless for values that are unique per call.

```go
h1 := unique.Make("repeated/label")
h2 := unique.Make("repeated/label")
// h1 == h2 is a cheap pointer comparison; one backing copy is retained
s := h1.Value()   // recover the original
```

## Concurrency

**`sync.WaitGroup.Go` (1.25)** bundles `Add(1)` + `go` + `defer Done()`:

```go
var wg sync.WaitGroup
for _, job := range jobs {
    wg.Go(func() { run(job) })   // fire-and-forget
}
wg.Wait()
```
For **bounded** concurrency or **error collection**, use
`golang.org/x/sync/errgroup` (`SetLimit(n)`, first error cancels) — see the `lo`
skill's concurrency reference. `WaitGroup.Go` only helps the fire-and-forget case.

**Container-aware `GOMAXPROCS` (1.25)** — on Linux the runtime now respects the
cgroup CPU limit and re-reads it live. You can usually drop
`go.uber.org/automaxprocs`. Override with the `GOMAXPROCS` env var or
`runtime.SetDefaultGOMAXPROCS()`.

Also: `sync.Map.Clear()` (1.23), `sync/atomic.And`/`Or` (1.23).

## Testing

**`testing/synctest` — stable since 1.25.** Virtualizes time inside a "bubble"
so tests with timeouts/tickers run instantly and deterministically.

```go
func TestExpiry(t *testing.T) {
    synctest.Test(t, func(t *testing.T) {
        c := NewCache(time.Hour)
        c.Put("k", "v")
        time.Sleep(2 * time.Hour)   // fake clock — returns immediately
        synctest.Wait()             // wait for all bubble goroutines to block
        if _, ok := c.Get("k"); ok { t.Fatal("expected expiry") }
    })
}
```
Use `synctest.Test` (1.25+). The old experimental `synctest.Run` (1.24) was
removed in 1.26. *(Note: this repo's TUI tests use `teatest.WaitFor`, not
`synctest`; `synctest` fits time-driven logic without a Bubble Tea program.)*

**`testing.B.Loop` (1.24)** — replaces `for i := 0; i < b.N; i++`; setup before
the loop runs once and the body is kept alive (no dead-code elimination):

```go
func BenchmarkParse(b *testing.B) {
    data := load()        // runs once, not N times
    for b.Loop() { parse(data) }
}
```

Other testing additions: `t.Context()` and `t.Chdir(dir)` (1.24); `t.Attr`,
`t.Output()` (1.25); `t.ArtifactDir()` (1.26).

## `runtime.AddCleanup` over `SetFinalizer` (1.24)

`AddCleanup` is the modern replacement for `runtime.SetFinalizer`: multiple
cleanups per object, no cycle-leak footgun, no object resurrection.

```go
runtime.AddCleanup(obj, func(fd int) { syscall.Close(fd) }, obj.fd)
```
Caveat: the cleanup's argument must not reference the object being cleaned up, or
it pins the object forever. Prefer this for any new finalizer-like need; treat
`SetFinalizer` as legacy.
