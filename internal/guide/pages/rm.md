# rm — remove

Delete files (use with caution!).

## Basic Usage

```bash
rm file.txt              # delete a single file
rm file1.txt file2.txt   # delete multiple files
```

## Common Flags

```bash
rm -r directory/     # recursive — delete directory and contents
rm -i file.txt       # interactive — confirm before each delete
rm -v file.txt       # verbose — print each file removed
```

## Deleting Directories

```bash
rm -r my-folder/        # remove folder and everything inside
rmdir empty-folder/     # remove only if empty (safer)
```

## Using Patterns

```bash
rm *.tmp           # delete all .tmp files
rm *.log           # delete all .log files
rm test_*          # delete files starting with test_
```

## Safety Tips

```bash
# Always preview what you'd delete first
ls *.tmp           # see what matches before running rm

# Use -i for interactive confirmation
rm -i important*
# rm: remove regular file 'important.doc'? y/n

# Move to trash instead (macOS)
# Consider using 'trash' command if installed
```

## What You Can't Undo

- There is **no recycle bin** — `rm` is permanent
- Double-check patterns before pressing enter
- Use `git` to protect code files from accidental deletion

## Tips

- Start with `ls` using the same pattern to preview matches
- Use `-i` when deleting anything you're not 100% sure about
- Prefer `rmdir` for empty directories — it won't delete contents
