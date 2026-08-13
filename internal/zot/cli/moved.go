package cli

import "github.com/urfave/cli/v3"

// This file is the boundary, written down. sci's Zotero surface is the
// public local read plane — it opens the user's own zotero.sqlite
// read-only and stops there. Everything that needs a credential, an
// upstream index, or a write left, and every one of those verbs stays
// registered here as a stub so the change is discoverable instead of
// arriving as "command not found".
//
// Most of them went to the sibling `zot` binary, which owns the credential
// ([movedToZotCommand]). One family went nowhere: a saved search is a
// stored QUERY, and the Zotero Web API can hold its definition but cannot
// evaluate it — only the desktop client runs one. A write verb for a thing
// only the desktop can use belongs to the desktop's own UI, so those
// retire outright and their remedy is prose
// ([retiredOutrightCommand]).
//
// Neither shape ever fills Fix — see retire.go.

// Item writes. Every one of these is a POST or PATCH against the Zotero
// Web API, which is the credential sci no longer holds.
const itemWriteWhy = "creating, editing and deleting items is a credentialed write against the Zotero Web API"

func itemAddStub() *cli.Command {
	return movedToZotCommand("add", "moved to `zot item add`",
		[]string{"item", "add"}, "zot item add",
		itemWriteWhy+" (and `--openalex` also spends a metered upstream lookup)")
}

func itemUpdateStub() *cli.Command {
	return movedToZotCommand("update", "moved to `zot item update`",
		[]string{"item", "update"}, "zot item update",
		itemWriteWhy+" (`--from-json` applies the plans `zot backfill` and `zot enrich` write)")
}

func itemDeleteStub() *cli.Command {
	return movedToZotCommand("delete", "moved to `zot item delete`",
		[]string{"item", "delete"}, "zot item delete", itemWriteWhy)
}

func itemAttachStub() *cli.Command {
	return movedToZotCommand("attach", "moved to `zot item attach`",
		[]string{"item", "attach"}, "zot item attach",
		"attaching a file uploads it through the Zotero Web API")
}

func itemNoteAddStub() *cli.Command {
	return movedToZotCommand("add", "moved to `zot item note add`",
		[]string{"item", "note", "add"}, "zot item note add",
		"posting a note is a credentialed write against the Zotero Web API")
}

func itemNoteUpdateStub() *cli.Command {
	return movedToZotCommand("update", "moved to `zot item note update`",
		[]string{"item", "note", "update"}, "zot item note update",
		"editing a note is a credentialed write against the Zotero Web API")
}

// Collection writes. `collection list` stays — it reads the local mirror.
const collectionWriteWhy = "changing collections or their membership is a credentialed write against the Zotero Web API"

func collectionCreateStub() *cli.Command {
	return movedToZotCommand("create", "moved to `zot collection create`",
		[]string{"collection", "create"}, "zot collection create", collectionWriteWhy)
}

func collectionDeleteStub() *cli.Command {
	return movedToZotCommand("delete", "moved to `zot collection delete`",
		[]string{"collection", "delete"}, "zot collection delete", collectionWriteWhy)
}

func collectionAddStub() *cli.Command {
	return movedToZotCommand("add", "moved to `zot collection add`",
		[]string{"collection", "add"}, "zot collection add", collectionWriteWhy)
}

func collectionRemoveStub() *cli.Command {
	return movedToZotCommand("remove", "moved to `zot collection remove`",
		[]string{"collection", "remove"}, "zot collection remove", collectionWriteWhy)
}

// Tag writes. `zot tags` owns the whole write side — add and remove per
// item, delete library-wide — so keeping a second copy here is exactly the
// two-answers-to-one-question duplication the three-tool split forbids.
// `tags list` and `tags browse` stay: they read the local mirror.
const tagWriteWhy = "attaching, removing and deleting tags is a credentialed write against the Zotero Web API"

func tagsAddStub() *cli.Command {
	return movedToZotCommand("add", "moved to `zot tags add`",
		[]string{"tags", "add"}, "zot tags add", tagWriteWhy)
}

func tagsRemoveStub() *cli.Command {
	return movedToZotCommand("remove", "moved to `zot tags remove`",
		[]string{"tags", "remove"}, "zot tags remove", tagWriteWhy)
}

