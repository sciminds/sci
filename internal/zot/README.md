# zot — sci's Zotero library driver

Reads open the user's own `zotero.sqlite` and answer from it — one connection opened read-only, immutable and `query_only`, no credential, no network. Writes go through the Zotero Web API (`internal/zot/api`, a thin hand-written client) with the user's own key, and refuse without one. `sci zot setup` records the data directory (reads) and, optionally, a verified API key (writes).

```
        ~/Zotero/zotero.sqlite                       api.zotero.org
                  │                                         │
    file:…?mode=ro&immutable=1                Zotero-API-Key + If-Unmodified-
          &_pragma=query_only(1)              Since-Version, Zotero-Write-Token
                  │                                         │
        ┌─────────▼──────────┐                    ┌─────────▼──────────┐
        │    local.Reader    │  internal/local    │     api.Client     │  internal/zot/api
        │    (*local.DB)     │  the read door     │  (hand-written v3) │  the write door
        └─────────┬──────────┘                    └─────────┬──────────┘
                  │                                         │
   search  item  notes  collection  export  doctor     collection create/update/delete/add/remove
   bib     link  view   tags        browse  hygiene    item add/update/delete/attach · tags add/remove
                  │                                         │
                  └────────────── cli/ ─────────────────────┘
                     reads: openLocalDB          writes: requireAPIClient
                                                 (config → key → online → one library)

   item add <doi|arxiv|pdf|dir>:  doiorg (doi.org CSL-JSON, no key) + pdfid (pdftotext → bib.ScanText)
                                  → csl.ToItem → api.CreateItem (+ 4-phase attachment upload)

       ┌──── what setup records ─────────────────────┐
       │  data_dir — where zotero.sqlite lives        │
       │  shared_group_id / _name — from the local    │
       │    `groups` table                            │
       │  api_key + user_id — optional; the key is    │
       │    verified with GET /keys/current and the   │
       │    user id comes from the answer             │
       └─────────────────────────────────────────────┘

       ┌──── the fence: lint-guard rule 18 ──────────┐
       │  Only internal/zot/{api,doiorg,cli} may     │
       │  reach net/http. Every other package under  │
       │  internal/zot/ and all of internal/local/   │
       │  is checked transitively by `go list -deps`. │
       └─────────────────────────────────────────────┘
```

**The read door is read-only all the way down.** `mode=ro` forbids writes, `immutable=1` skips WAL processing so we never contend with the running desktop app's locks, and `query_only(1)` catches anything that would sneak past the first two. Consumers take the `local.Reader` interface rather than `*local.DB`, which puts the contract in every signature. Nothing sci does ever touches that file.

**The write door respects Zotero's optimistic concurrency.** Every PATCH and DELETE carries `If-Unmodified-Since-Version`; on 412 the object is re-read and the request rebuilt once, and membership edits (collections, tags) always derive from the freshly fetched server copy because PATCH replaces whole arrays. Creates carry a `Zotero-Write-Token` so a lost response cannot manufacture a duplicate. Every write returns the server's copy of what it wrote — the local mirror lags until Zotero desktop syncs, so that copy is the only honest answer.

**Immutable mode is what the plane cannot see.** Commits Zotero has made but not yet checkpointed are invisible to this connection, and the mirror itself lags whatever the server holds until the desktop app syncs. Both gaps are reported rather than hidden — `walStaleWarning` sizes the first in bytes, `staleLocalWarning` sizes the second in days, and `openLocalDBScoped` emits them where the database is opened so every command inherits them.

**`--library shared` and `--library all` need no probe.** The group's ID and display name live in the local `groups` table: `local.DB.Groups` reads them, `local.ForGroupByAPIID` bridges the numeric group ID Zotero shares across clients to the SQLite `libraryID`, and `local.ForAll` merges both libraries into one pool where every row carries its own `library` provenance.

Invariants, per-command contracts and the gotchas that cost real data: `CLAUDE.md` in this directory.
