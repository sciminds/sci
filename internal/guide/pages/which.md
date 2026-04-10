# which — locate command

Show the full path of a command.

## Basic Usage

```bash
which python        # /usr/bin/python
which git           # /usr/bin/git
which node          # /usr/local/bin/node
```

## Why Use It

```bash
# "Which version am I running?"
which python
# /usr/local/bin/python  (Homebrew)
# vs /usr/bin/python     (system)

# "Is this tool installed?"
which rg
# /opt/homebrew/bin/rg   → installed
which unknown
# (no output)            → not found
```

## Multiple Results

```bash
which -a python     # show ALL matching paths, not just first
# /usr/local/bin/python
# /usr/bin/python
```

## Common Patterns

```bash
# Check before using a command
which brew && brew update

# Debug PATH issues
which -a node     # see if multiple versions exist
echo $PATH        # see the search order
```

## Related Commands

```bash
which git        # where the command lives
type git         # how the shell resolves it (alias, builtin, file)
command -v git   # POSIX-portable version of which
whereis git      # binary, source, and man page locations
```

## Understanding PATH

When you type a command, the shell searches each directory in `$PATH`
left to right. `which` shows you which one it finds first:

```bash
echo $PATH
# /usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

which python
# Returns the first match found scanning left to right
```

## Tips

- `which` only finds executables in your PATH, not shell builtins
- Use `type` instead if you want to know about aliases and functions
- No output + exit code 1 means the command isn't installed
