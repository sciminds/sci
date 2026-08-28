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
> that changes: it would take a long-running daemon, and every sci verb is a
> one-shot command. For ordinary command output, use the `cmdutil.Result` path,
> not `slog`.

## JSON: `omitzero` (1.24) and json/v2 (stable 1.27)

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

**`encoding/json/v2` (stable since 1.27)** is the major revision of
`encoding/json`. In practice:

- **Existing v1 code just gets faster.** `encoding/json` is now backed by the
  v2 implementation — behavior preserved, unmarshal significantly faster. No
  migration required; the v1 API remains supported indefinitely. (Only the
  exact *text* of error messages may differ — don't assert on it.)
- **The v2 API** (`encoding/json/v2`) offers `Marshal`/`Unmarshal` plus
  streaming variants (`MarshalWrite`/`UnmarshalRead`,
  `MarshalEncode`/`UnmarshalDecode`), all taking variadic `Options`:

  ```go
  data, err := json.Marshal(v, json.Deterministic(true))       // v2: options per call
  err = json.UnmarshalRead(r, &v)                              // stream from io.Reader
  ```
- **v2 defaults are stricter and more interoperable than v1**: invalid UTF-8
  in strings and duplicate object names are rejected instead of silently
  accepted. Per-call `Options` can restore v1 semantics piecemeal; the v1
  package docs list every behavioral difference.
- `encoding/json/jsontext` is the low-level layer: `Encoder`/`Decoder` over
  `Token`/`Value` with a validity-enforcing state machine — reach for it for
  streaming transforms where you don't want full struct binding.

For new code, prefer the v2 API when you need options, streaming, or the
stricter validation; plain v1 `json.Marshal`/`Unmarshal` remains fine for
simple cases.

## `uuid` — UUIDs in the stdlib (1.27)

Retires the `github.com/google/uuid` dependency for the common cases:

```go
id := uuid.New()               // v4 (random) — "no specific algorithm needed" default
id7 := uuid.NewV7()            // v7 (timestamp-prefixed) — prefer for DB primary keys (index locality)
u, err := uuid.Parse(s)        // parse; MustParse panics for constants
_ = id.String()                // canonical "xxxxxxxx-xxxx-..." form
```

`UUID` implements `TextMarshaler`/`TextUnmarshaler`, so it round-trips through
JSON and database drivers as text. `uuid.Nil()` and `uuid.Max()` return the
all-zeros / all-ones values — note they're **functions** here, unlike
`google/uuid`'s variables, so `id == uuid.Nil` won't compile; compare against
`uuid.Nil()`. `Compare` gives ordering. Use the stdlib package in new code;
keep `google/uuid` only where its extra versions (v1/v3/v5) are needed.

## `strings.CutLast` / `bytes.CutLast` (1.27)

Same shape as `strings.Cut` (1.18) but around the **last** occurrence of the
separator — replaces the `LastIndex` + manual-slicing dance:

```go
// Before:
i := strings.LastIndex(path, "/")
dir, file := path[:i], path[i+1:]      // and an easy off-by-one when i == -1

// 1.27:
dir, file, ok := strings.CutLast(path, "/")
```

Reflex: any `LastIndex` immediately followed by slicing is a `CutLast`. (And
any `Index` + slicing is a `Cut`.)

## `net/url` clones (1.27)

`URL.Clone()` and `Values.Clone()` produce deep copies — use them instead of
manual struct copies that forget `User` or share the underlying `Values` map:

```go
u2 := u.Clone()
q := u.Query()          // already a copy
v2 := vals.Clone()      // explicit deep copy of url.Values
```

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

1.24 methods include `Open`, `Create`, `Mkdir`, `Stat`, `Remove`, and
`Root.FS()`. 1.25 added `Rename`, `MkdirAll`, `ReadFile`, `WriteFile`,
`RemoveAll`, `Symlink`, `Readlink`, and `Chmod`. There is no `Root.ReadDir` —
list directories via `fs.ReadDir(root.FS(), dir)` or a `File.ReadDir`.

## `math/rand/v2` (1.22)

Auto-seeded (no `rand.Seed`), better algorithms, and a generic `rand.N`.

```go
n := rand.IntN(100)               // was rand.Intn
d := rand.N(5 * time.Minute)      // generic over int-like types, incl. Duration
r := rand.New(rand.NewPCG(1, 2))
x := r.N(int32(10))               // 1.27: Rand.N generic METHOD mirrors top-level N
```
Renames: `Intn→IntN`, `Int31→Int32`, `Int63→Int64`. For cryptographic
randomness, still use `crypto/rand`. (`Rand.N` is also the flagship example of
1.27 generic methods — see language.md.)

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
removed in 1.26. 1.27 adds `synctest.Sleep(d)`, which combines `time.Sleep` +
`synctest.Wait` — the common "advance the clock, then let everything settle"
step becomes one call. *(Note: this repo's TUI tests use `teatest.WaitFor`, not
`synctest`; `synctest` fits time-driven logic without a Bubble Tea program.)*

**`httptest.NewTestServer` (1.27)** — the modern default for HTTP handler
tests. Compared to `NewServer`: it takes the `*testing.T`, shuts down via
`t.Cleanup` (no manual `defer server.Close()`), fails the test on handler
panics, and runs on an **in-memory fake network** — no real ports, and it
composes with `synctest` so client-timeout tests run instantly:

```go
server := httptest.NewTestServer(t, handler)
resp, err := server.Client().Get("http://example.com/")   // any host routes to the server
```
Don't call `Start`/`StartTLS` on it. Reach for the old `NewServer` only when
something outside the test process must dial the port for real.

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

## Goroutine leak profiles (stable 1.27)

The runtime now detects **leaked goroutines** — goroutines blocked on a channel,
`sync.Mutex`, `sync.Cond`, etc. that can provably never be unblocked (the GC
checks whether the primitive is reachable from any runnable goroutine). Grab the
profile when a service's goroutine count creeps up:

```go
pprof.Lookup("goroutineleak").WriteTo(os.Stdout, 1)
// or, with net/http/pprof:  GET /debug/pprof/goroutineleak
```

Caveat: reachability-based, so leaks via primitives still reachable from globals
or runnable goroutines' locals can go undetected — absence of leaks in the
profile is not proof of absence.

## `runtime.AddCleanup` over `SetFinalizer` (1.24)

`AddCleanup` is the modern replacement for `runtime.SetFinalizer`: multiple
cleanups per object, no cycle-leak footgun, no object resurrection.

```go
runtime.AddCleanup(obj, func(fd int) { syscall.Close(fd) }, obj.fd)
```
Caveat: the cleanup's argument must not reference the object being cleaned up, or
it pins the object forever. Prefer this for any new finalizer-like need; treat
`SetFinalizer` as legacy.
