// Package cli builds the urfave/cli v3 command tree for Zotero operations,
// mounted under `sci zot` from cmd/sci/zot.go.
//
// # Library scope (`--library personal|shared`)
//
// The persistent `--library` flag threads through every non-setup command via
// PersistentFlags + ValidateLibraryBefore. cmd/sci/zot.go installs both on
// the zot subcommand. The resolved zot.LibraryRef lives on ctx;
// requireAPIClient and openLocalDB pull it back out and route the api client
// / local reader to the user or group library accordingly.
package cli

import (
	"context"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// experimental is the colored "[experimental]" tag prepended to Usage strings.
var experimental = uikit.TUI.TextPink().Render("[experimental]")

// libraryCtxKey is the unexported context key the Before hook uses to stash
// the resolved library scope for subcommand actions.
type libraryCtxKey struct{}

// PersistentFlags are the flags every zot subcommand inherits.
// cmd/sci/zot.go installs these on the `zot` command; they cascade to all
// its subcommands. Deliberately no Destination — the Before hook reads via
// cmd.String("library") so the value is per-invocation, not retained in a
// package-level var (which would leak between tests and between repeated
// runs).
func PersistentFlags() []cli.Flag {
	return []cli.Flag{
		// lint:no-local — persistent flag intentionally cascades to subcommands.
		&cli.StringFlag{
			Name:  "library",
			Usage: "Zotero library to target: personal or shared (required)",
		},
	}
}

// ValidateLibraryBefore is the Before hook that validates the --library
// value (if supplied) and pins a *libraryHolder on ctx so leaves can
// resolve scope via ensureLibraryScope.
//
// It deliberately does NOT enforce that --library is present — doing so at
// this level would shadow help for sub-namespaces (`sci zot item` with no
// further args would error instead of dumping help). Leaves call
// ensureLibraryScope (via openLocalDB / requireAPIClient) which auto-
// selects when only one library is configured, prompts when both are and
// the session is interactive, or errors in --json mode.
//
// Commands that don't need --library at all (`setup`, `info` without a
// flag, `find`, `import`, `guide`) simply never call ensureLibraryScope.
// The holder is still installed on ctx — that's harmless and keeps the
// hook uniform.
//
// Unknown subcommands are handled upstream by cmdutil.RejectUnknownSubcommand
// (wired tree-wide by cmdutil.WireNamespaceDefaults in buildRoot), so they
// never reach this hook.
func ValidateLibraryBefore(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	val := cmd.String("library")
	holder := &libraryHolder{
		JSONMode: cmdutil.IsJSON(cmd),
	}
	if val != "" {
		if err := zot.ValidateLibraryScope(val); err != nil {
			return ctx, err
		}
		holder.HasFlag = true
		holder.Partial = zot.LibraryScope(val)
	}
	ctx = withLibraryHolder(ctx, holder)
	if val != "" {
		// Back-compat: the (Scope-only) ref on libraryCtxKey is still
		// consulted by older callers. New code reads via libraryHolder.
		ctx = context.WithValue(ctx, libraryCtxKey{}, zot.LibraryRef{Scope: zot.LibraryScope(val)})
	}
	return ctx, nil
}

// LibraryFromContext returns the (partial) library ref the Before hook
// stashed when --library was set. Returns false if --library was unset.
// Most leaves now consult ensureLibraryScope via openLocalDB /
// requireAPIClient instead — those drive the auto-select / prompt / error
// flow and return a fully-resolved LibraryRef.
func LibraryFromContext(ctx context.Context) (zot.LibraryRef, bool) {
	ref, ok := ctx.Value(libraryCtxKey{}).(zot.LibraryRef)
	return ref, ok
}

// Commands returns the full zot subcommand tree.
// Entry points wrap this in their own root cli.Command.
//
// Top-level layout:
//
//	guide                       agent-friendly cheat sheet of common workflows
//	setup                       configure API key + library
//	info                        library summary (alias: stats)
//	view                        interactive read-only table viewer
//	search  <query>             cross-field search (supports --export, --notes)
//	find    <subcommand>        OpenAlex paper/author lookup (works/authors)
//	export                      full-library BibTeX / CSL-JSON export
//	import  <path>              drag-drop import via Zotero desktop (metadata recognition)
//	item    <subcommand>        per-item ops (read/add/update/delete/list/open/export)
//	collection <subcommand>     collections (list/create/delete/add/remove)
//	saved-search <subcommand>   saved searches (list/show/create/update/delete)
//	tags    <subcommand>        tags (list/add/remove/delete)
//	notes   <subcommand>        docling extraction notes (list/read/add/update/delete)
//	llm     <subcommand>        [experimental] LLM-agent tools for querying docling notes
//	                            llm {catalog,read,query}
//	doctor  [subcommand]        hygiene: run every check, or drill in via
//	                            doctor {invalid,missing,orphans,duplicates}
//	graph   <subcommand>        traverse citation relationships (library + OpenAlex)
//	extract <parent-key>        [experimental] run docling PDF extraction pipeline
//	extract-lib                 [experimental] bulk extract every PDF → child note (via docling)
//
// `item`, `collection`, and `tags` all reuse the leaf commands defined in
// read.go / write.go — the wrapper functions below just parent them under
// the right namespace.
func Commands() []*cli.Command {
	return []*cli.Command{
		guideCommand(),
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
		Description: "$ sci zot --library personal item read ABC12345\n" +
			"$ sci zot --library personal item add --type journalArticle --title \"My Paper\"\n" +
			"$ sci zot --library personal item list --limit 20\n" +
			"$ sci zot --library shared item export ABC12345",
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