func tagsDeleteStub() *cli.Command {
	return movedToZotCommand("delete", "moved to `zot tags delete`",
		[]string{"tags", "delete"}, "zot tags delete", tagWriteWhy)
}

// Saved-search writes. These retire with no home in either binary, and
// that is the decision rather than an omission: the Zotero Web API stores a
// saved search's definition but never evaluates it — the desktop client is
// the only thing that runs the query. The same quirk once silently no-op'd
// a pipeline predicate built on a saved search, which is why the reads
// (`list`, `show`) stay: seeing what a search is defined as remains useful
// even though only the desktop can answer it.
const savedSearchWriteWhy = "the Zotero Web API stores a saved search's definition but cannot evaluate it — only the desktop client runs the query"

const savedSearchWriteRemedy = "create, edit and delete saved searches in Zotero desktop, where they are evaluated (right-click the library in the left sidebar → New Saved Search); `sci zot saved-search list` and `show` still read them back"

func savedSearchCreateStub() *cli.Command {
	return retiredOutrightCommand("create", "retired — saved searches are edited in Zotero desktop",
		[]string{"saved-search", "create"}, savedSearchWriteWhy, savedSearchWriteRemedy)
}

func savedSearchUpdateStub() *cli.Command {
	return retiredOutrightCommand("update", "retired — saved searches are edited in Zotero desktop",
		[]string{"saved-search", "update"}, savedSearchWriteWhy, savedSearchWriteRemedy)
}

func savedSearchDeleteStub() *cli.Command {
	return retiredOutrightCommand("delete", "retired — saved searches are edited in Zotero desktop",
		[]string{"saved-search", "delete"}, savedSearchWriteWhy, savedSearchWriteRemedy)
}

// Upstream indexes. OpenAlex and Crossref are metered third-party APIs;
// a public local read surface must not dial either one.
func findStub() *cli.Command {
	return movedToZotCommand("find", "moved to `zot find`",
		[]string{"find"}, "zot find",
		"OpenAlex is a metered upstream index, and sci's Zotero surface makes no network call")
}

func openalexStub() *cli.Command {
	return movedToZotCommand("openalex", "moved to `zot openalex sync`",
		[]string{"openalex"}, "zot openalex sync",
		"the OpenAlex work cache is staging for zot's snapshot, and filling it is a metered upstream fetch")
}

func crossrefStub() *cli.Command {
	return movedToZotCommand("crossref", "moved to `zot crossref`",
		[]string{"crossref"}, "zot crossref",
		"the Crossref candidate and works caches are staging for zot's snapshot, and filling them is a metered upstream fetch")
}

// Retired outright, superseded by zot's snapshot plane rather than moved
// verb for verb.
func graphStub() *cli.Command {
	return movedToZotCommand("graph", "retired — see `zot cites` / `zot refs`",
		[]string{"graph"}, "zot cites",
		"traversing citations here cost an OpenAlex call per hop; zot walks a local citation graph with occurrence snippets at no network cost (`zot refs` for outgoing edges, `zot cites` and `zot cited-by` for incoming)")
}

func contentStub() *cli.Command {
	return movedToZotCommand("content", "retired — see `zot read` / `zot search`",
		[]string{"content"}, "zot read",
		"paper text belongs to the snapshot now: zot indexes located sentences carrying section roles over a corpus that reports its own coverage, replacing the per-library BM25 cache this built (`zot search` to find, `zot read` to read, `zot coverage` for what the corpus holds)")
}

func llmStub() *cli.Command {
	return movedToZotCommand("llm", "retired — see `zot read` / `zot query`",
		[]string{"llm"}, "zot read",
		"this piped Zotero note bodies through mq; zot reads the docling JSON and the GROBID TEI directly, so `zot search`, `zot read` and `zot query` answer without the note layer in between")
}

// Extraction. Running docling and posting the result back is a
// credentialed write on top of an expensive local job.
const extractionWhy = "extraction runs docling and posts the text back as a Zotero child note, which is a credentialed write"

func extractStub() *cli.Command {
	return movedToZotCommand("extract", "moved to `zot extract-lib`",
		[]string{"extract"}, "zot extract-lib", extractionWhy)
}

func extractLibStub() *cli.Command {
	return movedToZotCommand("extract-lib", "moved to `zot extract-lib`",
		[]string{"extract-lib"}, "zot extract-lib", extractionWhy)
}
