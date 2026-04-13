// Package cli builds the urfave/cli v3 command tree for Zotero operations.
//
// The tree is consumed by two entry points:
//   - cmd/zot (standalone `zot` binary)
//   - cmd/sci/zot.go (integrated `sci zot` subcommand)
//
// Both share identical behavior: any change here shows up in both surfaces.
package cli

import (
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

// experimental is the colored "[experimental]" tag prepended to Usage strings.
var experimental = ui.TUI.TextPink().Render("[experimental]")

// Commands returns the full zot subcommand tree.
// Entry points wrap this in their own root cli.Command.
//
// Top-level layout:
//
//	setup                       configure API key + library
//	info                        library summary (alias: stats)
//	view                        interactive read-only table viewer
//	search  <query>             cross-field search (supports --export, --notes)
//	export                      full-library BibTeX / CSL-JSON export
//	item    <subcommand>        per-item ops (read/add/update/delete/list/open/export)
//	collection <subcommand>     collections (list/create/delete/add/remove)
//	tags    <subcommand>        tags (list/add/remove/delete)
//	notes   <subcommand>        docling extraction notes (list/read/add/update/delete)
//	llm     <subcommand>        [experimental] LLM-agent tools for querying docling notes
//	                            llm {catalog,read,query}
//	doctor  [subcommand]        hygiene: run every check, or drill in via
//	                            doctor {invalid,missing,orphans,duplicates}
//	extract <parent-key>        [experimental] run docling PDF extraction pipeline
//	extract-lib                 [experimental] bulk extract every PDF → child note (via docling)
//
// `item`, `collection`, and `tags` all reuse the leaf commands defined in
// read.go / write.go — the wrapper functions below just parent them under
// the right namespace.
func Commands() []*cli.Command {
	return []*cli.Command{
		setupCommand(),
		infoCommand(),
		viewCommand(),
		searchCommand(),
		libraryExportCommand(),
		itemCommand(),
		collectionCommand(),
		tagsCommand(),
		notesCommand(),
		llmCommand(),
		doctorCommand(),
		extractCommand(),
		extractLibCommand(),
	}
}

// itemCommand groups per-item operations under a single namespace. Nothing
// here is defined inline — the leaf commands live in read.go / write.go.
func itemCommand() *cli.Command {
	return &cli.Command{
		Name:  "item",
		Usage: "Work with individual items (read, add, update, delete, list, open, export)",
		Description: "$ zot item read ABC12345\n" +
			"$ zot item add --type journalArticle --title \"My Paper\"\n" +
			"$ zot item list --limit 20\n" +
			"$ zot item export ABC12345",
		Commands: []*cli.Command{
			readCommand(),
			addCommand(),
			updateCommand(),
			deleteCommand(),
			listCommand(),
			childrenCommand(),
			openCommand(),
			exportCommand(),
		},
	}
}
