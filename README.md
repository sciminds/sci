# SciMinds Command Line Toolkit 

A small smart CLI toolkit for academic work on macOS in a single command: `sci`

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/sciminds/sci/main/install.sh | sh
```

Written in Go because:  
- Go is *typed* and *compiled* which makes it much faster than Python/JS *and* makes TDD with LLMs more reliable 
- Easy to create single-file programs that work on any computer
- No complicated dev tooling: everything is pretty standardized in the ecosystem and distribution is just GitHub
- Eshin wanted to learn a new language

## What can it do?

### Getting started

| Command | What it does |
|---------|--------------|
| `sci help` | Interactive help TUI with demos for any command |
| `sci learn` | Interactive guides with terminal demos |
| `sci doctor check` | Check your environment and install missing tools |
| `sci doctor reccs` | Pick optional tools to install |
| `sci update` | Update sci to the latest version |

### Browse data files

| Command | What it does |
|---------|--------------|
| `sci view <file>` | Interactively browse any tabular data file (CSV, JSON, SQLite) |

The interactive data viewer is powered by dbtui (`internal/tui/dbtui/`), also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/dbtui@latest`.

### Manage Python projects

| Command | What it does |
|---------|--------------|
| `sci proj new` | Create a new Python project |
| `sci proj add` | Add packages to the project |
| `sci proj remove` | Remove packages from the project |
| `sci proj config` | Refresh config files in your project |
| `sci proj preview` | Start a live preview server for documents |
| `sci proj render` | Build documents into HTML or PDF |
| `sci proj run` | Run a project task |
| `sci py repl` | Open a Python scratchpad |
| `sci py marimo` | Open a marimo notebook |
| `sci py tutorials` | Browse and run tutorial notebooks in marimo |
| `sci py convert` | Convert between marimo (.py), MyST (.md), and Quarto (.qmd) |

### Manage databases

| Command | What it does |
|---------|--------------|
| `sci db create` | Create an empty database |
| `sci db info` | Show database metadata and tables |
| `sci db add` | Import CSV files into a database |
| `sci db rename` | Rename a table in a database |
| `sci db delete` | Delete a table from a database |
| `sci db reset` | Delete and recreate an empty database |

### Cloud storage

| Command | What it does |
|---------|--------------|
| `sci cloud setup` | Authenticate with GitHub (requires sciminds org membership) |
| `sci cloud put <file>` | Upload a file to cloud storage |
| `sci cloud get <name>` | Download a shared file |
| `sci cloud list` | List all shared files |
| `sci cloud remove <name>` | Remove a shared file |

### Lab storage (SFTP)

| Command | What it does |
|---------|--------------|
| `sci lab setup` | Configure SSH access to lab storage |
| `sci lab ls` | List remote directory contents |
| `sci lab get` | Download a file or directory from lab storage |
| `sci lab put` | Upload a file or directory to your lab space |
| `sci lab browse` | Open an SSH shell in lab storage |

### Manage Homebrew packages

| Command | What it does |
|---------|--------------|
| `sci brew list` | List packages in the Brewfile |
| `sci brew install` | Install packages from the Brewfile, or add and install a new package |
| `sci brew uninstall` | Remove a package from the Brewfile and uninstall it |
| `sci brew update` | Update the Homebrew registry and upgrade outdated packages |

### Video/audio editing

| Command | What it does |
|---------|--------------|
| `sci vid info` | Show video info (resolution, duration, codec, fps, size) |
| `sci vid cut` | Trim a segment (e.g. `0:30 1:00`) |
| `sci vid compress` | Shrink a video file (reduce file size) |
| `sci vid convert` | Convert to another format (MP4, WebM, etc.) |
| `sci vid gif` | Convert to optimized GIF |
| `sci vid resize` | Scale video (720p, 1080p, 4k, 50%, W:H) |
| `sci vid speed` | Change playback speed (e.g. `2` = 2x faster) |
| `sci vid mute` | Remove audio from a video |
| `sci vid extract-audio` | Extract audio track to file |
| `sci vid strip-subs` | Remove subtitles from a video |

### Canvas LMS & GitHub Classroom

