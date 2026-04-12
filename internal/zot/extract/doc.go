// Package extract converts a Zotero attachment PDF into a child note
// via an external converter (currently docling) and posts it back
// through the Zotero Web API.
//
// Lives in its own sub-package so it can import both
// internal/zot/api (the Web API client) and internal/zot/local
// (the read-only library reader) without cycling through the parent
// zot package. Same split rationale as internal/zot/fix.
//
// Shape mirrors fix: a pure PlanExtract computes what would happen
// (Create / Replace / Skip) against a ChildLister interface, and
// Execute (added alongside extract.go) runs the converter and posts
// via a narrow NoteWriter interface. Both interfaces are satisfied by
// *api.Client but tests use fakes so the package has no HTTP dep.
package extract
