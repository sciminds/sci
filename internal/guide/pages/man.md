# man — manual pages

Read the built-in manual for any command.

## Basic Usage

```bash
man ls         # open the manual for ls
man git        # open the manual for git
man grep       # open the manual for grep
```

## Navigating

| Key           | Action                    |
|---------------|---------------------------|
| `↑` / `↓`    | Scroll one line           |
| `Space`       | Scroll one page down      |
| `b`           | Scroll one page up        |
| `/pattern`    | Search forward            |
| `n`           | Next search match         |
| `N`           | Previous search match     |
| `q`           | Quit                      |

## Sections

Man pages are organized into numbered sections:

| Section | Content                |
|---------|------------------------|
| 1       | User commands          |
| 2       | System calls           |
| 3       | Library functions      |
| 5       | File formats           |
| 8       | System admin commands  |

```bash
man 1 printf    # shell command printf
man 3 printf    # C library printf
man 5 crontab   # crontab file format
```

## Searching for Man Pages

```bash
man -k "copy files"     # search descriptions for keywords
man -k network          # find network-related commands
apropos "copy files"    # same as man -k
```

## Quick Reference

```bash
man -f ls          # one-line description (whatis)
man -k keyword     # search all man pages (apropos)
```

## Getting a Summary

```bash
# Just the description
whatis ls
# ls(1) - list directory contents

# When you don't know the exact command
apropos "disk space"
# df(1) - display free disk space
# du(1) - estimate file space usage
```

## Tips

- Press `/` then type to search within a man page
- Most flags are documented under **OPTIONS** — search for it
- `man man` teaches you how to use man itself
