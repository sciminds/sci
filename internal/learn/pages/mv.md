# mv — move / rename

Move or rename files and directories.

## Renaming

```bash
mv old_name.txt new_name.txt     # rename a file
mv my-dir/ better-name/         # rename a directory
```

## Moving

```bash
mv file.txt ~/Documents/        # move to another directory
mv file.txt ~/Documents/f.txt   # move and rename in one step
mv *.py src/                    # move all .py files into src/
mv dir1/ dir2/ archive/         # move multiple dirs at once
```

## Common Flags

```bash
mv -i src.txt dest.txt    # interactive — prompt before overwrite
mv -n src.txt dest.txt    # no-clobber — never overwrite
mv -v src.txt dest.txt    # verbose — print what's happening
```

## Rename vs Move

```bash
# Same directory = rename
mv report.txt report-final.txt

# Different directory = move
mv report.txt ~/Desktop/

# Both at once
mv report.txt ~/Desktop/report-final.txt
```

## Common Patterns

```bash
# Organize files by type
mv *.jpg photos/
mv *.pdf documents/

# Undo an accidental rename
mv new_name.txt old_name.txt
```

## Tips

- `mv` works on directories without `-r` (unlike `cp`)
- Moving within the same filesystem is instant (just renames the pointer)
- `mv` overwrites silently by default — use `-i` to be safe
