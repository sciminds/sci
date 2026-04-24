// Package cli builds the urfave/cli v3 command tree for Zotero operations.
//
// The tree is consumed by two entry points:
//   - cmd/zot (standalone `zot` binary)
//   - cmd/sci/zot.go (integrated `sci zot` subcommand)
//
// Both share identical behavior: any change here shows up in both surfaces.
//
// # Library scope (`--library personal|shared`)
//
// The persistent `--library` flag threads through every non-setup command via
// PersistentFlags + ValidateLibraryBefore. Entry points install both on their
// root command. The resolved zot.LibraryRef lives on ctx; requireAPIClient
// and openLocalDB pull it back out and route the api client / local reader
// to the user or group library accordingly.
package cli

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// experimental is the colored "[experimental]" tag prepended to Usage strings.
var experimental = uikit.TUI.TextPink().Render("[experimental]")

// libraryCtxKey is the unexported context key the Before hook uses to stash
// the resolved library scope for subcommand actions.
type libraryCtxKey struct{}

// PersistentFlags are the root-level flags that every zot subcommand inherits.
// Entry points install these on the `zot` root command; they cascade to all
// subcommands. Deliberately no Destination — the Before hook reads via
// cmd.String("library") so the value is per-invocation, not retained in a
// package-level var (which would leak between tests and between repeated
// runs of the root command).
func PersistentFlags() []cli.Flag {
	return []cli.Flag{
		// lint:no-local — persistent flag intentionally cascades to subcommands.
		&cli.StringFlag{
			Name:  "library",
			Usage: "Zotero library to target: personal or shared (required)",
		},
	}
}

// libraryExemptCommands are subcommands where --library is optional.
//   - setup configures both libraries at once.
//   - info summarizes both when no scope is given; --library narrows.
//   - find hits OpenAlex, not Zotero — scope is meaningless there.
//   - import goes through Zotero desktop's connector, which writes to
//     whichever library is currently selected in the desktop UI.
var libraryExemptCommands = map[string]bool{
	"setup":  true,
	"info":   true,
	"find":   true,
	"import": true,
}

// ValidateLibraryBefore is the Before hook that validates --library and
// stashes the scope in the context for every subcommand action.
//
// Exempt: subcommands listed in libraryExemptCommands, empty args (help
// listing), and --help/--version (handled by urfave/cli before Before fires).
// For exempt commands, --library is still validated if supplied so typos
// are caught early.
func ValidateLibraryBefore(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	first := cmd.Args().First()
	val := cmd.String("library")

	if first == "" || libraryExemptCommands[first] {
		if val == "" {
			return ctx, nil
		}
		// Still validate the value if the user supplied one.
		if err := zot.ValidateLibraryScope(val); err != nil {
			return ctx, err
		}
		ref := zot.LibraryRef{Scope: zot.LibraryScope(val)}
		return context.WithValue(ctx, libraryCtxKey{}, ref), nil
	}

	if val == "" {
		return ctx, fmt.Errorf("--library is required (values: personal, shared)")
	}
	if err := zot.ValidateLibraryScope(val); err != nil {
		return ctx, err
	}
	// Carry just the scope through ctx — full resolution (reading config,
	// lazy-probing shared) happens inside requireAPIClient/openLocalDB so
	// tests that don't care about the config can still exercise routing.
	ref := zot.LibraryRef{Scope: zot.LibraryScope(val)}
	return context.WithValue(ctx, libraryCtxKey{}, ref), nil
}

// LibraryFromContext returns the library ref the Before hook stashed, if any.
// Only the Scope field is guaranteed populated here — full resolution
// (APIPath, LocalID, Name) is performed by callers that have a config.
func LibraryFromContext(ctx context.Context) (zot.LibraryRef, bool) {
	ref, ok := ctx.Value(libraryCtxKey{}).(zot.LibraryRef)
	return ref, ok
}

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
//	saved-search <subcommand>   saved searches (list/show/create/update/delete)
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
		findCommand(),
		libraryExportCommand(),
		importCommand(),
		itemCommand(),
		collectionCommand(),
		savedSearchCommand(),
		tagsCommand(),
		notesCommand(),
		llmCommand(),
		doctorCommand(),
		graphCommand(),
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
		Description: "$ zot --library personal item read ABC12345\n" +
			"$ zot --library personal item add --type journalArticle --title \"My Paper\"\n" +
			"$ zot --library personal item list --limit 20\n" +
			"$ zot --library shared item export ABC12345",
		Commands: []*cli.Command{
			readCommand(),
			addCommand(),
			itemAttachCommand(),
			updateCommand(),
			deleteCommand(),
			listCommand(),
			childrenCommand(),
			openCommand(),
			exportCommand(),
			itemNoteCommand(),
		},
	}
}
