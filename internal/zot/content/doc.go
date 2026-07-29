// Package content maintains sci's full-text index over item content —
// the text OF a paper, not notes about it.
//
// # Why this exists
//
// Zotero stores a docling extraction as a child note, so for years sci
// treated it as one. That model does not survive contact with the data:
// in a real 5,159-item library, 5,098 of 5,140 notes are extractions.
// Calling them notes makes the two things a user actually has — 40
// annotations and 2 standalone notes — unfindable, and makes "search my
// notes" mean "grep every paper I own".
//
// The correct model is that an extraction is not a note at all. It is
// the item's body text, an attribute of the paper, and it belongs on the
// same plane as the paper's title and DOI. This package is that plane.
//
// # Fallback happens at index time
//
// Two sources can supply an item's text, and they cover different items:
// docling extractions (higher fidelity, structure preserved) and
// Zotero's own .zotero-ft-cache (pdftotext-grade, but present for
// anything Zotero indexed). Rather than query both and union the
// results, [Build] picks the best available source per item and records
// which one it used in [Doc.Source].
//
// That choice is the design. A query-time union would mean two matching
// semantics — FTS5 tokens against Zotero's word index — reconciled per
// search, with snippets available for some hits and not others. Choosing
// at index time collapses it to one row per item, one code path, and one
// notion of what "matched" means, while [Source] still tells the caller
// how good the text behind a hit is.
//
// # Staleness
//
// The index is a cache under os.UserCacheDir; zotero.sqlite remains the
// source of truth and is never written. Each [Doc] carries the Zotero
// object version of whatever produced its text, so [Plan] can diff the
// index against the library cheaply (no bodies read) and reindex only
// what drifted.
//
// The library fingerprint cannot see changes to sci's own indexing
// rules, so [IndexFormat] versions them separately: bumping it forces a
// full rebuild of any index built under an older format. The index
// location ([DefaultPath]) is per-library — personal and group
// libraries each get their own index.
package content
