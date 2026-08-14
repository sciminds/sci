package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/urfave/cli/v3"
)

// runSciTree mounts the production zot tree the way cmd/sci does — under a
// root that declares the persistent --json flag with the destination
// main() reads after Run returns — and runs argv through it. out receives
// anything urfave writes (help pages); pass io.Discard when you don't care.
//
// It exists because runZotTree's bare `zot` root declares no --json, and
// the whole point of these tests is what happens to that flag. It takes
// treeRunMu for the same reason runZotTree does: two trees built out of
// Commands() parsing at once is the data race described there.
func runSciTree(t *testing.T, out io.Writer, argv ...string) (jsonMode bool, err error) {
	t.Helper()
	treeRunMu.Lock()
	defer treeRunMu.Unlock()
	root := &cli.Command{
		Name:   "sci",
		Flags:  []cli.Flag{cmdutil.JSONFlag(&jsonMode)},
		Writer: out,
		Commands: []*cli.Command{{
			Name:     "zot",
			Flags:    PersistentFlags(),
			Commands: Commands(),
		}},
	}
	err = root.Run(context.Background(), argv)
	return jsonMode, err
}

// renderFailure runs argv and then renders whatever came back exactly the
// way main() does: cmdutil.HandleError fed the root's --json destination.
// It returns what landed on stdout plus the process exit code, which
// together are the entire machine-facing answer.
func renderFailure(t *testing.T, argv ...string) (stdout string, exit int) {
	t.Helper()
	jsonMode, err := runSciTree(t, io.Discard, argv...)
	if err == nil {
		t.Fatalf("`%s` should refuse, got nil", strings.Join(argv, " "))
	}
	read, restore := captureStdout(t)
	defer restore()
	exit = cmdutil.HandleError(err, jsonMode)
	return read(), exit
}

// TestStubJSONIsPositionIndependent — a stub's SkipFlagParsing swallows a
// TRAILING --json along with the old write flags, and that is the position
// the guide and every help screen teach (`sci zot bib draft.md --json`).
// Before the fix, `sci zot item add --json` left stdout EMPTY and put the
// refusal on stderr as human text, so an agent got a JSON parse error on
// empty input instead of the Try telling it where the verb went.
//
// The envelope must be byte-identical in both positions: a consumer must
// not be able to tell which one it used.
func TestStubJSONIsPositionIndependent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		leading  []string
		trailing []string
	}{
		{
			name:     "moved verb",
			leading:  []string{"sci", "--json", "zot", "item", "add"},
			trailing: []string{"sci", "zot", "item", "add", "--json"},
		},
		{
			name:     "moved verb still carrying its old write flags",
			leading:  []string{"sci", "--json", "zot", "item", "add", "--type", "journalArticle", "--title", "X"},
			trailing: []string{"sci", "zot", "item", "add", "--type", "journalArticle", "--title", "X", "--json"},
		},
		{
			name:     "namespace stub reached through its subcommand positional",
			leading:  []string{"sci", "--json", "zot", "content", "read", "ABC12345"},
			trailing: []string{"sci", "zot", "content", "read", "ABC12345", "--json"},
		},
		{
			name:     "retired-outright verb",
			leading:  []string{"sci", "--json", "zot", "saved-search", "create", "Recent ML"},
			trailing: []string{"sci", "zot", "saved-search", "create", "Recent ML", "--json"},
		},
		{
			name:     "--json=true after a positional",
			leading:  []string{"sci", "--json", "zot", "tags", "add", "ABC12345", "neuroimaging"},
			trailing: []string{"sci", "zot", "tags", "add", "ABC12345", "neuroimaging", "--json=true"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leadOut, leadExit := renderFailure(t, tc.leading...)
			trailOut, trailExit := renderFailure(t, tc.trailing...)

			if leadOut == "" {
				t.Fatalf("leading --json emitted nothing on stdout for `%s`", strings.Join(tc.leading, " "))
			}
			if trailOut != leadOut {
				t.Errorf("trailing --json must emit the same envelope as leading:\n leading  = %q\n trailing = %q",
					leadOut, trailOut)
			}
			if trailExit != leadExit {
				t.Errorf("exit code differs by --json position: leading %d, trailing %d", leadExit, trailExit)
			}

			var env struct {
				OK    bool `json:"ok"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Fix     string `json:"fix"`
					Try     string `json:"try"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(trailOut), &env); err != nil {
				t.Fatalf("trailing --json must be parseable JSON, got %q: %v", trailOut, err)
			}
			if env.OK {
				t.Error("envelope must carry ok=false")
			}
			if env.Error.Try == "" {
				t.Error("the Try is the whole point of the stub; it must reach the machine consumer")
			}
		})
	}
}

