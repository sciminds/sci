# head — show beginning

Display the first lines of a file.

## Basic Usage

```bash
head file.txt          # first 10 lines (default)
head -n 5 file.txt     # first 5 lines
head -n 20 file.txt    # first 20 lines
```

## Multiple Files

```bash
head file1.txt file2.txt    # shows header for each file
head -n 3 *.csv             # first 3 lines of every CSV
```

## Bytes Instead of Lines

```bash
head -c 100 file.txt    # first 100 bytes
head -c 1k file.txt     # first 1 kilobyte
```

## Common Patterns

```bash
# Quick peek at a CSV header
head -n 1 data.csv

# Preview a large log file
head -n 50 server.log

# Check if a file has the right format
head -n 3 config.yaml
```

## Companion: tail

```bash
tail file.txt          # last 10 lines
tail -n 20 file.txt    # last 20 lines
tail -f server.log     # follow — live updates as file grows
```

## Head vs Cat vs Less

```bash
head file.txt    # just the beginning — fast, small output
cat file.txt     # entire file — can flood the terminal
less file.txt    # scrollable pager — best for large files
```

## Tips

- `head -n 1` is great for checking CSV headers
- Pipe into head to limit any command's output: `ls -la | head`
- Use `tail -f` for watching log files in real time
