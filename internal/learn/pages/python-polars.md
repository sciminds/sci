# Polars (DataFrames)

## Setup

```python
import polars as pl
from polars import col, when, lit
import polars.selectors as cs
```

## Load & Inspect

```python
df = pl.read_csv("data.csv")

df.shape          # (rows, cols)
df.columns        # column names
df.head(5)        # first 5 rows
df.glimpse()      # transposed view
df.describe()     # summary stats
```

## Indexing

```python
df[0, 1]            # single value
df[:, "Name"]       # column by name
df["Name"]          # shorthand
df[0:5, "Name"]     # slice rows + column
```

## Select Columns

```python
df.select(col("accuracy").mean())

df.select(
    acc_mean=col("accuracy").mean(),
    acc_std=col("accuracy").std(),
)

# Multiple columns, same operation
df.select(col("accuracy", "rt").median())

# Rename with .alias()
df.select(col("accuracy").mean().alias("acc_mean"))
```

## Add Columns

```python
df.with_columns(
    acc_mean=col("accuracy").mean(),
    rt_scaled=col("rt") / 100,
)

# Group-wise with .over() (preserves all rows)
df.with_columns(
    acc_mean=col("accuracy").mean().over("participant"),
)
```

## Group & Aggregate

```python
df.group_by("participant", maintain_order=True).agg(
    col("rt").mean(),
    col("accuracy").mean(),
)

# Multiple grouping columns
df.group_by(["participant", "condition"]).agg(
    count=col("accuracy").count(),
)
```

## Filter Rows

```python
df.filter(col("participant") == 1)
df.filter(col("rt").gt(100) & col("rt").lt(500))
df.filter(~col("participant").eq(1))         # negation

# Combine: & (and), | (or), ~ (not)
df.filter(
    col("participant").eq(1) | col("participant").eq(3)
)
```

## Conditional Columns

```python
df.with_columns(
    speed=when(col("rt") >= 300)
        .then(lit("slow"))
        .otherwise(lit("fast"))
)
```

## Summary Expressions

```python
col("x").mean()       col("x").std()
col("x").median()     col("x").count()
col("x").min()        col("x").max()
col("x").sum()        col("x").n_unique()
```

## Reusable Expressions

```python
def zscore(name):
    c = col(name)
    return (c - c.mean()) / c.std()

df.select(acc_z=zscore("accuracy"))

# With .over() for group-wise
df.with_columns(acc_z=zscore("accuracy").over("participant"))
```

## Column Selectors

```python
df.select(cs.numeric().mean())
df.select(cs.exclude("id").std())
df.select(cs.starts_with("obs").count())
df.select(cs.string())
```

## Name Suffix

```python
df.with_columns(
    col("accuracy", "rt").mean().name.suffix("_mean")
)
```

## Reshape

```python
# Wide to long
df.unpivot(
    on=cs.starts_with("trial"),
    index="participant",
    variable_name="trial",
    value_name="score",
)

# Long to wide
df.pivot(on="trial", index="participant", values="score")

# Explode lists
df.explode("numbers")

# Horizontal ops
df.with_columns(avg=pl.mean_horizontal("a", "b", "c"))

# Concat strings
df.with_columns(label=pl.concat_str("month", "year", separator="-"))
```

## String Operations

```python
col("name").str.to_uppercase()
col("name").str.to_lowercase()
col("date").str.split_exact("-", 1).struct.unnest()
```

## Missing Data

```python
col("x").is_null()          # check null
col("x").fill_null(0)       # replace null
col("x").fill_nan(0)        # replace NaN (floats)

# NaN to null, then fill
(col("x") / col("y")).fill_nan(None).fill_null(0)
```

## Concatenate DataFrames

```python
pl.concat([df1, df2, df3])
```

## Sampling

```python
df.sample(fraction=1, shuffle=True)                       # permute
df.sample(fraction=1, shuffle=True, with_replacement=True) # bootstrap
```
