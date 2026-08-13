// Package extract converts a Zotero attachment PDF into a child note
// via an external converter (currently docling) and posts it back
// through the Zotero Web API.
//
// Lives in its own sub-package so it can import both
// internal/zot/api (the Web API client) and pkg/local
// (the read-only library reader) without cycling through the parent
// zot package. Same split rationale as internal/zot/enrich.
//
// Shape: a pure PlanExtract decides Create or Skip based on whether the
// parent already has a docling-tagged child note (queried from the local
// DB). Execute runs the converter and posts via a narrow NoteWriter
// interface. NoteWriter is satisfied by *api.Client but tests use fakes
// so the package has no HTTP dep.
//
// Beyond single items: [ExecuteBatch] (batch.go) drives bulk extraction,
// resuming across runs via [MarkdownCache] (cache.go). Extraction work
// is ordered longest-first by estimated page count ([EstimatePages],
// pagecount.go) and drained by Jobs workers from one queue — one
// docling invocation per chunk, oversize documents isolated so a book
// can't block papers (planChunks, schedule.go). [KeyLayout]
// (layout.go) persists a per-key artifact directory (config extract_dir)
// that is independent of the Zotero note — either store can exist
// without the other, and neither gates the other. Every path that posts
// an extraction note tags the parent [MarkdownTag] ("has-markdown");
// content listing keys off that tag, so a create path that skips it
// makes the extraction invisible. When config extract.runner=ssh
// (zot.Config.ExtractRunner), the whole command is delegated to the
// remote host before any local DB or docling work — see
// internal/zot/cli/remote.go.
package extract
