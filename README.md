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
| `sci doctor` | Check that your Mac is set up correctly |
| `sci update` | Update sci to the latest version |

![sci doctor](docs/casts/sci-doctor.gif)

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
| `sci py convert` | Convert between marimo (.py), MyST (.md), and Quarto (.qmd) |

![sci proj](docs/casts/sci-proj.gif)

### Manage databases

| Command | What it does |
|---------|--------------|
| `sci db create` | Create an empty database |
| `sci db info` | Show database metadata and tables |
| `sci db add` | Import CSV files into a database |
| `sci db rename` | Rename a table in a database |
| `sci db delete` | Delete a table from a database |
| `sci db reset` | Delete and recreate an empty database |

![sci db](docs/casts/sci-db.gif)

### Cloud storage

| Command | What it does |
|---------|--------------|
| `sci cloud setup` | Authenticate with GitHub (requires sciminds org membership) |
| `sci cloud put <file>` | Upload a file to cloud storage |
| `sci cloud get <name>` | Download a shared file |
| `sci cloud list` | List all shared files |
| `sci cloud remove <name>` | Remove a shared file |

![sci cloud](docs/casts/sci-cloud.gif)

### Lab storage (sftp)

| Command | What it does |
|---------|--------------|
| `sci lab setup` | Configure SSH access to lab storage |
| `sci lab ls` | List remote directory contents |
| `sci lab get` | Download a file or directory from lab storage |
| `sci lab put` | Upload a file or directory to your lab space |
| `sci lab browse` | Open an SSH shell in lab storage |

![sci lab](docs/casts/sci-lab.gif)

### Manage tools (Homebrew & uv)

| Command | What it does |
|---------|--------------|
| `sci tools list` | List packages in the Brewfile |
| `sci tools install` | Install packages from the Brewfile, or add and install a new package |
| `sci tools uninstall` | Remove a package from the Brewfile and uninstall it |
| `sci tools update` | Update the Homebrew registry and upgrade outdated packages |
| `sci tools reccs` | Pick optional tools to install |

![sci tools](docs/casts/sci-tools.gif)

### Video/audio editing

| Command | What it does |
|---------|--------------|
| `sci vid info` | Show video info (resolution, duration, codec, fps, size) |
| `sci vid cut` | Trim a segment (e.g. `0:30 1:00`) |
| `sci vid compress` | Shrink a video file (reduce file size) |
| `sci vid convert` | Convert to another format (mp4, webm, etc.) |
| `sci vid gif` | Convert to optimized GIF |
| `sci vid resize` | Scale video (720p, 1080p, 4k, 50%, W:H) |
| `sci vid speed` | Change playback speed (e.g. `2` = 2x faster) |
| `sci vid mute` | Remove audio from a video |
| `sci vid extract-audio` | Extract audio track to file |
| `sci vid strip-subs` | Remove subtitles from a video |

![sci vid](docs/casts/sci-vid.gif)

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

![sci cass](docs/casts/sci-cass.gif)

### Zotero library management (Experimental)

| Command | What it does |
|---------|--------------|
| `sci zot setup` | Save your Zotero API key + library ID |
| `sci zot info` | Library size and field-coverage summary |
| `sci zot search <query>` | Search the local Zotero library |
| `sci zot search <q> --export -o hits.bib` | Route search results through the export pipeline |
| `sci zot export -o refs.bib` | Full-library BibTeX / CSL-JSON export (filters: `--collection`, `--tag`, `--type`) |
| `sci zot item read <key>` | Show full metadata for an item |
| `sci zot item list` | List items with optional filters |
| `sci zot item children <key>` | List child attachments + notes of an item |
| `sci zot item export <key>` | Export a single item to CSL-JSON or BibTeX |
| `sci zot item open <key>` | Open the item's PDF attachment |
| `sci zot item extract <key>` | Convert the item's PDF into a Zotero child note (via `docling`) |
| `sci zot item extract <key> --out DIR` | Full extraction: md + json + referenced PNGs + CSV tables to DIR |
| `sci zot item extract <key> --delete` | Undo: trash any note carrying this PDF's sci-extract sentinel |
| `sci zot item add` / `update` / `delete` | Create / patch / trash items via the Zotero Web API |
| `sci zot collection` / `tags` | Manage collections and tags |
| `sci zot doctor` | Run all hygiene checks (invalid → missing → orphans → duplicates) |
| `sci zot doctor {invalid,missing,orphans,duplicates}` | Drill into individual hygiene reports |

