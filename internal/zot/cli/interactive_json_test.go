package cli

// The interactive read surfaces (`view`, `tags browse`, `collection browse`)
// must refuse --json the way `browse` already does. Without the guard the
// library-scope check fires first and hands back a Fix that is verbatim
// runnable — running it launches a TUI with no terminal, and bubbletea's
// "error opening TTY" carries no remediation at all. An agent that follows
// the Fix therefore dead-ends. Refusing up front costs one error and names
// the scriptable verb instead.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
)

func TestInteractiveCommands_RejectJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		argv    []string
		wantTry string // the scriptable equivalent the Try must name
	}{
		{name: "view", argv: []string{"view"}, wantTry: "sci zot item list --json"},
		{name: "tags browse", argv: []string{"tags", "browse"}, wantTry: "sci zot tags list --json"},
		{name: "collection browse", argv: []string{"collection", "browse"}, wantTry: "sci zot collection list --json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := slices.Concat([]string{"zot", "--json"}, tc.argv)
			err := runZotTree(t, argv...)
			if err == nil {
				t.Fatalf("`%s` under --json must refuse — it is interactive-only", strings.Join(tc.argv, " "))
			}
			coded, ok := errors.AsType[*cmdutil.CodedError](err)
			if !ok {
				t.Fatalf("want CodedError, got %T: %v", err, err)
			}
			if coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want %q", coded.Code, cmdutil.CodeUsage)
			}
			// A Fix is resubmittable verbatim, and there is no command line
			// that makes an interactive browser work under --json.
			if coded.Fix != "" {
				t.Errorf("Fix must stay empty — no rewrite makes this command scriptable; got %q", coded.Fix)
			}
			if !strings.Contains(coded.Try, tc.wantTry) {
				t.Errorf("Try must name the scriptable equivalent %q, got %q", tc.wantTry, coded.Try)
			}
		})
	}
}
