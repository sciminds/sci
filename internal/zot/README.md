# zot — the local read plane

sci's Zotero surface opens the user's own `zotero.sqlite` and answers from it. There is no HTTP client under this tree, no credential in the config, and no verb that writes: `sci zot setup` records a data directory, and every command reads through one connection opened read-only, immutable and `query_only`.

```
                        ~/Zotero/zotero.sqlite
                                  │
                    file:…?mode=ro&immutable=1
                          &_pragma=query_only(1)
                                  │
                        ┌─────────▼──────────┐
                        │    local.Reader    │   pkg/local
                        │    (*local.DB)     │   the one door in
                        └─────────┬──────────┘
                                  │
   ┌───────────┬─────────────┬────┴───────┬─────────────┬────────────┐
   │           │             │            │             │            │
 search      item          notes      collection      export      doctor
 bib         link          view       tags            browse      hygiene
   │           │             │            │             │            │
   └───────────┴─────────────┴─── cli/ ───┴─────────────┴────────────┘


       ┌──── what setup records ─────────────────────┐
       │                                             │
       │   data_dir — where zotero.sqlite lives      │
       │   shared_group_id / shared_group_name —     │
       │     read out of that same database's        │
       │     `groups` table                          │
       │                                             │
       │   Nothing else. No key, no user ID, no      │
       │   account, so `sci zot search` runs on a    │
       │   machine that has never been online.       │
       └─────────────────────────────────────────────┘

       ┌──── the fence: lint-guard rule 18 ──────────┐
       │                                             │
       │   No package under internal/zot/ may reach  │
       │   net/http. A `go list -deps` check with an │
       │   empty allowlist, so it cannot rot.        │
       │                                             │
       │   A new import here is not a policy         │
       │   question — it is the whole surface        │
       │   changing, and it fails the gate.          │
       └─────────────────────────────────────────────┘
```

**Read-only all the way down.** `mode=ro` forbids writes, `immutable=1` skips WAL processing so we never contend with the running desktop app's locks, and `query_only(1)` catches anything that would sneak past the first two. Consumers take the `local.Reader` interface rather than `*local.DB`, which puts the contract in every signature. Creating, editing and deleting are the desktop app's job; no verb here offers to do them.

**Immutable mode is what the plane cannot see.** Commits Zotero has made but not yet checkpointed are invisible to this connection, and the mirror itself lags whatever the server holds until the desktop app syncs. Both gaps are reported rather than hidden — `walStaleWarning` sizes the first in bytes, `staleLocalWarning` sizes the second in days, and `openLocalDBScoped` emits them where the database is opened so every command inherits them.

**`--library shared` and `--library all` need no probe.** The group's ID and display name live in the local `groups` table: `local.DB.Groups` reads them, `local.ForGroupByAPIID` bridges the numeric group ID Zotero shares across clients to the SQLite `libraryID`, and `local.ForAll` merges both libraries into one pool where every row carries its own `library` provenance.

Invariants, per-command contracts and the gotchas that cost real data: `CLAUDE.md` in this directory.
