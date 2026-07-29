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

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
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
						Note: "Runs docling and posts the paper's text. Dry-runs without --apply. Re-extract in place with `content refresh`, remove with `content drop`, list what has one with `content list`. Bulk: `sci zot extract-lib`.",
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
						Goal: "Export the whole library (or a slice) to BibTeX / CSL-JSON",
						Cmd:  "sci zot export --out refs.bib",
						Note: "Filter with --collection / --tag / --type; --format csl-json for a processor-friendly dump. With --out, writes a .zotero-citekeymap.json sidecar so drifted synthesized keys get a biblatex `ids = {oldkey}` alias next run.",
					},
					{
						Goal: "Build a .bib of exactly what a manuscript cites",
						Cmd:  "sci zot bib paper.qmd --out refs.bib",
						Note: "Scans markdown/Quarto for @citekeys, [[wikilinks]], DOIs, arXiv ids, URLs; resolves each against the library. Point at a folder (+ --recursive) to scan many. Refs matching 0 or >1 items are listed as unresolved, never guessed. --format csl-json supported.",
					},
					{
						Goal: "Check a draft's citations are real before you submit it",
						Cmd:  "sci zot bib draft.md --verify",
						Note: "Classifies every unresolved ref: external (real work, missing from the library — carries a runnable `item add --openalex` fix), not-found (no citation index AND no DOI registry has it — the fabricated-citation signal), ambiguous (>1 library match, fix shows the candidates), unchecked (cite-key/wikilink/URL — no identifier to verify, decide by hand), error (lookup failed, standing unknown). Needs network. Retracted works are flagged on the match.",
					},
				},
			},
			{
				Title: "Hygiene",
				Entries: []zot.GuideEntry{
					{
						Goal: "Health check the whole library",
						Cmd:  "sci zot doctor",
						Note: "Runs invalid → missing → orphans → duplicates. Drill in with `doctor invalid|missing|orphans|duplicates`.",
					},
					{
						Goal: "Find items missing PDFs and try to recover them",
						Cmd:  "sci zot doctor pdfs",
						Note: "OpenAlex-led lookup. Defaults to the local 'missing-pdf' collection; pass --saved-search NAME|KEY to drive off a Zotero saved search live (good when the local SQLite is stale or you've removed the manual collection), or --keys-from FILE|- to feed an explicit key list (one 8-char key per line). Add --download / --attach to write back; default is read-only triage.",
					},
					{
						Goal: "Flag publisher-subobject DOIs (Frontiers /abstract, PLOS .tNNN, PNAS supplements) so OpenAlex can resolve them",
						Cmd:  "sci zot doctor dois",
						Note: "Read-only by default. Add --fix for a dry-run; --fix --apply to patch the DOI field via the Web API.",
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
