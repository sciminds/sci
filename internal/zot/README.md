# zot — Read/Write Firewall

```
┌──────────────────────┐         ┌──────────────────────┐
│   local.Reader       │         │   api.Writer         │
│   (31 query methods) │         │   (13 write methods) │
│                      │         │                      │
│   *DB satisfies it   │         │   *Client satisfies  │
│   mode=ro+immutable  │         │   HTTP-only, no DB   │
│   +query_only pragma │         │   getItemRaw private │
└──────────┬───────────┘         └──────────┬───────────┘
           │                                │
     ┌─────┴─────┐                  ┌───────┴───────┐
     │ hygiene   │                  │ extract       │
     │ view      │                  │   .NoteWriter │
     │ doctor    │                  │ fix           │
     │ cli/read  │                  │   .CitekeyWriter│
     │ cli/write │                  │ cli/write     │
     │  (lookups)│                  │ cli/extract   │
     └───────────┘                  └───────────────┘
```

Reads go through `local.Reader` against the local `zotero.sqlite` (immutable, read-only).
Writes go through `api.Writer` against the Zotero Web API (HTTP-only, no local DB access).
See `CLAUDE.md` for full details.
