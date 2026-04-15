# ls — list files

Show the contents of a directory.

## Basic Usage

```bash
ls              # list current directory
ls /path/to/dir # list a specific directory
```

## Common Flags

```bash
ls -l           # long format (permissions, size, date)
ls -a           # include hidden files (dotfiles)
ls -la          # both: long format + hidden files
ls -lh          # human-readable sizes (KB, MB, GB)
ls -lt          # sort by modification time (newest first)
ls -lS          # sort by file size (largest first)
ls -R           # list subdirectories recursively
ls -1           # one file per line
```

## Filtering & Patterns

```bash
ls *.txt        # only .txt files
ls *.py         # only .py files
ls data/        # list contents of data/
ls -d */        # list only directories
```

## Reading the Output

```
-rw-r--r--  1 user  staff  4096 Jan 15 10:30 file.txt
│           │  │      │     │    │             └─ name
│           │  │      │     │    └─ last modified
│           │  │      │     └─ size in bytes
│           │  │      └─ group
│           │  └─ owner
│           └─ link count
└─ permissions (type, user, group, other)
```

## Tips

- Use `ls -la` as your go-to for detailed views
- Combine with `grep` to filter: `ls | grep ".py"`
- Alias suggestion: `alias ll='ls -lah'`
