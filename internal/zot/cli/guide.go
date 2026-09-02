package cli

// `sci zot guide` — agent-facing cheat sheet. Lists task-oriented intents
// ("Find papers in my library on X", "Read full PDF text of paper ABC123")
// paired with the exact command to run. Output is token-budgeted so an
// LLM driver can pull it once at session start.
//
// Tests in guide_test.go verify every cmd in a GuideEntry resolves to a
// real subcommand of `sci zot`, so the cheat sheet can't drift silently.

import (
	"context"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

// guideContent returns the canonical cheat sheet. Pulled out as a function
// (not a package-level var) so tests can compare against it directly.
//
// Conventions:
//   - Cmd lines always start with `sci zot …` so agents can copy-paste.
//   - Library scope is omitted — the resolver auto-selects / prompts.
//   - Notes call out tradeoffs, gotchas, or compose-with hints.
//
// ContractVersion 5 (2026-09-01) brings writes back: `collection create /
// update / delete / add / remove`, `item add / update / delete / attach` and
// `tags add / remove` are registered and the Curate section teaches them.
// Reads are unchanged — still local, still credential-free — but the
// contract an agent plans against is different: it can now add a paper,
// edit it, file it, tag it and prune it, and it must know that every write
// returns the server's copy because the mirror it reads lags. Setup grew
// `--api-key` to store the credential the writes need. A consumer cached at
// v4 holds a contract that says the surface never writes; that sentence is
// gone, which is exactly the kind of change this number announces. v4
// (2026-08-14) was the standalone-local cut that removed the previous Web
// API surface wholesale (`--remote`, `saved-search`, `item note`, `import`).
func guideContent() zot.GuideResult {
	return zot.GuideResult{
		ContractVersion: 5,
		Contract: []string{
			"Probe data.contract_version first — it bumps only on breaking changes to this contract.",
			"Reads are local and STANDALONE: every read below opens your own zotero.sqlite and answers from it, with no credential and no network. Extracting paper text, upstream indexes (OpenAlex, Crossref) and citation traversal are out of scope: those verbs are not registered here and fail as unknown commands.",
			"Writes need an API key (`sci zot setup --api-key`) and target ONE library (`--library personal|shared`); every write returns the server's copy of what it wrote in data, because the local mirror lags until Zotero desktop syncs — do not re-read the mirror to confirm a write. Items, their attachments, their tags and collections are writable (Curate); notes and relations are not.",
			"Every command accepts --json: one stream (stdout), one shape. Success: {ok:true, data, warnings}. Failure: {ok:false, error:{code, message, fix, try}} on a single line.",
			"error.fix is a complete corrected command — resubmit it verbatim. error.try is prose guidance. error.code is a closed vocabulary (usage, conflict, not-found, ambiguous, duplicate, offline, not-configured, runtime).",
			"Exit 2 means rewrite the command line and retry; exit 1 means the work itself failed.",
			"Act on warnings[] before trusting data — stale-local means this read cannot see everything Zotero has (the mirror lags the server, or the connection lags the file), and the remedy is Zotero desktop, so the warning carries prose and no fix; bib-quality means entries will be incomplete.",
			"truncated:true means count < total — raise --limit to see the rest.",
			"Search field clauses work bare: tag:read = @tag: read, and -tag:read excludes the tag. A query that STARTS with `-` must follow a `--` separator or the flag parser claims it first: sci zot search -- '-tag:read'. Anywhere later in the query no separator is needed: sci zot search 'cortex -tag:read'.",
			"--library all merges personal+shared into one ranked pool — accepted by search, bib, export --format ndjson and browse; each row's library field is its provenance (library_id reads 0). Everything else takes --library personal|shared.",
			"item read accepts cite keys as positionals, not just 8-char Zotero keys. --library goes anywhere in the command.",
			"Human output is for terminals; pipe --json instead.",
		},
		Sections: []zot.GuideSection{
			{
				Title: "Bootstrap",
				Entries: []zot.GuideEntry{
					{
						Goal: "Orient yourself in this library (top tags, collections, recent items, extraction coverage)",
						Cmd:  "sci zot info --orient",
						Note: "Run this first. top_tags may include auto-applied tags (docling, arXiv subject categories, and whatever else has written to the library) — eyeball the names: high-count + system-looking is noise; the user's curated taxonomy is usually the long tail. top_collections, recent_added, and extraction_coverage are clean signal.",
					},
				},
			},
			{
				Title: "Discovery",
				Entries: []zot.GuideEntry{
					{
						Goal: "Find papers in my library on a topic",
						Cmd:  "sci zot search \"large language models\" --library personal",
						Note: "Metadata match over title/DOI/pub/creators/citekey, ranked by title relevance then year. Bare words AND across fields ('jolly 2021' = creator + year; a bare 1500-2099 token means the year); quotes make a literal phrase. --notes FILTERS to items that have an extraction (it does not search inside one). The TEXT of your papers is not indexed here — this matches metadata only.",
					},
					{
						Goal: "Which paper is this cite-key / Zotero key?",
						Cmd:  "sci zot search '@citekey: saxe2022-ment'",
						Note: "Matches the stored citationKey and the 8-char Zotero key; a whole synthesized key resolves via its -ZOTKEY suffix even if the prefix drifted.",
					},
					{
						Goal: "Lookup an item by exact key",
						Cmd:  "sci zot item read ABC12345",
						Note: "Shows tags, collections, attachments, and related items (data.relations — `related` is the user's own dc:relation links, `other` is Zotero-managed owl:sameAs/dc:replaces, `titles` names each far end). An item or link written seconds ago lives only on the server until Zotero desktop syncs it back, so a miss here can be sync lag rather than absence — the stale-local warning says when that is likely.",
					},
					{
						Goal: "Read several items in one call",
						Cmd:  "sci zot item read ABC12345 DEF67890",
						Note: "N>1 keys emit {count, items} in request order, fully hydrated; a missing key fails the whole batch naming it.",
					},
					{
						Goal: "Lookup an item by DOI",
						Cmd:  "sci zot item read --doi 10.1038/nature12373",
						Note: "Local, case-insensitive. A DOI the library does not hold is a gap in THIS library, not a claim the paper does not exist.",
					},
					{
						Goal: "List or filter the items in a collection, tag, or type",
						Cmd:  "sci zot item list --collection ABC12345 --limit 25",
						Note: "The plain enumerate verb — combine --type / --collection / --tag with --limit / --offset / --order (added, modified, title).",
					},
					{
						Goal: "See an item's attachments and child notes",
						Cmd:  "sci zot item children ABC12345",
						Note: "One parent's children with filename and md5. A ZERO answer is ambiguous and says so in a stale-local warning: the local database cannot tell a childless item from one whose children were written since the last Zotero desktop sync — never read it as permission to write.",
					},
					{
						Goal: "List all collections / tags",
						Cmd:  "sci zot collection list",
						Note: "Or `sci zot tags list`. Both fast/local. Collections and an item's tags are editable from here (Curate).",
					},
				},
			},
			{
				Title: "Curate",
				Entries: []zot.GuideEntry{
					{
						Goal: "Add a paper by DOI or arXiv id",
						Cmd:  "sci zot item add 10.1038/nature12373 --collection \"To read\" --tag to-read",
						Note: "Any cited form works (bare, doi:, doi.org URL, arXiv:1706.03762 or the bare arXiv id). The record comes from doi.org, so a not-found means the DOI is unregistered or mistyped, not that the paper is missing. Dedup is by DOI, against the local mirror AND the server, so an item you added moments ago is caught before desktop syncs it down: a match is refused as `duplicate` naming the existing key (--force creates a second copy); an item with no DOI is never matched. data.item is the server's item; --collection takes a key or name.",
					},
					{
						Goal: "Add a paper from its PDF, with the file attached",
						Cmd:  "sci zot item add paper.pdf",
						Note: "Reads the DOI or arXiv id off the first pages (needs pdftotext: `sci tools reccs --install poppler`), resolves it, creates the item and uploads the PDF. data.attached / attachment_key report the upload. A failed upload after a successful create rides data.attach_error rather than failing the command: the empty attachment is trashed (attachment_key is \"\"), and `item attach KEY paper.pdf` adds a fresh one. A PDF with several identifiers uses the first in reading order and lists the rest in data.candidates (a stderr note says how to pick another). A PDF whose DOI is already in the library is a `duplicate` whose try names the `item attach KEY paper.pdf` that files it under the existing item. No identifier on the pages → not-found pointing at --title (manual mode: --author \"Last, First\", --date, --publication, --field name=value, one PDF positional to attach).",
					},
					{
						Goal: "Add a folder of downloaded PDFs, checking first",
						Cmd:  "sci zot item add ~/Downloads/ --dry-run",
						Note: "Every *.pdf in the folder (not recursive). --dry-run resolves each (doi.org and pdftotext run; nothing is written) and reports item_type, title, first_author, year, doi, collection, attach and any existing_key. Re-run without --dry-run to create: several inputs return {total, success, failed, data.rows} in input order, and a failing input is reported while the rest proceed.",
					},
					{
						Goal: "Attach a PDF to an item that exists, without attaching it twice",
						Cmd:  "sci zot item attach ABC12345 paper.pdf --skip-existing",
						Note: "A bare attach is not idempotent, and `item children` (local) answers a confident zero for children this CLI just wrote — so guard with --skip-existing, which hashes the file and asks the server whether the item already has that md5. data.uploaded is false when Zotero already held the bytes.",
					},
					{
						Goal: "Correct an item's fields",
						Cmd:  "sci zot item update ABC12345 --title \"Corrected title\" --field volume=12",
						Note: "Dedicated flags for title, doi, url, date, abstract, extra, publication (routed to the type's venue field) and type; --field name=value for the rest, validated against the item's type before anything is sent. An explicit empty value clears a field. --author / --creator REPLACE the whole creator list. Several keys return a per-key report.",
					},
					{
						Goal: "Remove an item",
						Cmd:  "sci zot item delete ABC12345 --yes",
						Note: "Moves it to Zotero's trash (never a permanent delete; restore or empty the trash in Zotero desktop). Already trashed is a success that changed nothing. --yes is required under --json.",
					},
					{
						Goal: "Tag an item, or untag it",
						Cmd:  "sci zot tags add ABC12345 to-read methods",
						Note: "The first positional is the item (key or cite key), the rest are tags; `sci zot tags remove` is the inverse. Already present / absent is a success that changed nothing; several tags return a per-tag report with data.unchanged. Tag names match exactly.",
					},
					{
						Goal: "Create a collection, optionally nested under another",
						Cmd:  "sci zot collection create \"Reading list\" --parent Projects",
						Note: "The parent is a key or exact name (case-insensitive) from the local mirror; a name matching several collections is an ambiguous error listing their keys. data is the server's collection; its key works immediately even though `collection list` will not show it until Zotero desktop syncs.",
					},
					{
						Goal: "File one item (or a few) into a collection",
						Cmd:  "sci zot collection add ABC12345 smith2024-paper \"Reading list\"",
						Note: "Every positional but the last is an item (8-char key or cite key); the last is the collection. Already a member is a success that changed nothing. Several items return {total, success, failed, data.unchanged}; a failing item is reported per key and the rest proceed.",
					},
					{
						Goal: "File every hit of a search into a collection",
						Cmd:  "sci zot search '@tag: to-read' --json | jq -r '.data.items[].key' | sci zot collection add --from-file - \"Reading list\"",
						Note: "--from-file: one key per line (- is stdin; blank lines and # comments skipped), then the collection is the only positional. Items are written one at a time (Zotero rate-limits).",
					},
					{
						Goal: "Take an item out of a collection",
						Cmd:  "sci zot collection remove ABC12345 \"Reading list\"",
						Note: "The item stays in the library and its other collections. Not a member is a success that changed nothing. Batch and --from-file work as for add.",
					},
					{
						Goal: "Rename a collection, or move it under another parent / to the top level",
						Cmd:  "sci zot collection update \"Reading list\" --name \"To read\" --parent Projects",
						Note: "At least one of --name, --parent, --top-level (the last two exclude each other). data is the collection as the server holds it now.",
					},
					{
						Goal: "Delete a collection",
						Cmd:  "sci zot collection delete \"To read\" --yes",
						Note: "Items stay in the library; sub-collections go with it (the prompt counts them). --yes is required under --json — an unanswerable prompt is a refusal (exit 2), not consent.",
					},
				},
			},
			{
				Title: "Notes and relations",
				Entries: []zot.GuideEntry{
					{
						Goal: "Read the notes you wrote, separately from the paper text",
						Cmd:  "sci zot notes list",
						Note: "Docling extractions are excluded — on the live library they run to thousands per library against dozens of notes a person actually wrote, so an unfiltered listing is a listing of extractions. `sci zot notes read <note-key> --md` adds a `markdown` field.",
					},
					{
						Goal: "See what an item is related to",
						Cmd:  "sci zot link list NOTEKEY1",
						Note: "The dc:relation set: `related` is what a person linked by hand, `other` is Zotero's own bookkeeping (owl:sameAs, dc:replaces), `titles` names each far end. Reads the local database, which lags until Zotero desktop syncs — a relation written seconds ago is not here yet. Relations are read here and never written.",
					},
				},
			},
			{
				Title: "Bibliography",
				Entries: []zot.GuideEntry{
					{
						Goal: "Export the whole library (or a slice) to BibLaTeX / CSL-JSON",
						Cmd:  "sci zot export --out refs.bib",
						Note: "Filter with --collection / --tag / --type; --format csl-json for a processor-friendly dump. With --out, writes a .zotero-citekeymap.json sidecar so drifted synthesized keys get a biblatex `ids = {oldkey}` alias next run.",
					},
					{
						Goal: "Mirror the whole item plane for a downstream store",
						Cmd:  "sci zot export --format ndjson --out zotero-items.ndjson",
						Note: "A mirror, not a bibliography: one kind-tagged JSON object per line (collections first so a streaming consumer can resolve membership as it goes), keeping versions, tags, collections and attachment metadata. Accepts --library all — every record carries its own `library`. With --out, a .meta.json sidecar is written LAST carrying the body's sha256, so its presence is the completeness signal; it also reports last_sync and pending_wal_bytes. No local filesystem paths.",
					},
					{
						Goal: "Build a .bib of exactly what a manuscript cites",
						Cmd:  "sci zot bib paper.qmd --out refs.bib",
						Note: "Scans markdown/Quarto for @citekeys, [[wikilinks]], DOIs, arXiv ids, URLs; resolves each against the library. Point at a folder (+ --recursive) to scan many. Refs matching 0 or >1 items are listed as unresolved, never guessed. --format csl-json supported.",
					},
					{
						Goal: "See which of a draft's citations your library cannot account for",
						Cmd:  "sci zot bib draft.md --json",
						Note: "`unresolved` lists every ref that matched 0 or >1 items, with the reason and (on an ambiguity) the competing keys. It is a statement about THIS library, not about the literature — a ref absent here may be a real paper you have not filed. Deciding which is which needs an upstream index, and this surface never queries one.",
					},
				},
			},
			{
				Title: "Hygiene",
				Entries: []zot.GuideEntry{
					{
						Goal: "Health check the whole library",
						Cmd:  "sci zot doctor",
						Note: "Runs invalid → missing → orphans → duplicates → citekeys; drill in with any of those as a sub-command. Every check reports over the local database — doctor never writes, dials out, or spends a metered lookup. Repairing what it finds is a Zotero write, which happens in Zotero itself.",
					},
					{
						Goal: "See which items lack the fields you cite with",
						Cmd:  "sci zot doctor missing --field doi,abstract",
						Note: "Fields: title, creators, date, doi, abstract, url, pdf, tags. Severity graded: title=error, creators/date=warn, rest=info.",
					},
					{
						Goal: "Flag publisher-subobject DOIs (article sections, tables, figures, supplements, eLife assets) so OpenAlex can resolve them",
						Cmd:  "sci zot doctor dois",
						Note: "A report — doctor never rewrites a DOI. Fix one deliberately: sci zot item update KEY --doi <parent-doi>.",
					},
				},
			},
		},
		Tip: "All commands accept --json for machine-readable output. On multi-library setups pass --library personal|shared in any position (`sci zot --library personal item list` and `sci zot item list --library personal` are equivalent). On single-library accounts it's auto-selected.",
	}
}

func guideCommand() *cli.Command {
	return &cli.Command{
		Name:        "guide",
		Usage:       "Agent-friendly cheat sheet of common workflows",
		Description: "Prints a task-oriented index of `sci zot` commands\n($ sci zot guide        # styled, ~50 lines\n$ sci zot guide --json # raw, suitable for piping to an LLM)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cmdutil.Output(cmd, guideContent())
			return nil
		},
	}
}
