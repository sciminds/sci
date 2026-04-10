# grep — search text

Find lines matching a pattern in files.

## Basic Usage

```bash
grep "hello" file.txt          # find lines containing "hello"
grep "error" *.log             # search across multiple files
grep "TODO" -r src/            # search recursively in a directory
```

## Common Flags

```bash
grep -i "error" file.txt     # case-insensitive
grep -n "error" file.txt     # show line numbers
grep -c "error" file.txt     # count matching lines
grep -l "error" *.py         # list only filenames with matches
grep -r "error" .            # recursive search in current dir
grep -v "debug" file.txt     # invert — show non-matching lines
grep -w "main" file.txt      # whole word only (not "mainly")
```

## Combining Flags

```bash
# Case-insensitive, with line numbers, recursive
grep -inr "todo" src/

# Count matches per file
grep -rc "import" src/
```

## Patterns

```bash
grep "^Start" file.txt      # lines starting with "Start"
grep "end$" file.txt         # lines ending with "end"
grep "err[0-9]" file.txt     # "err" followed by a digit
grep -E "cat|dog" file.txt   # extended regex — "cat" or "dog"
```

## Context Lines

```bash
grep -A 3 "error" file.txt   # show 3 lines After each match
grep -B 2 "error" file.txt   # show 2 lines Before
grep -C 2 "error" file.txt   # show 2 lines of Context (both)
```

## Piping

```bash
cat file.txt | grep "pattern"   # filter output (prefer below)
grep "pattern" file.txt         # direct — more efficient

# Chain filters
ps aux | grep "python"          # find running python processes
history | grep "git push"       # search command history
```

## Tips

- Use `-r` liberally — searching whole directories is common
- Combine `-in` (case-insensitive + line numbers) as a default
- For complex searches, try `rg` (ripgrep) — faster and friendlier
