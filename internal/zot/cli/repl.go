package cli

// `zot browse` — an inline search-and-open REPL: the fast alternative to
// booting the Zotero desktop app when all you want is "find the paper,
// open the PDF". A readline-style loop (history + emacs editing via
// x/term) in normal terminal flow, so results land in scrollback instead
// of an alternate screen.
//
// Grammar: bare text searches (same DSL as `zot search`), a bare number
// opens that hit's PDF in the system viewer, and `:commands` steer the
// session (`:library`, `:limit`, `:help`, `:quit`).
//
// Terminal discipline: raw mode is held ONLY inside the readLine wrapper
// (with its own deferred Restore), so every print happens in cooked mode
// through cmdutil.HumanWriter and a panic can't strand the terminal raw.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// errQuitREPL signals a clean user-initiated exit (`:q`); run treats it
// like EOF rather than a failure.
var errQuitREPL = errors.New("quit repl")

// browseStdoutIsTTY reports whether stdout can host the REPL. Split from
// defaultIsInteractive (stdin) because a piped stdout with a terminal
// stdin is still not a place to run raw-mode line editing. Tests override.
var browseStdoutIsTTY = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// browseREPL is the loop state. Every side effect that would touch the
// outside world (line input, `open`, stat) is an injected field so tests
// can script a session end to end.
type browseREPL struct {
	cfg   *zot.Config
	ref   zot.LibraryRef // current scope; swapped by :library
	db    local.Reader   // reopened on :library
	limit int            // hits per search; :limit
	last  []local.Item   // previous hits, indexed by the number grammar

	out      io.Writer
	readLine func() (string, error)
	launch   func(path string) error
	stat     func(path string) error
}

// run drives the loop until EOF (Ctrl-D / Ctrl-C) or `:q`.
func (r *browseREPL) run(ctx context.Context) error {
	for {
		line, err := r.readLine()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := r.dispatch(ctx, strings.TrimSpace(line)); err != nil {
			if errors.Is(err, errQuitREPL) {
				return nil
			}
			return err
		}
	}
}

// dispatch routes one input line: empty → no-op, `:cmd` → command,
// integer → open, anything else → search. Only genuinely broken state
// (a failed query against the DB) returns an error; user mistakes print
// guidance and keep the loop alive.
func (r *browseREPL) dispatch(ctx context.Context, line string) error {
	switch {
	case line == "":
		return nil
	case strings.HasPrefix(line, ":"), line == "?":
		return r.command(ctx, line)
	}
	if n, err := strconv.Atoi(line); err == nil {
		r.doOpen(n)
		return nil
	}
	return r.doSearch(line)
}

// command handles the `:` grammar. Unknown commands teach `:h` instead
// of erroring out of the session.
func (r *browseREPL) command(ctx context.Context, line string) error {
	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]
	switch cmd {
	case ":q", ":quit":
		return errQuitREPL
	case ":h", ":help", "?":
		r.printHelp()
	case ":lib", ":library":
		if len(args) != 1 {
			r.sayf("usage: :library personal|shared|all")
			return nil
		}
		r.setLibrary(args[0])
	case ":limit":
		if len(args) != 1 {
			r.sayf("usage: :limit N")
			return nil
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			r.sayf("usage: :limit N (a positive number)")
			return nil
		}
		r.limit = n
		r.sayf("limit set to %d (applies to the next search)", n)
	default:
		r.sayf("unknown command %s — :h for help", cmd)
	}
	return nil
}

// doSearch runs one query through the same pipeline as `zot search`:
// the DSL parse and ranking in SearchWithTotal.
func (r *browseREPL) doSearch(query string) error {
	items, total, err := r.db.SearchWithTotal(query, r.limit, local.SearchOptions{})
	if err != nil {
		return err
	}
	r.last = items
	if len(items) == 0 {
		r.sayf("no hits — searched %s", localSearchScope())
		return nil
	}
	_, _ = fmt.Fprint(r.out, renderBrowseHits(items, nil, total, r.ref.Scope == zot.LibAll))
	return nil
}

