package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot"
)

func TestBuildRemoteArgs_Basic(t *testing.T) {
	t.Parallel()
	got := BuildRemoteArgs("mbp", []string{"zot", "content", "extract", "ABC12345", "--apply"}, false)

	if got[0] != "ssh" {
		t.Fatalf("argv[0] = %q, want ssh", got[0])
	}
	if slices.Contains(got, "-t") {
		t.Error("non-interactive argv must not allocate a tty")
	}
	if !slices.Contains(got, "mbp") {
		t.Errorf("host missing from argv: %v", got)
	}

	remote := got[len(got)-1]
	// Login shell so the remote PATH resolves sci and docling.
	if !strings.HasPrefix(remote, "exec $SHELL -lc ") {
		t.Errorf("remote command not wrapped in a login shell: %q", remote)
	}
	// Recursion guard: a remote config that itself says runner=ssh must
	// be forced local.
	if !strings.Contains(remote, zot.EnvExtractRunner+"="+zot.RunnerLocal) {
		t.Errorf("remote command missing the runner env guard: %q", remote)
	}
	for _, part := range []string{"sci", "zot", "content", "extract", "ABC12345", "--apply"} {
		if !strings.Contains(remote, part) {
			t.Errorf("remote command missing %q: %q", part, remote)
		}
	}
}

func TestBuildRemoteArgs_Interactive(t *testing.T) {
	t.Parallel()
	got := BuildRemoteArgs("mbp", []string{"zot", "extract-lib"}, true)
	if !slices.Contains(got, "-t") {
		t.Errorf("interactive argv missing -t: %v", got)
	}
}

// TestBuildRemoteArgs_QuotingSurvivesShells: an argument with spaces and
// a single quote must survive BOTH remote shells (sshd's `$SHELL -c` and
// the inner `$SHELL -lc`). Simulate each unwrapping with a minimal
// single-quote-aware splitter and check the original arg comes back.
func TestBuildRemoteArgs_QuotingSurvivesShells(t *testing.T) {
	t.Parallel()
	nasty := `it's a "test" file.pdf`
	got := BuildRemoteArgs("mbp", []string{"zot", "content", "extract", nasty}, false)
	outer := got[len(got)-1]

	// sshd hands `outer` to the login shell via -c; the inner command's
	// single argument is the ShellQuote'd remote command line.
	innerWords := splitShellWords(t, strings.TrimPrefix(outer, "exec $SHELL -lc "))
	if len(innerWords) != 1 {
		t.Fatalf("inner shell should receive exactly one -c argument, got %v", innerWords)
	}
	words := splitShellWords(t, innerWords[0])
	if !slices.Contains(words, nasty) {
		t.Errorf("argument did not survive double shell evaluation: %v", words)
	}
}

// splitShellWords is a minimal POSIX single-quote-aware word splitter —
// just enough to verify ShellQuote round-trips (it understands '...' and
// the '\” escape, not double quotes or globbing).
func splitShellWords(t *testing.T, s string) []string {
	t.Helper()
	var words []string
	var cur strings.Builder
	inQuote := false
	started := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
			started = true
		case c == '\\' && !inQuote && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			started = true
		case c == ' ' && !inQuote:
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if inQuote {
		t.Fatalf("unbalanced quotes in %q", s)
	}
	if started {
		words = append(words, cur.String())
	}
	return words
}
