# Concurrency — Parallel Variants, Channels, Async, Retry

## Parallel Variants (lop)

Import: `lop "github.com/samber/lo/parallel"`

Run callbacks in goroutines. Results preserve order. **Only use for I/O-bound work** — goroutine overhead makes CPU-bound transforms slower than sequential `lo.Map`.

```go
// Parallel Map — fetch URLs concurrently
results := lop.Map(urls, func(url string, _ int) (*Response, error) {
    return http.Get(url)
})

// Parallel ForEach — fire-and-forget side effects
lop.ForEach(items, func(item Item, _ int) {
    sendNotification(item)
})

// Parallel GroupBy
groups := lop.GroupBy(items, func(i Item) string { return i.Category })

// Parallel Times
results := lop.Times(10, func(i int) Result { return expensiveCompute(i) })

// Parallel PartitionBy
partitions := lop.PartitionBy(items, func(i Item) string { return i.Status })
```

## Mutable Variants (lom)

Import: `lom "github.com/samber/lo/mutable"`

Update slices in place (avoids allocation). Only when you own the slice and don't need the original.

```go
lom.Filter(slice, func(x int, _ int) bool { return x > 0 })   // modifies slice
lom.Map(slice, func(x int, _ int) int { return x * 2 })        // same-type only
lom.Shuffle(slice)                                               // Fisher-Yates
lom.Reverse(slice)
```

## Channel Helpers

### SliceToChannel / ChannelToSlice

```go
ch := lo.SliceToChannel(2, items)      // buffered channel, sends items
slice := lo.ChannelToSlice(ch)         // blocks until channel closes
```

### Generator

```go
ch := lo.Generator(2, func(yield func(int)) {
    yield(1)
    yield(2)
    yield(3)
})
// <-chan int, buffered at 2
```

### FanIn / FanOut

```go
// Merge multiple channels into one
merged := lo.FanIn(100, ch1, ch2, ch3)

// Broadcast one channel to N consumers
outputs := lo.FanOut(5, 100, inputCh)
// returns [5]<-chan T
```

### Buffer / BufferWithTimeout / BufferWithContext

Batch items from a channel.

```go
items, length, readTime, ok := lo.Buffer(ch, 10)
items, length, readTime, ok := lo.BufferWithTimeout(ch, 10, 100*time.Millisecond)
items, length, readTime, ok := lo.BufferWithContext(ctx, ch, 10)
```

### ChannelDispatcher

Distribute items from one channel across N worker channels.

```go
children := lo.ChannelDispatcher(ch, 5, 10, lo.DispatchingStrategyRoundRobin[int])
```

Strategies: `RoundRobin`, `Random`, `WeightedRandom`, `First`, `Least`, `Most`.

## Retry / Attempt

### Attempt

Retry a function up to N times.

```go
iterations, err := lo.Attempt(5, func(i int) error {
    return connectToDB()
})
```

### AttemptWithDelay

Retry with fixed delay between attempts.

```go
iterations, duration, err := lo.AttemptWithDelay(5, 2*time.Second, func(i int, d time.Duration) error {
    return connectToDB()
})
```

### AttemptWhile / AttemptWhileWithDelay

Retry while callback returns `(error, shouldContinue)`.

```go
iterations, err := lo.AttemptWhile(10, func(i int) (error, bool) {
    err := tryConnect()
    if errors.Is(err, ErrFatal) {
        return err, false  // stop retrying
    }
    return err, true  // keep trying
})
```

## Debounce / Throttle

### Debounce

```go
trigger, cancel := lo.NewDebounce(100*time.Millisecond, func() {
    fmt.Println("debounced!")
})
trigger()  // resets timer each call
trigger()  // only the last call fires after 100ms of quiet
defer cancel()
```

### DebounceBy

Per-key debouncing.

```go
trigger, cancel := lo.NewDebounceBy(100*time.Millisecond, func(key string, count int) {
    fmt.Printf("key=%s called %d times\n", key, count)
})
trigger("user-1")
trigger("user-2")
```

### Throttle

Rate-limit calls.

```go
throttle, reset := lo.NewThrottle(100*time.Millisecond, func() {
    fmt.Println("throttled!")
})
throttle()  // fires immediately
throttle()  // suppressed until window passes
```

## Async

Fire-and-forget with channel result.

```go
ch := lo.Async(func() int { return expensiveWork() })
result := <-ch

// Multiple return values
ch := lo.Async2(func() (int, error) { return fetchData() })
tuple := <-ch  // lo.Tuple2[int, error]
```

Variants: `lo.Async0` through `lo.Async6`.

## Synchronize

Mutex wrapper for sequential access.

```go
s := lo.Synchronize()
go s.Do(func() { /* critical section 1 */ })
go s.Do(func() { /* critical section 2 */ })
```

## Transaction (Saga pattern)

Chain operations with automatic rollback.

```go
result, err := lo.NewTransaction[State]().
    Then(
        func(state State) (State, error) { /* step 1 */ },
        func(state State) State { /* rollback 1 */ },
    ).
    Then(
        func(state State) (State, error) { /* step 2 */ },
        func(state State) State { /* rollback 2 */ },
    ).
    Process(initialState)
```

## WaitFor / WaitForWithContext

Poll a condition.

```go
count, duration, ok := lo.WaitFor(
    func(i int) bool { return isReady() },
    10*time.Millisecond,   // max wait
    2*time.Millisecond,    // poll interval
)
```