| Command | What it does |
|---------|--------------|
| `sci cass setup` | Save your Canvas API token (one-time) |
| `sci cass init` | Create a `cass.yaml` config for a course directory |
| `sci cass pull` | Fetch students, assignments, and submissions from Canvas/GitHub |
| `sci cass status` | Show sync status, pending changes, and discrepancies |
| `sci cass diff` | Show pending grade changes (local or `--remote` 3-way) |
| `sci cass push` | Push grade changes to Canvas |
| `sci cass match` | Interactively match GitHub usernames to Canvas students |
| `sci cass revert` | Discard unpushed grade edits |
| `sci cass log` | Show operation history |
| `sci cass canvas modules` | List, create, publish, or delete course modules |
| `sci cass canvas assignments` | List, create, publish, or delete assignments |
| `sci cass canvas announce` | List, post, or delete announcements |
| `sci cass canvas files` | List course files |

Syncs course data to a local SQLite database (`cass.db`) with a git-like workflow: pull shows changelogs, diff shows pending grade changes, push sends grades to Canvas with conflict detection. GitHub Classroom is optional — works with Canvas-only courses.

### Ingest markdown into SQLite (Experimental)

| Command | What it does |
|---------|--------------|
| `sci markdb ingest <dir>` | Ingest a directory of `.md` files into SQLite |
| `sci markdb search --db <db> <query>` | Full-text search across files |
| `sci markdb info --db <db>` | Show database summary statistics |
| `sci markdb diff <dir> --db <db>` | Show what would change on next ingest |
| `sci markdb export --db <db> --dir <dir>` | Reconstruct markdown files from the database |

Ingests any folder of markdown files with YAML frontmatter into a single SQLite database. Frontmatter keys become real SQL columns (dynamically discovered), wikilinks and markdown links are tracked in a `links` table, and FTS5 enables full-text search. Export reconstructs byte-identical files from the database.

Also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/markdb@latest`.

---

## Releases

Every push to `main` triggers the [Release workflow](.github/workflows/release.yml):

1. **Check** — fmt, vet, lint, test
2. **Build** — cross-compiles `sci`, `dbtui`, and `markdb` for darwin/linux × arm64/amd64
3. **Publish** — uploads all binaries to a rolling `latest` GitHub release

Binaries are named `{tool}-{os}-{arch}` (e.g. `sci-darwin-arm64`, `dbtui-linux-amd64`).

**Updating:** Users run `sci update`, which compares the compiled-in commit SHA against the latest release and atomically replaces the binary if a newer build is available.

**Version:** The `VERSION` file at the repo root is the single source of truth — read by both the Justfile and the CI workflow.

## Development

Prerequisites: [Go 1.23+](https://go.dev/dl/) and [just](https://github.com/casey/just) (`brew install just`).

*This is also setup as a git pre-commit hook:*

```bash
# Run the full check suite (fmt, vet, lint, test, build)
just ok
```


Launch auto-documentation site:

```bash
just docs
```

To try commands during development:

```bash
just run doctor       # same as: go run ./cmd/sci doctor
just run proj new     # etc.
```

### Cloud auth infrastructure

`sci cloud setup` uses a GitHub OAuth device flow brokered by a Cloudflare Worker. The worker verifies `sciminds` GitHub org membership and returns shared R2 credentials.

**Components:**

1. **GitHub OAuth App** — [sciminds org settings → OAuth Apps](https://github.com/organizations/sciminds/settings/applications). Client ID is compiled into the CLI (`internal/cloud/device.go`). Must have **"Enable Device Flow"** checked.
2. **Cloudflare Worker** (`worker/`) — two endpoints: `POST /auth/device` and `POST /auth/token`. Deployed at `sci-auth.sciminds.workers.dev`.
3. **R2 bucket** — `sci-public` (public read, auth'd write).

**Deploy / redeploy the worker:**

```bash
cd worker
bun install
bunx wrangler deploy
```

**Set or rotate worker secrets:**

```bash
cd worker
bunx wrangler secret put GITHUB_CLIENT_SECRET
bunx wrangler secret put R2_ACCOUNT_ID
bunx wrangler secret put R2_ACCESS_KEY          # public bucket
bunx wrangler secret put R2_SECRET_KEY          # public bucket
bunx wrangler secret put R2_PUBLIC_URL           # e.g. https://pub-xxx.r2.dev
```

Each command prompts for the value interactively. Get these from the [Cloudflare R2 dashboard](https://dash.cloudflare.com/) → R2 → API Tokens.

**List / delete secrets:**

```bash
bunx wrangler secret list
bunx wrangler secret delete <NAME>
```

**Adding a new bucket:** Create the R2 bucket in Cloudflare, generate an API token scoped to it, set the corresponding `R2_*_ACCESS_KEY` / `R2_*_SECRET_KEY` worker secrets, and update `worker/wrangler.toml` vars if the bucket name differs.
