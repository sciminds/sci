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
func guideContent() zot.GuideResult {
	return zot.GuideResult{
		ContractVersion: 1,
		Contract: []string{
			"Probe data.contract_version first — it bumps only on breaking changes to this contract or the extraction layout.",
			"With extract.dir set in zot.json, extractions also land as <extract.dir>/<KEY>/ dirs: KEY.md, KEY.json (DoclingDocument), KEY_artifacts/, tables/*.csv, result.json, .done (written last). extract.runner=ssh delegates docling to extract.host.",
			"Every command accepts --json: one stream (stdout), one shape. Success: {ok:true, data, warnings}. Failure: {ok:false, error:{code, message, fix, try}} on a single line.",
			"error.fix is a complete corrected command — resubmit it verbatim. error.try is prose guidance. error.code is a closed vocabulary (usage, conflict, not-found, ambiguous, offline, not-configured, runtime).",
			"Exit 2 means rewrite the command line and retry; exit 1 means the work itself failed.",
			"Act on warnings[] before trusting data — stale-local means the local mirror lags Zotero sync (warning carries the --remote resubmit); bib-quality means entries will be incomplete.",
			"truncated:true means count < total — raise --limit to see the rest.",
			"Search field clauses work bare: tag:read = @tag: read, and -tag:read excludes the tag.",
			"--library all (search+bib only) merges personal+shared into one ranked pool; each row's library field is its provenance (library_id reads 0).",
			"item read accepts cite keys as positionals, not just 8-char Zotero keys. --library goes anywhere in the command.",
			"With --content, match evidence rides in data.snippets — a map keyed by item key, not a field on each item. Join it yourself: data.snippets[item.key]. A key is absent when the excerpt would only restate the item's own title.",
			"Human output is for terminals; pipe --json instead. Full paper text is data.body from `content read --json`, never the human stream.",
		},
		Sections: []zot.GuideSection{
			{
				Title: "Bootstrap",
				Entries: []zot.GuideEntry{
					{
						Goal: "Orient yourself in this library (top tags, collections, recent items, extraction coverage)",
						Cmd:  "sci zot info --orient",
						Note: "Run this first. top_tags may include auto-applied tags (docling, _no-openalex, arXiv subject categories) — eyeball the names: high-count + system-looking is noise; the user's curated taxonomy is usually the long tail. top_collections, recent_added, and extraction_coverage are clean signal.",
					},
				},
			},
			{
				Title: "Discovery",
				Entries: []zot.GuideEntry{
					{
						Goal: "Find papers in my library on a topic",
						Cmd:  "sci zot search \"large language models\" --library personal",
						Note: "Local metadata match over title/DOI/pub/creators/citekey, ranked by title relevance then year. Bare words AND across fields ('jolly 2021' = creator + year; a bare 1500-2099 token means the year); quotes make a literal phrase. --content also matches the full TEXT of your papers (one-time `sci zot content build`; quote phrases there too), --remote the Zotero Web fulltext (abstract + notes + PDFs). --notes instead FILTERS to items that have an extraction (no searching inside).",
					},
					{
						Goal: "Which paper is this cite-key / Zotero key?",
						Cmd:  "sci zot search '@citekey: saxe2022-ment'",
						Note: "Matches the stored citationKey and the 8-char Zotero key; a whole synthesized key resolves via its -ZOTKEY suffix even if the prefix drifted.",
					},
					{
						Goal: "Find papers I don't have yet (OpenAlex)",
						Cmd:  "sci zot find works \"theory of mind\"",
						Note: "Compact JSON shape by default (~12 fields/work). --verbose for raw OpenAlex record.",
					},
					{
						Goal: "Lookup an item by exact key",
						Cmd:  "sci zot item read ABC12345",
						Note: "Shows tags, collections, attachments, and related items (data.relations — `related` is the user's own dc:relation links, `other` is Zotero-managed owl:sameAs/dc:replaces, `titles` names each far end). Add --remote when the local DB may be stale: a link written seconds ago lives only on the server until Zotero desktop syncs it back, so a fresh `link add` won't show locally (the stale-local warning carries the resubmit).",
					},
					{
						Goal: "Read several items in one call",
						Cmd:  "sci zot item read ABC12345 DEF67890",
						Note: "N>1 keys emit {count, items} in request order, fully hydrated; a missing key fails the whole batch naming it.",
					},
					{
						Goal: "Lookup an item by DOI",
						Cmd:  "sci zot item read --doi 10.1038/nature12373",
						Note: "Local, case-insensitive. Errors point at `find works <doi>` when the DOI isn't in the library.",
					},
					{
						Goal: "List all collections / tags",
						Cmd:  "sci zot collection list",
						Note: "Or `sci zot tags list`. Both fast/local.",
					},
					{
						Goal: "Walk citation neighbors (incoming/outgoing)",
						Cmd:  "sci zot graph refs ABC12345",
						Note: "Splits into in_library (Zotero keys) vs outside_library (OpenAlex ids; pipe into `item add --openalex`). Default --limit 25; pass --limit 0 for the full bibliography.",
					},
				},
			},
			{
				Title: "Full-text extraction (has-markdown items)",
				Entries: []zot.GuideEntry{
					{
						Goal: "Items tagged `has-markdown` carry a child docling note with the full PDF extraction. Anything you can do with markdown — `llm read`, `llm query`, mq, grep — works on them",
						Cmd:  "sci zot llm catalog",
						Note: "Compact index of every paper with an extraction. Add --full to inline citekey + year + authors + abstract per entry.",
					},
					{
						Goal: "Read full markdown content of one or more papers",
						Cmd:  "sci zot llm read ABC12345 DEF67890",
						Note: "Returns the docling note body verbatim. Use after `llm catalog` to pick keys. For a single note by note-key rather than parent-key, `sci zot notes read <note-key> --md` adds a `markdown` field (works for HTML notes too, not just extractions).",
					},
					{
						Goal: "Query specific section across many papers (mq pipeline)",
						Cmd:  "sci zot llm query -s transformers -- '.h2 | select(contains(\"Discussion\"))'",
						Note: "mq is jq-for-markdown. Selectors: .h1/.h2/.heading/.text/.code. Filter via select(...) + ||/&&.",
					},
					{
						Goal: "Extract a PDF I haven't extracted yet (auto-applies the has-markdown tag)",
						Cmd:  "sci zot content extract ABC12345 --apply",
						Note: "Runs docling and posts the paper's text. Dry-runs without --apply. Re-extract in place with `content refresh`, remove with `content drop`, list what has one with `content list`. Bulk: `sci zot extract-lib`; --plan previews.",
					},
				},
			},
			{
				Title: "Authoring",
				Entries: []zot.GuideEntry{
					{
						Goal: "Add a paper from OpenAlex / DOI / arXiv",
						Cmd:  "sci zot item add --openalex 10.1038/nature12373 --collection ABC12345",
						Note: "Resolves metadata from OpenAlex; layer --tag, --collection, --author over the auto-fill.",
					},
					{
						Goal: "Add a book chapter / proceedings paper by hand",
						Cmd:  "sci zot item add --type bookSection --title \"A Chapter\" --author \"Manning, Jeremy\" --creator \"editor:Gazzaniga, Michael\" --publication \"The Volume\" --field pages=45-70",
						Note: "--publication is the VENUE and lands in whichever field the type names it; --field name=value and --creator type:name reach anything else the type declares. All validated against the type's Zotero schema first, so a bad name exits 2 listing the valid ones. --creator is add-only: a PATCH clobbers arrays.",
					},
					{
						Goal: "Drag-drop import a PDF (uses Zotero desktop's recognizer)",
						Cmd:  "sci zot import paper.pdf",
						Note: "Requires Zotero desktop running. Bypasses --library (writes to whatever desktop has selected).",
					},
					{
						Goal: "Attach a child note (markdown or HTML)",
						Cmd:  "sci zot item note add ABC12345 --body \"my thoughts\"",
						Note: "Tag with --tag. This is for notes YOU write; paper text goes through `sci zot content extract`.",
					},
					{
						Goal: "Attach a PDF you already have on disk, safely across a batch",
						Cmd:  "sci zot item attach ABC12345 ./paper.pdf --skip-existing",
						Note: "A bare attach is NOT idempotent — twice makes two attachments. --skip-existing no-ops on a same-md5 child, which is what makes a batch resumable. Never resume off local `item children`: it cannot see what this CLI just wrote and answers 0. Use `item children KEY --remote`; both listings carry md5.",
					},
					{
						Goal: "Relate a note to the papers it discusses, without doing it by hand",
						Cmd:  "sci zot link suggest NOTEKEY1",
						Note: "Reads the note and resolves every reference in it — zotero:// item links, @citekeys, DOIs, arXiv ids, [[wikilinks]] — into proposed dc:relation links. Dry-run by default; --apply writes (--yes skips the confirm). Each suggestion carries a status: proposed | already-linked (relation exists, reported not rewritten, so a re-run reads as \"nothing to do\") | unresolved (matched 0 or >1 items — listed, never guessed). Pass --remote to read the note's CURRENT relations live: after a recent `link add` the local mirror still lags, so a stale read re-proposes links that already exist. Refuses docling extractions: those references are the PAPER's bibliography, not yours. Manual pairs: `link add A B`, `link rm A B`, `link list KEY --remote`.",
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
						Goal: "Build a .bib of exactly what a manuscript cites",
						Cmd:  "sci zot bib paper.qmd --out refs.bib",
						Note: "Scans markdown/Quarto for @citekeys, [[wikilinks]], DOIs, arXiv ids, URLs; resolves each against the library. Point at a folder (+ --recursive) to scan many. Refs matching 0 or >1 items are listed as unresolved, never guessed. --format csl-json supported.",
					},
					{
						Goal: "See which of a draft's citations your library cannot account for",
						Cmd:  "sci zot bib draft.md --json",
						Note: "`unresolved` lists every ref that matched 0 or >1 items, with the reason and (on an ambiguity) the competing keys. It is a statement about THIS library, not about the literature — a ref absent here may be a real paper you have not filed. Deciding which is which needs an upstream index and lives on the zot side.",
					},
				},
			},
			{
				Title: "Hygiene",
				Entries: []zot.GuideEntry{
					{
						Goal: "Health check the whole library",
						Cmd:  "sci zot doctor",
						Note: "Runs invalid → missing → orphans → duplicates → citekeys; drill in with any of those as a sub-command. Every check reports over the local database — doctor never writes, dials out, or spends a metered lookup. The repairs they point at live in the zot binary.",
					},
					{
						Goal: "See which items lack the fields you cite with",
						Cmd:  "sci zot doctor missing --field doi,abstract",
						Note: "Fields: title, creators, date, doi, abstract, url, pdf, tags. Severity graded: title=error, creators/date=warn, rest=info.",
					},
					{
						Goal: "Flag publisher-subobject DOIs (article sections, tables, figures, supplements, eLife assets) so OpenAlex can resolve them",
						Cmd:  "sci zot doctor dois",
						Note: "A report. Rewriting each DOI to its parent-paper form is a Zotero write and lives in the zot binary as `zot fix dois`.",
					},
				},
			},
		},
		Tip: "All commands accept --json for machine-readable output. On multi-library accounts pass --library personal|shared in any position (`sci zot --library personal item list` and `sci zot item list --library personal` are equivalent). On single-library accounts it's auto-selected.",
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
