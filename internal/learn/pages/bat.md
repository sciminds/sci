# bat — better cat

View files with syntax highlighting and line numbers.

## Basic Usage

```bash
bat file.py              # display with syntax highlighting
bat file.py file.js      # view multiple files
```

## Key Features

```bash
bat script.py            # automatic language detection
bat -n file.txt          # line numbers only (no grid)
bat -p file.txt          # plain — no line numbers or header
bat -A file.txt          # show non-printable characters
```

## Themes

```bash
bat --list-themes              # see all available themes
bat --theme="Dracula" file.py  # use a specific theme
```

## Line Ranges

```bash
bat -r 10:20 file.py     # show only lines 10-20
bat -r :5 file.py        # first 5 lines
bat -r 50: file.py       # line 50 to end
```

## Highlighting Lines

```bash
bat -H 5 file.py          # highlight line 5
bat -H 10:15 file.py      # highlight lines 10-15
```

## As a Pager

```bash
# Use bat as a pager for other commands
man ls | bat -l man
git diff | bat

# In git config
git config --global core.pager "bat --style=changes"
```

## bat vs cat

| Feature         | cat    | bat                  |
|-----------------|--------|----------------------|
| Syntax color    | No     | Yes                  |
| Line numbers    | -n     | Always (default)     |
| Paging          | No     | Auto (for long files)|
| Git integration | No     | Shows changed lines  |

## Tips

- `bat` pages automatically for long files (like `less`)
- Set `export BAT_THEME="theme"` in your shell profile
- Use `bat -pp` for completely plain output (good for piping)
