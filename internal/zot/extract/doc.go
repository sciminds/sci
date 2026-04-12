// Package extract converts a Zotero attachment PDF into a child note
// via an external converter (currently docling) and posts it back
// through the Zotero Web API.
//
// Lives in its own sub-package so it can import both
// internal/zot/api (the Web API client) and internal/zot/local
// (the read-only library reader) without cycling through the parent
// zot package. Same split rationale as internal/zot/fix.
//
// Shape: a pure PlanExtract decides Create or Skip based on whether the
// parent already has a docling-tagged child note (queried from the local
// DB). Execute runs the converter and posts via a narrow NoteWriter
// interface. NoteWriter is satisfied by *api.Client but tests use fakes
// so the package has no HTTP dep.
package extract
