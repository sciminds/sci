// Package board implements a shared kanban-style board synchronized across
// authenticated sci lab users via Cloudflare R2.
//
// # Design
//
// The board is an append-only event log per board. Each user writes events
// only to their own key prefix, so two clients can never write the same key
// and there are no write conflicts to resolve. State is reconstructed by
// listing all events and folding them in ULID (time-sortable) order via the
// pure [Apply] function.
//
// Concurrent edits merge cleanly because ops are granular: Alice patching a
// card's title and Bob patching its description at the same time produce two
// independent events that both survive the fold. True concurrent writes to
// the same field resolve deterministically — last ULID wins.
//
// # File layout
//
//	doc.go       — package overview (this file)
//	board.go     — Board, Column, Card, and other value types
//	event.go     — Event types and payload codec
//	apply.go     — pure fold: Apply(Board, Event) (Board, error)
//	position.go  — fractional indexing for conflict-free card ordering
//	local.go     — SQLite cache and pending-write queue
//	store.go     — R2-backed Store, glues cloud.Client and the local cache
//
// # Sync model
//
// Load: GET latest snapshot → LIST events after snapshot → fold.
// Append: apply locally, PUT to R2, cache on success, queue on failure.
// Poll: LIST events with start-after=lastSeenId; toast on new events, reload
// on user request. No live merge while the TUI is open.
package board
