package cli

// Tests for `sci zot guide` — both that it produces useful output, and
// (critically) that every command it references actually exists. The
// referenced-command check is the drift fence: if someone renames a leaf
// without updating the guide, this fails immediately.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

func TestGuide_HumanOutput_HasSections(t *testing.T) {
	root := &cli.Command{
		Name:     "zot",
		Flags:    PersistentFlags(),
		Before:   ValidateLibraryBefore,
		Commands: Commands(),
	}
	read, restore := captureStdout(t)
	defer restore()
	if err := root.Run(context.Background(), []string{"zot", "guide"}); err != nil {
		t.Fatalf("guide: %v", err)
	}
	got := stripANSI(read())
	for _, want := range []string{"Discovery", "Full-text extraction", "Authoring", "Hygiene"} {
		if !strings.Contains(got, want) {
			t.Errorf("guide output missing section %q:\n%s", want, got)
		}
	}
}

func TestGuide_JSON_MachineReadable(t *testing.T) {
	root := &cli.Command{
		Name: "zot",
		Flags: append([]cli.Flag{
			cmdutil.JSONFlag(new(bool)),
		}, PersistentFlags()...),
		Before:   ValidateLibraryBefore,
		Commands: Commands(),
	}
	read, restore := captureStdout(t)
	defer restore()
	if err := root.Run(context.Background(), []string{"zot", "--json", "guide"}); err != nil {
		t.Fatalf("guide --json: %v", err)
	}
	raw := read()

	var result zot.GuideResult
	if err := json.Unmarshal(unwrapData(t, []byte(raw)), &result); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, raw)
	}
	if len(result.Sections) < 3 {
		t.Errorf("got %d sections, want at least 3", len(result.Sections))
	}
	totalEntries := 0
	for _, s := range result.Sections {
		totalEntries += len(s.Entries)
	}
	if totalEntries < 8 {
		t.Errorf("got %d entries total, want at least 8 (cheat sheet should cover the common workflows)", totalEntries)
	}
}

// TestGuide_ReferencedCommandsExist is the drift fence: it parses every
// `sci zot …` example in the guide and walks the command tree to verify
// the path resolves. Renaming a command without updating the guide
// trips this immediately.
func TestGuide_ReferencedCommandsExist(t *testing.T) {
	root := &cli.Command{
		Name:     "zot",
		Commands: Commands(),
	}

	guide := guideContent()
	checked := 0
	for _, sec := range guide.Sections {
		for _, e := range sec.Entries {
			path, ok := parseGuideCmd(e.Cmd)
			if !ok {
				t.Errorf("section %q: cannot parse cmd %q", sec.Title, e.Cmd)
				continue
			}
			if !commandExists(root, path) {
				t.Errorf("section %q: cmd %q references missing path: sci zot %s",
					sec.Title, e.Cmd, strings.Join(path, " "))
				continue
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no guide entries were checked — parser is broken")
	}
}

// parseGuideCmd extracts the leaf path from a `sci zot foo bar …` example.
// Returns (path, true) where path is e.g. ["item", "add"] or
// (["doctor", "pdfs"]). Stops at the first flag, positional placeholder
// (starts with `-`, capital letter, `<`, or quote), or value-like token.
func parseGuideCmd(cmd string) ([]string, bool) {
	tokens := strings.Fields(cmd)
	if len(tokens) < 2 || tokens[0] != "sci" || tokens[1] != "zot" {
		return nil, false
	}
	var path []string
	for _, tok := range tokens[2:] {
		if isFlagOrArg(tok) {
			break
		}
		path = append(path, tok)
	}
	if len(path) == 0 {
		return nil, false
	}
	return path, true
}

func isFlagOrArg(tok string) bool {
	if tok == "" {
		return true
	}
	c := tok[0]
	if c == '-' || c == '"' || c == '\'' || c == '<' {
		return true
	}
	// Uppercase first letter signals a placeholder like "ABC12345" or "DOI".
	if c >= 'A' && c <= 'Z' {
		return true
	}
	// Bare-word positional like a query string ("transformers", "theory") —
	// these come AFTER the leaf, so anything we haven't matched as a
	// subcommand by now must be a positional. Distinguish by looking up
	// the token in the tree at the call site instead — here we treat any
	// token containing `:` (mq filter, e.g. `.h2`) or starting with `.`
	// as a non-command.
	if c == '.' || strings.Contains(tok, ":") || strings.Contains(tok, "/") || strings.Contains(tok, ".") {
		return true
	}
	return false
}

// commandExists walks the cli tree to check whether path resolves to a
// real command (leaf or namespace).
func commandExists(root *cli.Command, path []string) bool {
	cur := root
	for _, name := range path {
		next := findChild(cur, name)
		if next == nil {
			return false
		}
		cur = next
	}
	return true
}

func findChild(parent *cli.Command, name string) *cli.Command {
	for _, c := range parent.Commands {
		if c.HasName(name) {
			return c
		}
	}
	return nil
}

// TestGuide_GuideCommandRegistered verifies guide is wired into the zot
// command tree. The drift-fence for the help text pointer ("Description
// mentions guide") lives in cmd/sci/zot_test.go where the description
// itself is defined.
func TestGuide_GuideCommandRegistered(t *testing.T) {
	root := &cli.Command{Name: "zot", Commands: Commands()}
	if findChild(root, "guide") == nil {
		t.Fatal("guide command not registered in Commands()")
	}
}

// TestGuide_ContractTeachesTheEnvelope pins the machine-contract section: an
// agent that reads nothing else must still learn the envelope shape and what
// to do with fix / warnings / truncated.
func TestGuide_ContractTeachesTheEnvelope(t *testing.T) {
	t.Parallel()
	guide := guideContent()
	if len(guide.Contract) == 0 {
		t.Fatal("guide must carry a machine contract")
	}
	joined := strings.Join(guide.Contract, " ")
	for _, must := range []string{
		"fix",       // resubmit-verbatim rule
		"warnings",  // act-before-trusting rule
		"truncated", // raise --limit rule
		`{ok:true`,  // success envelope shape
		`{ok:false`, // error envelope shape
		"Exit 2",    // remediation partition
		"cite keys", // absorption
		"--library", // flag placement
		"not-found", // code vocabulary present
		"snippets",  // the side-map join for --content match evidence
	} {
		if !strings.Contains(joined, must) {
			t.Errorf("contract missing %q:\n%s", must, joined)
		}
	}
}

// TestGuide_SizeBudget is the terse-beats-thorough fence: the JSON pack must
// stay small enough to pull into an agent's context at session start. Raise
// this ceiling only with a reason — every sentence costs reliability.
func TestGuide_SizeBudget(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(guideContent().JSON())
	if err != nil {
		t.Fatal(err)
	}
	const budget = 10 * 1024
	if len(raw) > budget {
		t.Errorf("guide JSON is %d bytes, over the %d budget — trim before growing", len(raw), budget)
	}
}

// TestGuide_PointersInFirstContactErrors verifies the delivery channel: the
// errors an unconfigured or library-less agent hits first must point at the
// guide, because the error channel is the only surface that reaches an agent
// with zero prior context.
func TestGuide_PointersInFirstContactErrors(t *testing.T) {
	t.Parallel()
	lib := mustCoded(t, errLibraryRequiredArgs("reason", []string{"sci", "zot", "search", "x"}))
	if !strings.Contains(lib.Try, "sci zot guide --json") {
		t.Errorf("library-required Try should point at the guide, got %q", lib.Try)
	}
}
