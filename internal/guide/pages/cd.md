# cd — change directory

Move into a different folder.

## Basic Usage

```bash
cd Documents       # move into Documents/
cd /usr/local/bin  # move to an absolute path
```

## Special Shortcuts

```bash
cd ~          # go to your home directory
cd            # same as cd ~ (no argument)
cd ..         # go up one level (parent directory)
cd ../..      # go up two levels
cd -          # go back to the previous directory
```

## Path Types

```bash
# Absolute path — starts from /
cd /Users/alice/projects

# Relative path — starts from where you are
cd projects/my-app
cd ./projects/my-app    # same thing (explicit)
```

## Tab Completion

Press `Tab` to auto-complete directory names:

```
cd Doc<Tab>        →  cd Documents/
cd ~/Des<Tab>      →  cd ~/Desktop/
```

Double-tap `Tab` to see all matching options.

## Tips

- `pwd` shows where you are after `cd`
- Spaces in names need quotes: `cd "My Folder"`
- Use tab completion to avoid typos