// doOpen launches result n's PDF in the system viewer. Every failure is
// a printed message, never a session-ending error — a missing PDF on one
// paper shouldn't cost the search that found it.
func (r *browseREPL) doOpen(n int) {
	if len(r.last) == 0 {
		r.sayf("no results yet — search first, then pick a number")
		return
	}
	if n < 1 || n > len(r.last) {
		r.sayf("%d is out of range — pick 1-%d", n, len(r.last))
		return
	}
	item := r.last[n-1]
	att, err := r.db.ResolvePDFAttachment(item.Key)
	if err != nil {
		r.sayf("no PDF for %q (%s)", item.Title, item.Key)
		return
	}
	path := zot.AttachmentPath(r.cfg.DataDir, &local.Attachment{Key: att.Key, Filename: att.Filename})
	if err := r.stat(path); err != nil {
		r.sayf("PDF file missing on disk: %s", path)
		return
	}
	if err := r.launch(path); err != nil {
		r.sayf("launch failed: %v", err)
		return
	}
	r.sayf("%s %s", uikit.TUI.TextGreen().Render("opened"), att.Filename)
}

// setLibrary swaps the read pool, reopening the local reader against the
// new scope and dropping the previous hit list (its numbers indexed rows
// that are no longer on screen).
func (r *browseREPL) setLibrary(arg string) {
	if err := zot.ValidateLibraryScope(arg); err != nil {
		r.sayf("%v", err)
		return
	}
	scope := zot.LibraryScope(arg)
	if scope == r.ref.Scope {
		r.sayf("already on %s", scope)
		return
	}
	ref, err := r.cfg.Resolve(scope)
	if err != nil {
		r.sayf("%v", err)
		return
	}
	sel, err := localSelectorFor(r.cfg, ref)
	if err != nil {
		r.sayf("%v", err)
		return
	}
	db, err := local.Open(r.cfg.DataDir, sel)
	if err != nil {
		r.sayf("open %s library: %v", scope, err)
		return
	}
	_ = r.db.Close()
	r.db, r.ref, r.last = db, ref, nil
	r.sayf("library: %s — %s", uikit.TUI.TextBlue().Render(string(ref.Scope)), refName(ref))
}

func (r *browseREPL) printHelp() {
	_, _ = fmt.Fprint(r.out, strings.Join([]string{
		"  type a query          search (same syntax as `sci zot search`: bare terms, author:, year:, tag:, \"phrases\", -negation)",
		"  type a number         open that hit's PDF in the system viewer",
		"  :lib, :library X      switch scope: personal | shared | all",
		"  :limit N              hits per search",
		"  :h, :help, ?          this help",
		"  :q, :quit, Ctrl-D     exit",
		"",
	}, "\n"))
}

// sayf prints one status line inside the loop.
func (r *browseREPL) sayf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.out, "  %s %s\n", uikit.SymArrow, fmt.Sprintf(format, args...))
}

// refName labels a scope for the human header.
func refName(ref zot.LibraryRef) string {
	return cmp.Or(ref.Name, string(ref.Scope))
}

// renderBrowseHits formats the numbered hit list: number, year, first
// author, title, dim citekey when present, and a [shared]/[personal]
// provenance marker only under the merged scope (constant otherwise).
// Snippets ride under their row, dimmed, one line each.
func renderBrowseHits(items []local.Item, snippets map[string]string, total int, mergedScope bool) string {
	var b strings.Builder
	for i, it := range items {
		title := it.Title
		if title == "" {
			title = uikit.TUI.Dim().Render("(untitled)")
		}
		year := uikit.TUI.Dim().Render("(----)")
		if it.Year > 0 {
			year = uikit.TUI.Dim().Render(fmt.Sprintf("(%d)", it.Year))
		}
		author := cmp.Or(firstAuthor(it.Creators), uikit.TUI.Dim().Render("—"))
		line := fmt.Sprintf("%3s %s %s — %s",
			uikit.TUI.TextBlue().Render(strconv.Itoa(i+1)+"."), year, author, title)
		if it.Citekey != "" {
			line += " " + uikit.TUI.Dim().Render("@"+it.Citekey)
		}
		if mergedScope && it.Library != "" {
			line += " " + uikit.TUI.TextPink().Render("["+it.Library+"]")
		}
		b.WriteString(line + "\n")
		if snip := snippets[it.Key]; snip != "" {
			flat := strings.Join(strings.Fields(snip), " ")
			fmt.Fprintf(&b, "     %s\n", uikit.TUI.Dim().Render(flat))
		}
	}
	if len(items) < total {
		fmt.Fprintf(&b, "  %s %d of %d — :limit to see more\n", uikit.SymArrow, len(items), total)
	} else {
		fmt.Fprintf(&b, "  %s %d hit(s)\n", uikit.SymArrow, len(items))
	}
	return b.String()
}

