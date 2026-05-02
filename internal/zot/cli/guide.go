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
						Cmd:  "sci zot search \"large language models\"",
						Note: "Local title/DOI/publication/creators; add --remote for Zotero Web fulltext (matches abstract + notes + PDFs).",
					},
					{
						Goal: "Find papers I don't have yet (OpenAlex)",
						Cmd:  "sci zot find works \"theory of mind\"",
						Note: "Compact JSON shape by default (~12 fields/work). --verbose for raw OpenAlex record.",
					},
					{
						Goal: "Lookup an item by exact key",
						Cmd:  "sci zot item read ABC12345",
						Note: "Add --remote when the local DB may be stale (e.g. just-created items).",
					},
					{
						Goal: "List all collections / tags",
						Cmd:  "sci zot collection list",
						Note: "Or `sci zot tags list`. Both fast/local.",
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
						Note: "Returns the docling note body verbatim. Use after `llm catalog` to pick keys.",
					},
					{
						Goal: "Query specific section across many papers (mq pipeline)",
						Cmd:  "sci zot llm query -s transformers -- '.h2 | select(contains(\"Discussion\"))'",
						Note: "mq is jq-for-markdown. Selectors: .h1/.h2/.heading/.text/.code. Filter via select(...) + ||/&&.",
					},
					{
						Goal: "Extract a PDF I haven't extracted yet (auto-applies the has-markdown tag)",
						Cmd:  "sci zot extract ABC12345",
						Note: "Runs docling, attaches result as a child markdown note. Bulk: `sci zot extract-lib`.",
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
						Note: "Tag with --tag. For docling extractions use `sci zot extract` instead.",
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
						Note: "OpenAlex-led lookup. Add --download / --attach to write back; default is read-only triage.",
					},
				},
			},
		},
		Tip: "All commands accept --json for machine-readable output. Pass --library personal|shared on multi-library accounts; otherwise it's auto-selected.",
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
