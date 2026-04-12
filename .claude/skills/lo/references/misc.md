# Miscellaneous — Tuples, Math, Strings, Time, Pointers, Conditionals

## Tuples

### Create / Unpack

```go
t := lo.T2("hello", 42)          // Tuple2[string, int]{A: "hello", B: 42}
a, b := lo.Unpack2(t)             // "hello", 42
a, b := t.Unpack()                // same thing

// Up to 9 elements: lo.T3, lo.T4, ..., lo.T9
t3 := lo.T3("x", 1, true)        // Tuple3[string, int, bool]
```

### Zip / Unzip

Pair up parallel slices.

```go
tuples := lo.Zip2(names, scores)
// []Tuple2[string, int]{{A: "Alice", B: 95}, {A: "Bob", B: 87}}

names, scores := lo.Unzip2(tuples)
// []string{"Alice", "Bob"}, []int{95, 87}
```

### ZipBy / UnzipBy

Custom pairing/splitting.

```go
items := lo.ZipBy2(names, scores, func(name string, score int) string {
    return fmt.Sprintf("%s: %d", name, score)
})

names, lengths := lo.UnzipBy2(words, func(w string) (string, int) {
    return w, len(w)
})
```

### CrossJoin

Cartesian product.

```go
lo.CrossJoin2([]string{"a", "b"}, []int{1, 2})
// []Tuple2: {a,1}, {a,2}, {b,1}, {b,2}
```

## Math

### Sum / SumBy

```go
lo.Sum([]int{1, 2, 3, 4, 5})                                         // 15
lo.SumBy([]string{"foo", "bar"}, func(s string) int { return len(s) })  // 6
```

### Product / ProductBy

```go
lo.Product([]int{1, 2, 3, 4, 5})                // 120
```

### Mean / MeanBy

```go
lo.Mean([]float64{2, 3, 4, 5})                  // 3.5
```

### Mode

Most frequent value(s).

```go
lo.Mode([]int{2, 2, 3, 4})           // []int{2}
lo.Mode([]float64{2, 2, 3, 3})       // []float64{2, 3}  (tie)
```

### Range / RangeFrom / RangeWithSteps

```go
lo.Range(4)                            // []int{0, 1, 2, 3}
lo.Range(-4)                           // []int{0, -1, -2, -3}
lo.RangeFrom(1, 5)                     // []int{1, 2, 3, 4, 5}
lo.RangeWithSteps(0, 20, 5)            // []int{0, 5, 10, 15}
lo.RangeWithSteps[float64](0.0, 1.0, 0.2)  // []float64{0.0, 0.2, 0.4, 0.6, 0.8}
```

### Clamp

```go
lo.Clamp(42, -10, 10)                 // 10
lo.Clamp(-42, -10, 10)                // -10
lo.Clamp(5, -10, 10)                  // 5
```

## String Helpers

```go
lo.RandomString(10, lo.LettersCharset)      // random string
lo.Substring("hello", 2, 3)                // "llo"
lo.ChunkString("123456", 2)                // []string{"12", "34", "56"}
lo.RuneLength("hello")                      // 5 (rune-aware, unlike len())

// Case conversion
lo.PascalCase("hello_world")               // "HelloWorld"
lo.CamelCase("hello_world")                // "helloWorld"
lo.KebabCase("helloWorld")                 // "hello-world"
lo.SnakeCase("HelloWorld")                 // "hello_world"

lo.Words("helloWorld")                     // []string{"hello", "world"}
lo.Capitalize("heLLO")                     // "Hello"
lo.Ellipsis("Lorem Ipsum", 5)             // "Lo..."
```

## Time

### Duration

Measure execution time.

```go
elapsed := lo.Duration(func() {
    expensiveWork()
})
// time.Duration

result, elapsed := lo.Duration1(func() int {
    return compute()
})

str, n, err, elapsed := lo.Duration3(func() (string, int, error) {
    return fetch()
})
```

## Pointer Helpers

### ToPtr / FromPtr / FromPtrOr

```go
ptr := lo.ToPtr("hello")              // *string → "hello"
val := lo.FromPtr(ptr)                // string → "hello"
val := lo.FromPtrOr(nilPtr, "default")  // safe deref with fallback
```

### Nil / EmptyableToPtr

```go
lo.Nil[float64]()                      // nil *float64
lo.EmptyableToPtr("")                   // nil *string (zero value → nil)
lo.EmptyableToPtr("hello")             // *string → "hello"
```

### ToSlicePtr / FromSlicePtr / FromSlicePtrOr

```go
lo.ToSlicePtr([]string{"a", "b"})         // []*string
lo.FromSlicePtr([]*string{&s1, nil})      // []string{"hello", ""}
lo.FromSlicePtrOr([]*string{nil}, "fb")   // []string{"fb"}
```

### ToAnySlice / FromAnySlice

```go
lo.ToAnySlice([]int{1, 2, 3})              // []any{1, 2, 3}
ints, ok := lo.FromAnySlice[int](anySlice)  // []int, true
```

## Conditionals

### Ternary / TernaryF

```go
lo.Ternary(true, "a", "b")                 // "a"
lo.TernaryF(true, expensiveA, expensiveB)   // lazy — only evaluates the winner
```

### If / ElseIf / Else

Chain for multi-branch:
```go
result := lo.
    If(score >= 90, "A").
    ElseIf(score >= 80, "B").
    ElseIf(score >= 70, "C").
    Else("F")
```

Lazy variants: `lo.IfF`, `ElseIfF`, `ElseF`

### Switch / Case / Default

```go
result := lo.Switch[int, string](statusCode).
    Case(200, "OK").
    Case(404, "Not Found").
    Case(500, "Internal Error").
    Default("Unknown")
```

Lazy variants: `CaseF`, `DefaultF`

## Type Inspection

### IsNil / IsNotNil

Correctly handles the nil-interface gotcha in Go.

```go
var x any = (*string)(nil)
x == nil         // false  (Go gotcha!)
lo.IsNil(x)     // true   (correct)
lo.IsNotNil(x)  // false
```

### Empty / IsEmpty / IsNotEmpty

```go
lo.Empty[int]()          // 0
lo.Empty[string]()       // ""
lo.IsEmpty(0)            // true
lo.IsEmpty("")           // true
lo.IsNotEmpty("hello")   // true
```

### Coalesce / CoalesceOrEmpty

First non-zero value.

```go
result, ok := lo.Coalesce(0, 0, 42, 100)   // 42, true
result := lo.CoalesceOrEmpty(0, 0, 42)      // 42

// Collection variants:
result, ok := lo.CoalesceSlice(nil, []int{}, []int{1, 2})  // [1, 2], true
result, ok := lo.CoalesceMap(nil, map[string]int{"a": 1})   // {"a": 1}, true
```

## Partial Application

```go
add := func(a, b int) int { return a + b }
add5 := lo.Partial(add, 5)
add5(10)  // 15

// Up to 5 args: lo.Partial2, lo.Partial3, lo.Partial4, lo.Partial5
```