// browseScope picks the launch scope: an explicit --library wins, else
// the widest configured pool (all when a shared group exists, personal
// otherwise). Pure so the defaulting is unit-testable without a TTY.
func browseScope(hasFlag bool, partial zot.LibraryScope, sharedConfigured bool) zot.LibraryScope {
	if hasFlag {
		return partial
	}
	if sharedConfigured {
		return zot.LibAll
	}
	return zot.LibPersonal
}

func browseCommand() *cli.Command {
	return &cli.Command{
		Name:        "browse",
		Usage:       "Interactive search-and-open REPL over the library",
		Description: "$ sci zot browse            # search, pick a number, PDF opens\n$ sci zot browse --library personal",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmdutil.IsJSON(cmd) {
				return cmdutil.Coded(cmdutil.CodeUsage, "browse is interactive-only and has no --json mode").
					WithTry("use `sci zot search` for scriptable queries")
			}
			if !defaultIsInteractive() || !browseStdoutIsTTY() {
				return cmdutil.Coded(cmdutil.CodeUsage, "browse needs a terminal on stdin and stdout (agent / pipe / CI detected)").
					WithTry("use `sci zot search` for non-interactive queries")
			}
			cfg, err := requireConfigCoded()
			if err != nil {
				return err
			}
			holder := libraryHolderFromCtx(ctx)
			if holder == nil {
				return cmdutil.Coded(cmdutil.CodeUsage, "--library is required (values: personal, shared, all)")
			}
			scope := browseScope(holder.HasFlag, holder.Partial, cfg.SharedGroupID != "")
			ref, err := cfg.Resolve(scope)
			if err != nil {
				return err
			}
			holder.Resolved = &ref
			sel, err := localSelectorFor(cfg, ref)
			if err != nil {
				return err
			}
			db, err := local.Open(cfg.DataDir, sel)
			if err != nil {
				return err
			}

			out := cmdutil.HumanWriter()
			_, _ = fmt.Fprintf(out, "  %s Library: %s — %s   %s\n",
				uikit.SymArrow,
				uikit.TUI.TextBlue().Render(string(ref.Scope)),
				uikit.TUI.Dim().Render(refName(ref)),
				uikit.TUI.Dim().Render("(:h for help, Ctrl-D to exit)"),
			)
			for _, w := range localReadWarnings(db, "") {
				_, _ = fmt.Fprintf(out, "  %s %s\n", uikit.SymArrow, w.Message)
			}

			stdinFD := int(os.Stdin.Fd())
			// Belt-and-braces: whatever happens inside the loop, the
			// terminal leaves cooked. The per-read Restore below is the
			// primary mechanism; this one catches a panic mid-print.
			if st, err := term.GetState(stdinFD); err == nil {
				defer func() { _ = term.Restore(stdinFD, st) }()
			}
			t := term.NewTerminal(struct {
				io.Reader
				io.Writer
			}{os.Stdin, os.Stdout}, "zot> ")
			// A degenerate size (some ptys report 0x0) would make the
			// Terminal wrap every rune; keep the 80x24 default instead.
			if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && h > 0 {
				_ = t.SetSize(w, h)
			}
			readLine := func() (string, error) {
				old, err := term.MakeRaw(stdinFD)
				if err != nil {
					return "", err
				}
				defer func() { _ = term.Restore(stdinFD, old) }()
				return t.ReadLine()
			}

			repl := &browseREPL{
				cfg:      cfg,
				ref:      ref,
				db:       db,
				limit:    15,
				out:      out,
				readLine: readLine,
				launch:   zot.LaunchFile,
				stat: func(path string) error {
					_, err := os.Stat(path)
					return err
				},
			}
			// Close whichever handle the session ends on — :library
			// swaps repl.db, closing the superseded one as it goes.
			defer func() { _ = repl.db.Close() }()
			return repl.run(ctx)
		},
	}
}
