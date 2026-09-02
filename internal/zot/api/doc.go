// Package api is a thin, hand-written client for the Zotero Web API v3,
// covering exactly the calls the `sci zot` write verbs make: single-object
// reads, multi-object creates, versioned PATCH/DELETE, membership edits on
// an item's collections and tags, and the item-type schema.
//
// Design in one paragraph: a [Client] is bound to one library ([Credentials]
// plus a [zot.LibraryRef] that is personal or shared — never the merged
// read pool). Every request carries the Zotero-API-Key and
// Zotero-API-Version headers and goes through a retry layer that honours
// Zotero's rate-limit signals (429 + Retry-After, the Backoff header) and
// backs off on 5xx and network errors. Objects come back as [Object] with a
// generic data map — sci never models Zotero's per-type schema.
//
// Optimistic concurrency is the one Zotero quirk every write here has to
// respect: a PATCH or DELETE carries If-Unmodified-Since-Version and the
// server answers 412 if the object moved since it was read. Recovery is a
// re-read plus a rebuilt request, which is per-operation knowledge, so the
// transport layer never retries a 412; each write re-reads once and then
// surfaces a [VersionConflictError]. Membership edits (AddTag,
// AddToCollection, …) always derive their payload from the freshly fetched
// server copy, because Zotero PATCH replaces whole arrays — stamping a fresh
// version onto a stale array would erase what another writer added.
package api
