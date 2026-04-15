# mkdir — make directory

Create a new folder.

## Basic Usage

```bash
mkdir my-project     # create a single directory
mkdir dir1 dir2 dir3 # create multiple at once
```

## Create Nested Directories

```bash
# Without -p: fails if parent doesn't exist
mkdir projects/new-app/src
# mkdir: projects/new-app: No such file or directory

# With -p: creates all missing parents
mkdir -p projects/new-app/src
```

## Common Patterns

```bash
# Project scaffold
mkdir -p my-app/{src,tests,docs}
# Creates:
#   my-app/
#   my-app/src/
#   my-app/tests/
#   my-app/docs/

# Nested scaffold
mkdir -p my-app/src/{components,utils,styles}
```

## Permissions

```bash
mkdir -m 755 shared    # set permissions on creation
mkdir -m 700 private   # owner-only access
```

## Tips

- Always use `-p` when creating nested paths — it's safe even if dirs exist
- Brace expansion `{a,b,c}` saves typing for multiple sibling dirs
- Combine with `cd`: `mkdir my-project && cd my-project`
