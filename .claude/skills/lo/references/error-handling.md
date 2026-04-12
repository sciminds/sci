# Error Handling — *Err Variants & Utilities

## Pattern: *Err Variants

Most higher-order functions have `*Err` counterparts where the callback returns an error. On the first error, processing stops and the error propagates.

**When to use:** whenever the callback touches I/O, does validation, or can fail for any reason.

```go
// Regular — callback cannot fail
results := lo.Map(urls, func(url string, _ int) string {
    return strings.TrimSpace(url)
})

// *Err — callback can fail, short-circuits on first error
results, err := lo.MapErr(urls, func(url string, _ int) (Response, error) {
    resp, err := http.Get(url)
    if err != nil {
        return Response{}, fmt.Errorf("fetching %s: %w", url, err)
    }
    return parseResponse(resp)
})
```

## Complete List of *Err Variants

### Slice transforms
| Regular | *Err variant |
|---|---|
| `lo.Map` | `lo.MapErr` |
| `lo.Filter` | `lo.FilterErr` |
| `lo.FlatMap` | `lo.FlatMapErr` |
| `lo.Reduce` | `lo.ReduceErr` |
| `lo.ReduceRight` | `lo.ReduceRightErr` |
| `lo.Reject` | `lo.RejectErr` |
| `lo.UniqBy` | `lo.UniqByErr` |
| `lo.GroupBy` | `lo.GroupByErr` |
| `lo.GroupByMap` | `lo.GroupByMapErr` |
| `lo.PartitionBy` | `lo.PartitionByErr` |
| `lo.KeyBy` | `lo.KeyByErr` |
| `lo.CountBy` | `lo.CountByErr` |
| `lo.SumBy` | `lo.SumByErr` |
| `lo.ProductBy` | `lo.ProductByErr` |
| `lo.MeanBy` | `lo.MeanByErr` |
| `lo.MinBy` | `lo.MinByErr` |
| `lo.MaxBy` | `lo.MaxByErr` |
| `lo.MinIndexBy` | `lo.MinIndexByErr` |
| `lo.MaxIndexBy` | `lo.MaxIndexByErr` |
| `lo.RepeatBy` | `lo.RepeatByErr` |
| `lo.Find` | `lo.FindErr` |
| `lo.FindDuplicatesBy` | `lo.FindDuplicatesByErr` |
| `lo.WithoutBy` | `lo.WithoutByErr` |
| `lo.EarliestBy` | `lo.EarliestByErr` |
| `lo.LatestBy` | `lo.LatestByErr` |

### Map transforms
| Regular | *Err variant |
|---|---|
| `lo.MapKeys` | `lo.MapKeysErr` |
| `lo.MapValues` | `lo.MapValuesErr` |
| `lo.MapEntries` | `lo.MapEntriesErr` |
| `lo.MapToSlice` | `lo.MapToSliceErr` |
| `lo.FilterMapToSlice` | `lo.FilterMapToSliceErr` |
| `lo.FilterKeys` | `lo.FilterKeysErr` |
| `lo.FilterValues` | `lo.FilterValuesErr` |
| `lo.PickBy` | `lo.PickByErr` |
| `lo.OmitBy` | `lo.OmitByErr` |

### Tuples
| Regular | *Err variant |
|---|---|
| `lo.ZipBy2` | `lo.ZipByErr2` |
| `lo.UnzipBy2` | `lo.UnzipByErr2` |
| `lo.CrossJoinBy2` | `lo.CrossJoinByErr2` |

## Error Utilities

### Validate

Returns an error if condition is false, nil otherwise.

```go
err := lo.Validate(len(slice) > 0, "slice must not be empty")
err := lo.Validate(age >= 18, "must be %d+, got %d", 18, age)
```

### Must / Must0-Must6

Wraps a function call, panics if the last return value is an error (or false for bool).
Use at init-time or when failure is truly unrecoverable.

```go
val := lo.Must(time.Parse("2006-01-02", "2022-01-15"))
val1, val2 := lo.Must2(strconv.ParseInt("42", 10, 64))
lo.Must0(os.Setenv("KEY", "value"))
```

### Try / Try0-Try6

Call a function, return false if it panics or returns an error.

```go
ok := lo.Try(func() error {
    return riskyOperation()
})
```

### TryOr / TryOr0-TryOr6

Call a function, return default value on panic or error.

```go
result, ok := lo.TryOr(func() (string, error) {
    return riskyFetch()
}, "fallback")
// "fallback", false  (if riskyFetch panicked or errored)
```

### TryCatch / TryCatchWithErrorValue

```go
lo.TryCatch(func() error {
    return riskyOp()
}, func() {
    log.Println("caught a panic or error")
})
```

### ErrorsAs

Generic shortcut for `errors.As` — no need to declare a target variable.

```go
if rateLimitErr, ok := lo.ErrorsAs[*RateLimitError](err); ok {
    time.Sleep(rateLimitErr.RetryAfter)
}
```

### Assert / Assertf

Panic when invariant is violated. For defensive programming, not user input validation.

```go
lo.Assert(len(items) > 0, "items must not be empty")
lo.Assertf(idx < len(items), "index %d out of bounds (len=%d)", idx, len(items))
```
