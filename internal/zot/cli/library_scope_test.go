package cli

// Tests for the persistent --library flag + context plumbing. These reference
// symbols not yet implemented:
//   - PersistentFlags() []cli.Flag        (to be added in cli/cli.go)
//   - ValidateLibraryBefore                (Before hook for the zot root)
//   - LibraryFromContext(ctx) (zot.LibraryRef, bool)
// Entry points (cmd/zot/main.go, cmd/sci/zot.go) wire these into their
// root commands; both test cases exercise the same shared helpers.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// buildTestRoot constructs a minimal zot root command that mirrors the
// wiring in cmd/zot/main.go, so we can exercise the persistent --library
// flag and its Before hook without pulling in the full sci wrapper.
func buildTestRoot(t *testing.T) *cli.Command {
	t.Helper()
	return &cli.Command{
		Name:     "zot",
		Flags:    PersistentFlags(),
		Before:   ValidateLibraryBefore,
		Commands: Commands(),
	}
}

// sentinel error returned by a test-only action to assert the Before
// hook let us through; the alternative (nil) could also mean the hook
// short-circuited without error, so we use a distinct sentinel.
var errReachedAction = errors.New("reached test action")

func TestPersistentFlags_IncludesLibrary(t *testing.T) {
	found := false
	for _, f := range PersistentFlags() {
		for _, n := range f.Names() {
			if n == "library" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("PersistentFlags() does not include --library")
	}
}

func TestRoot_RejectsMissingLibrary(t *testing.T) {
	root := buildTestRoot(t)
	// Any non-setup leaf command should require the flag.
	err := root.Run(context.Background(), []string{"zot", "item", "list"})
	if err == nil {
		t.Fatal("expected error when --library is missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "library") {
		t.Errorf("err=%v, want mention of --library", err)
	}
}

func TestRoot_RejectsInvalidLibraryValue(t *testing.T) {
	root := buildTestRoot(t)
	err := root.Run(context.Background(), []string{"zot", "--library", "bogus", "item", "list"})
	if err == nil {
		t.Fatal("expected error for invalid --library value")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "personal") || !strings.Contains(msg, "shared") {
		t.Errorf("err=%v, want valid values listed", err)
	}
}

func TestRoot_AcceptsPersonal(t *testing.T) {
	// info's action isn't trivially mockable; instead we shim a leaf
	// command whose Action is our sentinel. Do it through a fresh root
	// so we don't mutate the package-level tree.
	root := &cli.Command{
		Name:   "zot",
		Flags:  PersistentFlags(),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{
				Name: "probe",
				Action: func(ctx context.Context, _ *cli.Command) error {
					ref, ok := LibraryFromContext(ctx)
					if !ok {
						return errors.New("library not in ctx")
					}
					if string(ref.Scope) != "personal" {
						return errors.New("wrong scope: " + string(ref.Scope))
					}
					return errReachedAction
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"zot", "--library", "personal", "probe"})
	if !errors.Is(err, errReachedAction) {
		t.Fatalf("expected sentinel, got err=%v", err)
	}
}

func TestRoot_AcceptsShared(t *testing.T) {
	root := &cli.Command{
		Name:   "zot",
		Flags:  PersistentFlags(),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{
				Name: "probe",
				Action: func(ctx context.Context, _ *cli.Command) error {
					ref, ok := LibraryFromContext(ctx)
					if !ok {
						return errors.New("library not in ctx")
					}
					if string(ref.Scope) != "shared" {
						return errors.New("wrong scope: " + string(ref.Scope))
					}
					return errReachedAction
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"zot", "--library", "shared", "probe"})
	if !errors.Is(err, errReachedAction) {
		t.Fatalf("expected sentinel, got err=%v", err)
	}
}

// TestRoot_SetupDoesNotRequireLibrary — the setup command configures
// both libraries at once, so the persistent flag must be exempt.
// Implementation should either skip the Before check for setup or make
// the flag optional with a special meaning when setup is the subcommand.
func TestRoot_SetupDoesNotRequireLibrary(t *testing.T) {
	root := buildTestRoot(t)
	// We invoke `zot setup --help` to avoid the interactive prompts and
	// any real side effects; --help exits cleanly and proves the Before
	// hook didn't reject on missing --library.
	err := root.Run(context.Background(), []string{"zot", "setup", "--help"})
	if err != nil {
		t.Fatalf("setup --help should not require --library: %v", err)
	}
}
