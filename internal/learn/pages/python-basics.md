# Python Basics

## Variables & Types

```python
x = 42              # int
pi = 3.14           # float
name = "Alice"      # str
flag = True         # bool
items = [1, 2, 3]   # list
coords = (1, 2)     # tuple (immutable)
info = {"a": 1}     # dict
unique = {1, 2, 3}  # set
```

## F-Strings

```python
f"Hello {name}, you are {age} years old"
f"{value:.2f}"      # 2 decimal places
f"{value:>10}"      # right-align, width 10
```

## Conditionals

```python
if x > 0:
    print("positive")
elif x == 0:
    print("zero")
else:
    print("negative")
```

## Loops

```python
for item in items:
    print(item)

for i, item in enumerate(items):
    print(i, item)

for key, val in info.items():
    print(key, val)

# List comprehension
squares = [x**2 for x in range(10)]
evens = [x for x in range(10) if x % 2 == 0]
```

## Functions

```python
def greet(name, greeting="Hello"):
    return f"{greeting}, {name}!"

# Lambda
double = lambda x: x * 2

# *args and **kwargs
def flexible(*args, **kwargs):
    print(args)    # tuple of positional args
    print(kwargs)  # dict of keyword args
```

## Common Operations

```python
# Strings
"hello".upper()              # "HELLO"
"hello world".split()        # ["hello", "world"]
", ".join(["a", "b", "c"])   # "a, b, c"
"  spaces  ".strip()         # "spaces"

# Lists
items.append(4)              # add to end
items.extend([5, 6])         # add multiple
items.pop()                  # remove last
sorted(items)                # sorted copy
len(items)                   # length

# Dicts
info.get("key", default)     # safe access
info.keys()                  # all keys
info.values()                # all values
info.update({"b": 2})        # merge
```

## Slicing

```python
items[0]        # first
items[-1]       # last
items[1:3]      # index 1 and 2
items[::2]      # every other
items[::-1]     # reversed
```

## File I/O

```python
# Read
with open("file.txt") as f:
    content = f.read()

# Write
with open("file.txt", "w") as f:
    f.write("hello")
```

## Imports

```python
import math
from pathlib import Path
from collections import Counter, defaultdict
```

## Error Handling

```python
try:
    result = 10 / 0
except ZeroDivisionError as e:
    print(f"Error: {e}")
finally:
    print("always runs")
```