// TestStubJSONIgnoresArgsAfterDoubleDash — after a bare `--`, `--json` is a
// literal argument in urfave's own parsing, so a stub must not read it as a
// mode switch either. `sci zot llm query -s x -- --json` asked to pass the
// string through.
func TestStubJSONIgnoresArgsAfterDoubleDash(t *testing.T) {
	for _, argv := range [][]string{
		{"sci", "zot", "llm", "query", "-s", "transformers", "--", "--json"},
		{"sci", "zot", "item", "add", "--json=false"},
	} {
		t.Run(strings.Join(argv[1:], " "), func(t *testing.T) {
			jsonMode, err := runSciTree(t, io.Discard, argv...)
			if err == nil {
				t.Fatal("stub should still refuse")
			}
			if jsonMode {
				t.Errorf("`%s` must not switch the output mode", strings.Join(argv, " "))
			}
		})
	}
}

// TestStubHelpShowsItsDescription — `--help` arrives as a positional under
// SkipFlagParsing, so it used to print the one-line error. The authored
// Description is the clearest statement of the boundary in the tree, and
// it was reachable only through the `sci help` TUI, which a scripting user
// never opens.
func TestStubHelpShowsItsDescription(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want []string
	}{
		{
			name: "moved verb, --help",
			argv: []string{"sci", "zot", "item", "add", "--help"},
			want: []string{"zot item add", "never writes to your library"},
		},
		{
			name: "moved verb, -h",
			argv: []string{"sci", "zot", "item", "add", "-h"},
			want: []string{"zot item add", "never writes to your library"},
		},
		{
			name: "namespace stub, --help after its subcommand positional",
			argv: []string{"sci", "zot", "content", "read", "--help"},
			want: []string{"zot read", "never writes to your library"},
		},
		{
			name: "retired-outright verb keeps its prose remedy",
			argv: []string{"sci", "zot", "saved-search", "create", "--help"},
			want: []string{"Zotero desktop", "cannot evaluate it"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			_, err := runSciTree(t, &out, tc.argv...)
			if err != nil {
				t.Fatalf("`%s` should print help and succeed, got %v", strings.Join(tc.argv, " "), err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help for `%s` must carry %q; got:\n%s",
						strings.Join(tc.argv, " "), want, out.String())
				}
			}
		})
	}
}

// TestStubHelpNeverDumpsHelpUnderJSON — same discipline as
// cmdutil.UsageErrorf: the consumer under --json is a machine, and a
// styled help page on its stdout is not the envelope it is parsing.
func TestStubHelpNeverDumpsHelpUnderJSON(t *testing.T) {
	for _, argv := range [][]string{
		{"sci", "--json", "zot", "item", "add", "--help"},
		{"sci", "zot", "item", "add", "--help", "--json"},
	} {
		t.Run(strings.Join(argv[1:], " "), func(t *testing.T) {
			var out bytes.Buffer
			jsonMode, err := runSciTree(t, &out, argv...)
			if err == nil {
				t.Fatal("--json must get the error envelope, not a help page")
			}
			if !jsonMode {
				t.Error("--json must still be honored alongside --help")
			}
			if out.Len() > 0 {
				t.Errorf("no styled help under --json; got:\n%s", out.String())
			}
		})
	}
}

// TestSurvivingVerbsKeepTheirOwnHelp guards the blast radius: the stubs'
// help interception must not leak into a live read verb, whose --help is
// urfave's own and whose `--` handling is real flag parsing.
func TestSurvivingVerbsKeepTheirOwnHelp(t *testing.T) {
	var out bytes.Buffer
	_, err := runSciTree(t, &out, "sci", "zot", "search", "--help")
	if err != nil {
		t.Fatalf("`zot search --help` should succeed, got %v", err)
	}
	if !strings.Contains(out.String(), "search") {
		t.Errorf("`zot search --help` lost its help page; got:\n%s", out.String())
	}
}