Reads the local `zotero.sqlite` (immutable, no contention with the running Zotero desktop app); writes go through the Zotero Web API. `zot doctor --deep` enables fuzzy duplicate detection and noisier orphan kinds.

**PDF → child note extraction.** `zot item extract <KEY>` pipes the item's PDF attachment through [`docling`](https://github.com/DS4SD/docling), renders the markdown as HTML, and posts it as a child note on the parent — tagged `docling` and stamped with a sentinel comment so re-runs dedupe by sha256. Default mode produces a clean, Zotero-friendly note from a temp dir. `--out DIR` switches to full extraction (md + json + referenced PNGs + CSV tables per `docling`'s always-on TableFormer) persisted for Obsidian-style vault exports; `--no-note` skips the Zotero post entirely. Identical re-runs Skip; PDF updates PATCH-in-place so the note key stays stable. `--delete` is the surgical undo — matches notes by their embedded sentinel (not tag) and trashes them via `zot item delete`'s standard path. Requires `docling` on PATH (`sci doctor` installs it via `uv`).

**Library export details.** `zot export` honors user-pinned cite-keys (Zotero 7's native `citationKey` field, or legacy Better BibTeX `Citation Key:` lines in `extra`) and synthesizes semantic keys for everything else as `lastname{year}{firstword}-ZOTKEY`. The trailing 8-char Zotero key suffix guarantees uniqueness without collision arithmetic and keeps entries round-trippable back to the source item. Pinned entries also carry a `zotero://select/library/items/<KEY>` URI in the `note` field (appended to any existing user prose, never overwriting). A `.zotero-citekeymap.json` sidecar is written next to the output file; on the next run, any synthesized prefix that drifted (e.g. after a metadata typo fix) gets a biblatex `ids = {oldkey}` alias so manuscripts citing the old form still resolve.

Also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/zot@latest`.

<details>
<summary><b>Demos</b> — click to expand</summary>

#### Setup & library overview

![zot setup + info](docs/casts/zot-setup.gif)

#### Search & export

![zot search](docs/casts/zot-search.gif)
![zot export](docs/casts/zot-export.gif)

#### Items

![zot item](docs/casts/zot-item.gif)

#### Organize

![zot collection](docs/casts/zot-collection.gif)
![zot tags](docs/casts/zot-tags.gif)

#### Hygiene

![zot doctor](docs/casts/zot-doctor.gif)

</details>

### Ingest markdown into SQLite (Experimental)

| Command | What it does |
|---------|--------------|
| `sci markdb ingest <dir>` | Ingest a directory of `.md` files into SQLite |
| `sci markdb search --db <db> <query>` | Full-text search across files |
| `sci markdb info --db <db>` | Show database summary statistics |
| `sci markdb diff <dir> --db <db>` | Show what would change on next ingest |
| `sci markdb export --db <db> --dir <dir>` | Reconstruct markdown files from the database |

Ingests any folder of markdown files with YAML front-matter into a single SQLite database. Front-matter keys become real SQL columns (dynamically discovered), wikilinks and markdown links are tracked in a `links` table, and FTS5 enables full-text search. Export reconstructs byte-identical files from the database.

Also installable as a standalone binary: `go install github.com/sciminds/cli/cmd/markdb@latest`.

![sci markdb](docs/casts/sci-markdb.gif)

---

## Releases

Every push to `main` triggers the [Release workflow](.github/workflows/release.yml):

1. **Check** — fmt, vet, lint, test
2. **Build** — cross-compiles `sci`, `dbtui`, and `markdb` for darwin/linux × arm64/amd64
3. **Publish** — uploads all binaries to a rolling `latest` GitHub release

Binaries are named `{tool}-{os}-{arch}` (e.g. `sci-darwin-arm64`, `dbtui-linux-amd64`).

**Updating:** Users run `sci update`, which compares the compiled-in commit SHA against the latest release and atomically replaces the binary if a newer build is available.

## Development

Prerequisites: [Go 1.26+](https://go.dev/dl/) and [just](https://github.com/casey/just) (`brew install just`).

You'll also need [`asciicinema`](https://docs.asciinema.org/manual/cli/quick-start/#__tabbed_1_3) to create new terminal "casts" to place in `internal/guide/casts/`

*This is also set up as a git pre-commit hook:*

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
