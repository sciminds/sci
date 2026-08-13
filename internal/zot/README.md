# zot — the read boundary

sci's Zotero surface is the public local read plane: it opens the user's own
`zotero.sqlite` and stops there. Everything that writes to a library, extracts
paper text, or asks a metered upstream index lives in the sibling `zot` binary.

```
┌──────────────────────┐         ┌──────────────────────┐
│   local.Reader       │         │   api (*Client)      │
│   (every read query) │         │   live reads only    │
│                      │         │                      │
│   *DB satisfies it   │         │   the user's own key │
│   mode=ro+immutable  │         │   against their own  │
│   +query_only pragma │         │   library            │
└──────────┬───────────┘         └──────────┬───────────┘
           │                                │
     ┌─────┴─────┐                  ┌───────┴────────┐
     │ hygiene   │                  │ --remote on    │
     │ view      │                  │  item read     │
     │ doctor    │                  │  item list     │
     │ bib       │                  │  item children │
     │ export    │                  │  collection ls │
     │ cli/read  │                  │  search        │
     └───────────┘                  │  link list     │
                                    └────────────────┘

       ┌──── the one write, and the app makes it ────┐
       │                                             │
       │   connector — localhost, `sci zot import`   │
       │   hands a PDF to the running Zotero desktop │
       │   and the app recognizes + syncs it.        │
       └─────────────────────────────────────────────┘

       ┌──── moved to zot, registered as stubs ──────┐
       │                                             │
       │   item add/update/delete/attach/note write  │
       │   collection create/delete/add/remove       │
       │   tags add/remove/delete                    │
       │   find · openalex · crossref                │
       │   graph · content · llm                     │
       │   extract · extract-lib                     │
       │                                             │
       │   Each names its `zot` replacement in Try,  │
       │   never in Fix — zot is a different program │
       │   and is not installed everywhere sci is.   │
       └─────────────────────────────────────────────┘

       ┌──── retired outright, remedy is prose ──────┐
       │                                             │
       │   saved-search create/update/delete         │
       │                                             │
       │   The Web API stores a saved search but     │
       │   never evaluates it — only Zotero desktop  │
       │   runs the query, so writing one is the     │
       │   desktop UI's job. list/show stay.         │
       └─────────────────────────────────────────────┘
```

No metered third-party index is reachable from this tree at all, and
`scripts/lint-guard.sh` rule 18 is the fence. See `CLAUDE.md` for full details.
