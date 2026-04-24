package cli

// Tests for the persistent --library flag + context plumbing. Covers:
//   - PersistentFlags() []cli.Flag
//   - ValidateLibraryBefore                (Before hook for the zot root)
//   - LibraryFromContext(ctx) (zot.LibraryRef, bool)
// cmd/sci/zot.go wires these into the `sci zot` subcommand; the tests
// construct a minimal equivalent root so they can exercise the hook without
// pulling in the full sci wrapper.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/urfave/cli/v3"
)

// buildTestRoot constructs a minimal zot root command that mirrors the
// wiring in cmd/sci/zot.go, so we can exercise the persistent --library
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

// TestRoot_MissingLibrary_PropagatesToLeaf — the Before hook deliberately
// does NOT reject a missing --library (that would shadow help for
// sub-namespaces; see ValidateLibraryBefore's doc comment). Missing scope
// surfaces at the leaf via localSelectorFor / resolveLibraryRef, with an
// error that mentions --library so the user still sees the right hint.
func TestRoot_MissingLibrary_PropagatesToLeaf(t *testing.T) {
	// Probe leaf that mimics what every library-requiring leaf does:
	// pull the ref from ctx; absence is the error condition.
	root := &cli.Command{
		Name:   "zot",
		Flags:  PersistentFlags(),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{
				Name: "probe",
				Action: func(ctx context.Context, _ *cli.Command) error {
					if _, ok := LibraryFromContext(ctx); !ok {
						return errors.New("--library is required (values: personal, shared)")
					}
					return errReachedAction
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"zot", "probe"})
	if err == nil {
		t.Fatal("expected error when --library is missing at the leaf")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "library") {
		t.Errorf("err=%v, want mention of --library", err)
	}
}

// TestRoot_MissingLibrary_NamespaceShowsHelp — the whole reason we moved
// required-ness to the leaf: `sci zot item` with no trailing args should
// dump help, not error with "--library is required". Exercises the full
// Before chain (WireNamespaceDefaults + ValidateLibraryBefore) on pure
// namespaces under zot. Commands with their own Action (doctor, tools,
// etc.) run their Action instead of showing help and are out of scope
// for this test.
func TestRoot_MissingLibrary_NamespaceShowsHelp(t *testing.T) {
	// Mirror cmd/sci's wiring: WireNamespaceDefaults chains
	// RejectUnknownSubcommand into every namespace's Before, matching
	// what buildRoot does for the real binary.
	root := buildTestRoot(t)
	cmdutil.WireNamespaceDefaults(root)
	var buf strings.Builder
	root.Writer = &buf

	for _, path := range [][]string{
		{"zot", "item"},
		{"zot", "collection"},
		{"zot", "tags"},
		{"zot", "notes"},
		{"zot", "llm"},
		{"zot", "saved-search"},
	} {
		buf.Reset()
		err := root.Run(context.Background(), path)
		if err != nil {
			t.Errorf("%v: expected help (nil error), got: %v", path, err)
		}
	}
}

// `find` hits OpenAlex, not Zotero, so --library is meaningless. It simply
// never reads LibraryFromContext, so the Before hook's no-op behavior on
// missing --library lets the command run through.
func TestRoot_FindDoesNotRequireLibrary(t *testing.T) {
	root := &cli.Command{
		Name:   "zot",
		Flags:  PersistentFlags(),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{
				Name: "find",
				Commands: []*cli.Command{
					{
						Name: "works",
						Action: func(ctx context.Context, _ *cli.Command) error {
							return errReachedAction
						},
					},
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"zot", "find", "works", "query"})
	if !errors.Is(err, errReachedAction) {
		t.Fatalf("find should bypass --library, got err=%v", err)
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
// both libraries at once, so it must not require --library. Since the
// Before hook no longer enforces required-ness (see ValidateLibraryBefore's
// doc comment) and setup doesn't read LibraryFromContext, this just confirms
// the path stays open.
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

// Unknown-subcommand handling is now a tree-wide invariant enforced by
// cmdutil.WireNamespaceDefaults on the sci root; the dedicated test for
// that behavior lives in cmd/sci/commands_test.go (TestNamespaceRejects
// UnknownChildren) and covers every namespace in the tree, including
// zot. No per-package regression test needed here.
