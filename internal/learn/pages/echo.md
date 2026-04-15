# echo — print text

Print text to the screen or redirect it to a file.

## Basic Usage

```bash
echo "Hello, world!"       # print to terminal
echo Hello world            # quotes optional for simple text
```

## Variables

```bash
echo $HOME                  # print an environment variable
echo "User: $USER"          # embed variable in a string
echo "Path is: $PATH"       # show your PATH
echo "Today is $(date)"     # embed command output
```

## Writing to Files

```bash
# Overwrite file (creates if doesn't exist)
echo "first line" > file.txt

# Append to file
echo "another line" >> file.txt
```

## Common Flags

```bash
echo -n "no newline"    # suppress trailing newline
echo -e "line1\nline2"  # interpret escape sequences
echo -e "col1\tcol2"    # tab character
```

## Escape Sequences (with -e)

```bash
echo -e "line1\nline2"    # \n newline
echo -e "col1\tcol2"      # \t tab
echo -e "wait\b\b\b\bstop"  # \b backspace
```

## Common Patterns

```bash
# Add a header to a new file
echo "# My Notes" > notes.md

# Append a line to a config
echo "alias ll='ls -la'" >> ~/.bashrc

# Create a simple script
echo '#!/bin/bash' > script.sh
echo 'echo "Hello"' >> script.sh

# Print a blank line
echo
```

## Tips

- Use `>` to overwrite, `>>` to append — easy to mix up
- Single quotes `'...'` prevent variable expansion
- Double quotes `"..."` allow variables: `echo "Hi $USER"`
