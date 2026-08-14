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
// ContractVersion 4 (2026-08-14) is the standalone-local cut, and it is the
// widest break so far: sci's Zotero surface lost the Web API entirely, so
// `--remote` is gone from every verb that had it (search, item read / list /
// children, collection list, link list), `saved-search` and `item note` are
// gone with the API that was their only source, and `import` went with the
// desktop connector. A consumer cached at v3 holds a map to six flags and
// three verbs that no longer parse. The retirement STUBS went too: an old
// verb is now an ordinary unknown command, so a consumer that probed by
// running one gets urfave's rejection instead of a routed explanation.
// Setup changed shape underneath all of it — there is no API key or user ID
// to supply, only a data directory.
func guideContent() zot.GuideResult {
	return zot.GuideResult{
		ContractVersion: 4,
		Contract: []string{
			"Probe data.contract_version first — it bumps only on breaking changes to this contract.",
			"This surface is READ-ONLY, local, and STANDALONE: every command below opens your own zotero.sqlite and answers from it. There is no Zotero Web API client here and no credential of any kind — setup records a data directory and nothing else, so nothing has a live plane to fall back on. Writing to the library, extracting paper text, upstream indexes (OpenAlex, Crossref) and citation traversal are all out of scope: those verbs are not registered here and fail as unknown commands.",
			"Every command accepts --json: one stream (stdout), one shape. Success: {ok:true, data, warnings}. Failure: {ok:false, error:{code, message, fix, try}} on a single line.",
			"error.fix is a complete corrected command — resubmit it verbatim. error.try is prose guidance. error.code is a closed vocabulary (usage, conflict, not-found, ambiguous, offline, not-configured, runtime).",
			"Exit 2 means rewrite the command line and retry; exit 1 means the work itself failed.",
			"Act on warnings[] before trusting data — stale-local means this read cannot see everything Zotero has (the mirror lags the server, or the connection lags the file), and the remedy is Zotero desktop, so the warning carries prose and no fix; bib-quality means entries will be incomplete.",
			"truncated:true means count < total — raise --limit to see the rest.",
			"Search field clauses work bare: tag:read = @tag: read, and -tag:read excludes the tag.",
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
						Note: "Or `sci zot tags list`. Both fast/local. Changing either — creating a collection, tagging an item — is a write, and this surface never writes.",
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
						Note: "A report. Rewriting each DOI to its parent-paper form is a Zotero write, which this surface never makes.",
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
