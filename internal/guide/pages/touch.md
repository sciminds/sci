# touch — create file

Create an empty file or update its timestamp.

## Basic Usage

```bash
touch newfile.txt            # create an empty file
touch file1.txt file2.txt    # create multiple files at once
```

## What Touch Actually Does

1. If the file **doesn't exist** → creates an empty file
2. If the file **already exists** → updates its modification timestamp

```bash
touch existing.txt    # no data lost — just updates the date
ls -l existing.txt    # timestamp is now "just now"
```

## Common Patterns

```bash
# Create a placeholder
touch README.md
touch .gitkeep          # keep empty dirs in git

# Create several files at once
touch src/{main,utils,config}.py

# Create with a specific extension
touch notes.md todo.txt data.csv
```

## Timestamp Options

```bash
# Set a specific timestamp
touch -t 202401151030 file.txt    # Jan 15, 2024 10:30

# Copy timestamp from another file
touch -r reference.txt target.txt

# Update only access time
touch -a file.txt

# Update only modification time
touch -m file.txt
```

## Touch vs Echo vs Cat

```bash
touch file.txt          # empty file, no content
echo "" > file.txt      # file with a blank line
cat > file.txt          # interactive — type content, Ctrl+D to save
```

## Tips

- `touch` is the cleanest way to create empty files
- It never modifies file contents — safe on existing files
- Use brace expansion for batch creation: `touch test_{1..5}.txt`
