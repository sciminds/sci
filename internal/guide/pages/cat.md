# cat — concatenate and print

Display the contents of a file in the terminal.

## Basic Usage

```bash
cat file.txt           # print entire file
cat file1.txt file2.txt  # print multiple files in sequence
```

## Common Flags

```bash
cat -n file.txt    # show line numbers
cat -b file.txt    # number only non-blank lines
cat -s file.txt    # squeeze repeated blank lines
```

## Creating & Appending

```bash
# Write text to a new file (overwrite)
cat > notes.txt
Type your text here.
Press Ctrl+D when done.

# Append to an existing file
cat >> notes.txt
More text added at the end.
Ctrl+D
```

## Combining Files

```bash
# Merge files into one
cat header.txt body.txt footer.txt > full.txt

# Append one file to another
cat extra.txt >> existing.txt
```

## Alternatives for Large Files

```bash
less file.txt    # scrollable pager (q to quit)
head file.txt    # first 10 lines
tail file.txt    # last 10 lines
bat file.txt     # cat with syntax highlighting
```

## Tips

- `cat` dumps the whole file — use `less` for long files
- `cat -n` is handy for debugging line-specific errors
- Avoid `cat file | grep` — use `grep pattern file` directly
