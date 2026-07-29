# zot — Read/Write Firewall

```
┌──────────────────────┐         ┌──────────────────────┐
│   local.Reader       │         │   api.Writer         │
│   (every read query) │         │   (every write op)   │
│                      │         │                      │
│   *DB satisfies it   │         │   *Client satisfies  │
│   mode=ro+immutable  │         │   HTTP-only, no DB   │
│   +query_only pragma │         │                      │
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

       ┌───── agent escape hatches (explicit, opt-in) ─────┐
       │                                                   │
       │   *Client.GetItem / ListItems / ListCollections   │
       │   — exported reads on *Client, NOT on Writer.     │
       │   Used by --remote CLI flag and hydrated writes.  │
       └───────────────────────────────────────────────────┘
```

See `CLAUDE.md` for full details.
