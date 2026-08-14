// Package cli builds the urfave/cli v3 command tree for Zotero operations,
// mounted under `sci zot` from cmd/sci/zot.go.
//
// # Library scope (`--library personal|shared`)
//
// The persistent `--library` flag threads through every non-setup command via
// PersistentFlags + ValidateLibraryBefore. cmd/sci/zot.go installs both on
// the zot subcommand. The resolved zot.LibraryRef lives on ctx; openLocalDB
// pulls it back out and scopes the local reader to the user or group
// library accordingly.
package cli

import (
	"context"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

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
			Usage: "Zotero library to target: personal, shared, or all (auto-selected or prompted when omitted; `all` merges both and is accepted by search, bib, export --format ndjson, and browse)",
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
// ensureLibraryScope (via openLocalDB) which auto-selects when only one
// library is configured, prompts when both are and the session is
// interactive, or errors in --json mode.
//
// Commands that don't need --library at all (`setup`, `info` without a
// flag, `guide`) simply never call ensureLibraryScope.
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
// Most leaves now consult ensureLibraryScope via openLocalDB instead — that
// drives the auto-select / prompt / error flow and returns a fully-resolved
// LibraryRef.
func LibraryFromContext(ctx context.Context) (zot.LibraryRef, bool) {
	ref, ok := ctx.Value(libraryCtxKey{}).(zot.LibraryRef)
	return ref, ok
}

// Commands returns the full zot subcommand tree.
// Entry points wrap this in their own root cli.Command.
//
// Top-level layout — every verb here answers from the local
// zotero.sqlite, and there is nothing else:
//
//	guide                       agent-friendly cheat sheet of common workflows
//	setup                       locate the Zotero data directory
//	info                        library summary (alias: stats)
//	view                        interactive read-only table viewer
//	search  <query>             cross-field search (supports --export, --notes)
//	browse                      interactive search-and-open REPL (PDFs open in the system viewer)
//	export                      full-library BibLaTeX / CSL-JSON / NDJSON export
//	bib     <file-or-dir>       bibliography of exactly the items a document cites
//	item    <subcommand>        per-item reads (read/list/children/open/export)
//	collection <subcommand>     collections (list/browse)
//	tags    <subcommand>        tags (list/browse)
//	notes   <subcommand>        the notes YOU wrote (list/read)
//	link    <subcommand>        read an item's dc:relation links (list)
//	doctor  [subcommand]        hygiene reports: run every check, or drill in via
//	                            doctor {invalid,missing,orphans,duplicates,citekeys,dois}
//
// Verbs that once lived here and now belong to the sibling `zot` binary
// (every write, every upstream index, every extraction) are NOT registered:
// they are plain unknown commands, rejected by
// cmdutil.WireNamespaceDefaults like any other typo.
//
// `item` and `collection` reuse the leaf commands defined in read.go /
// write.go — the wrapper functions below just parent them under the right
// namespace.
func Commands() []*cli.Command {
	return []*cli.Command{
		guideCommand(),
		setupCommand(),
		infoCommand(),
		viewCommand(),
		searchCommand(),
		browseCommand(),
		libraryExportCommand(),
		bibCommand(),
		itemCommand(),
		collectionCommand(),
		tagsCommand(),
		notesCommand(),
		linkCommand(),
		doctorCommand(),
	}
}

// itemCommand groups per-item operations under a single namespace. Nothing
// here is defined inline — the leaf commands live in read.go.
func itemCommand() *cli.Command {
	return &cli.Command{
		Name:  "item",
		Usage: "Work with individual items (read, list, children, open, export)",
		Description: "$ sci zot --library personal item read ABC12345\n" +
			"$ sci zot --library personal item list --limit 20\n" +
			"$ sci zot --library personal item children 6R45EVSB\n" +
			"$ sci zot --library shared item export ABC12345",
		Commands: []*cli.Command{
			readCommand(),
			listCommand(),
			childrenCommand(),
			openCommand(),
			exportCommand(),
		},
	}
}
