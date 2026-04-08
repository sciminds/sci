# SciMinds Command Line Toolkit 

A small smart CLI toolkit for academic work on macOS in a single command: `sci`

Helps you setup/verify your computer with common libraries for Python-based scientific data storage, analysis, and writing, by *orchestrating* other tools together.  

Written in Go because:  
- Go is *typed* and *compiled* which makes it much faster than Python/JS *and* makes TDD with LLMs more reliable 
- Easy to create single-file programs that work on any computer
- No complicated dev tooling: everything is pretty standardized in the ecosystem and distribution is just GitHub
- Eshin wanted to learn a new language

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/sciminds/sci/main/install.sh | sh
```

## Goals

- Make hard CLI tools easy to use, e.g. `ffmpeg`
- Make setting up mac with essential software turn-key, e.g. via `brew`
- Include a full data-management solution powered by SQLite
- Provide easy tools to share files and databases across different lab members (private cloud storage via Pocketbase)

## Included Tools 

*Checked and installed automatically via `sci doctor`*

- `brew`: macOS package manager
- `git` / `gh`: Git & GitHub version control
- `uv`: Python project/library management
- `pixi`: Python project/library management
- `bun`: JavaScript/TypeScript runtime
- `node`: JavaScript/TypeScript runtime
- `ffmpeg`: all-in-one tool for editing video/audio files

*Optional tools available via `sci doctor tools`*

- `code`: Visual Studio Code IDE
- `zed`: Zed IDE
- `helix` / `nvim` / `msedit`: terminal editors
- `rg` / `ast-grep` / `jq` / `mq`: search and data tools
- `symbex` / `sqlite-utils` / `markitdown` / `datasette`: Python CLI utilities

## What can it do?

*Run `sci <command> --help` for details on any command*

### Basic Commands

| Command | What it does |
|---------|--------------|
| `sci doctor` | Check that your Mac is set up correctly |
| `sci doctor tools` | Install optional developer tools |
| `sci update` | Update sci to the latest version |
| `sci view <file>` | Browse any data file interactively (CSV, JSON, SQLite) |
| `sci guide basic` | Interactive demos of essential terminal commands |
| `sci guide git` | Interactive demos of essential Git commands |

### Manage Python projects

| Command | What it does |
|---------|--------------|
| `sci proj new` | Create a new Python project |
| `sci proj add` | Add packages to your project |
| `sci proj remove` | Remove packages from your project |
| `sci proj config` | Refresh config files in your project |
| `sci proj preview` | Start a live preview of your documents |
| `sci proj render` | Build documents into HTML or PDF |
| `sci proj run` | Run a project task |
| `sci py repl` | Open a Python scratchpad |
| `sci py marimo` | Open a marimo notebook |
| `sci py tutorials` | Browse and download tutorial notebooks |
| `sci py convert` | Convert between notebook formats (.py, .md, .qmd) |

### Manage databases and spreadsheets

| Command | What it does |
|---------|--------------|
| `sci db info` | Show database metadata and tables |
| `sci db view` | Browse a database or CSV/JSON file interactively |
| `sci db create` | Create a new database |
| `sci db reset` | Start fresh with an empty database |
| `sci db add` | Import CSV files into a database |
| `sci db delete` | Delete a table from a database |
| `sci db rename` | Rename a table |
| `sci db sync` | Sync your default database with the cloud |

The interactive database viewer (`sci db view`) is powered by dbtui (`internal/tui/dbtui/`), also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/dbtui@latest`.

### Ingest markdown files into SQLite (Experimental)

| Command | What it does |
|---------|--------------|
| `sci markdb ingest <dir>` | Ingest a directory of `.md` files into SQLite |
| `sci markdb search --db <db> <query>` | Full-text search across files and frontmatter |
| `sci markdb info --db <db>` | Show database summary statistics |
| `sci markdb diff <dir> --db <db>` | Preview what would change on next ingest |
| `sci markdb export --db <db> --dir <dir>` | Reconstruct original markdown files from the database |

Ingests any folder of markdown files with YAML frontmatter into a single SQLite database. Frontmatter keys become real SQL columns (dynamically discovered), wikilinks and markdown links are tracked in a `links` table, and FTS5 enables full-text search. Export reconstructs byte-identical files from the database.

Also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/markdb@latest`.

### Cloud storage

| Command | What it does |
|---------|--------------|
| `sci cloud auth` | Authenticate via GitHub (requires sciminds org membership) |
| `sci cloud auth --logout` | Clear saved credentials |
| `sci cloud share <file>` | Upload a file to the public bucket |
| `sci cloud share <file> --private` | Upload a file to the private bucket |
| `sci cloud get <name>` | Download a shared file |
| `sci cloud list` | Browse your shared files interactively |
| `sci cloud list --plain` | List your shared files (no TUI) |
| `sci cloud unshare <name>` | Remove a shared file |

All commands accept `--private` / `-p` to target the private bucket instead of the default public one.

### Manage Homebrew packages

| Command | What it does |
|---------|--------------|
| `sci brew list` | Browse installed packages interactively |
| `sci brew add` | Add a package to your Brewfile and install it |
| `sci brew remove` | Remove a package from your Brewfile and uninstall it |
| `sci brew install` | Install all packages from your Brewfile |

### Quickly edit video/audio

| Command | What it does |
|---------|--------------|
| `sci vid info` | Show video info (resolution, duration, etc.) |
| `sci vid cut` | Trim a clip by start and end time |
| `sci vid compress` | Shrink a video file |
| `sci vid convert` | Convert to another format (MP4, WebM, etc.) |
| `sci vid gif` | Turn a video into a GIF |
| `sci vid resize` | Scale a video (720p, 1080p, etc.) |
| `sci vid speed` | Speed up or slow down playback |
| `sci vid mute` | Remove audio from a video |
| `sci vid extract-audio` | Save the audio track to a file |
| `sci vid strip-subs` | Remove subtitles from a video |

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

```bash
# Run the full check suite (fmt, vet, lint, test, build)
just ok
```

*Run `just ok` after every change — it's the single gate for the project!*

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

`sci cloud auth` uses a GitHub OAuth device flow brokered by a Cloudflare Worker. The worker verifies `sciminds` GitHub org membership and returns shared R2 credentials.

**Components:**

1. **GitHub OAuth App** — [sciminds org settings → OAuth Apps](https://github.com/organizations/sciminds/settings/applications). Client ID is compiled into the CLI (`internal/cloud/device.go`). Must have **"Enable Device Flow"** checked.
2. **Cloudflare Worker** (`worker/`) — two endpoints: `POST /auth/device` and `POST /auth/token`. Deployed at `sci-auth.sciminds.workers.dev`.
3. **R2 buckets** — `sci-public` (public read, auth'd write) and `sci-private` (auth'd read/write).

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
bunx wrangler secret put R2_PRIVATE_ACCESS_KEY   # private bucket
bunx wrangler secret put R2_PRIVATE_SECRET_KEY   # private bucket
```

Each command prompts for the value interactively. Get these from the [Cloudflare R2 dashboard](https://dash.cloudflare.com/) → R2 → API Tokens.

**List / delete secrets:**

```bash
bunx wrangler secret list
bunx wrangler secret delete <NAME>
```

**Adding a new bucket:** Create the R2 bucket in Cloudflare, generate an API token scoped to it, set the corresponding `R2_*_ACCESS_KEY` / `R2_*_SECRET_KEY` worker secrets, and update `worker/wrangler.toml` vars if the bucket name differs.
