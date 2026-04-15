# pwd — print working directory

Show your current location in the filesystem.

## Basic Usage

```bash
pwd
# /Users/alice/projects/my-app
```

## When to Use

- After `cd` to confirm you're where you expect
- In scripts to capture the current path
- When giving someone your current location

## In Scripts

```bash
# Save current directory
HERE=$(pwd)

# Do work elsewhere
cd /tmp
# ...

# Return
cd "$HERE"
```

## Physical vs Logical

```bash
pwd        # logical path (follows symlinks)
pwd -P     # physical path (resolves symlinks)
```

Example with a symlink:

```bash
ln -s /usr/local/bin mylink
cd mylink
pwd        # /Users/alice/mylink
pwd -P     # /usr/local/bin
```

## Tips

- Your shell prompt often shows the current directory already
- `$PWD` is the environment variable holding the same value
