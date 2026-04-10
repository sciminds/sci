# cp — copy

Copy files or directories.

## Basic Usage

```bash
cp source.txt dest.txt        # copy a file
cp file.txt ~/Desktop/        # copy to a directory
cp file1.txt file2.txt dir/   # copy multiple files into dir/
```

## Common Flags

```bash
cp -r src_dir/ dest_dir/    # recursive — copy entire directory
cp -i file.txt dest.txt     # interactive — prompt before overwrite
cp -n file.txt dest.txt     # no-clobber — never overwrite
cp -v file.txt dest.txt     # verbose — print each file copied
cp -p file.txt dest.txt     # preserve permissions & timestamps
```

## Directory Copying

```bash
# Copy a directory and everything inside it
cp -r projects/app/ projects/app-backup/

# Trailing slash matters:
cp -r src/  dest/    # copies contents of src INTO dest
cp -r src   dest/    # copies the src directory itself into dest
```

## Common Patterns

```bash
# Backup a file before editing
cp config.yaml config.yaml.bak

# Copy with a new name
cp report.pdf report-final.pdf

# Copy matching files
cp *.csv data/
cp src/*.py backup/
```

## Tips

- Always use `-r` for directories — without it, `cp` skips them
- Use `-i` when you're unsure if the destination exists
- `cp` overwrites silently by default — be careful
