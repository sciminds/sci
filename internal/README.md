# internal/

All packages live under `internal/` so they cannot be imported by external projects. The binaries in `cmd/` are the only public entry points.

For the current set of packages, run `ls internal/`. Each package has a doc comment on its `package` declaration explaining its purpose.

## Packages with their own CLAUDE.md (read before editing)

- `tui/dbtui/` — SQLite browser TUI (also `cmd/dbtui` standalone)
- `zot/` — Zotero CLI + hygiene checks (mounted under `sci zot`)

Cross-cutting design rules and the workflow gate live in the repo-root `CLAUDE.md`.
