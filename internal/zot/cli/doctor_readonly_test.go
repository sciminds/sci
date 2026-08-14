package cli

import (
	"slices"
	"strings"
	"testing"
)

// TestDoctorWriteFlagsAreGone pins the boundary: doctor reports and stops.
// Every write arm is absent rather than deprecated — and an absent flag has
// to FAIL, not be quietly ignored, or a script that still passes it looks
// like it worked.
func TestDoctorWriteFlagsAreGone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		argv []string
		flag string
	}{
		{name: "dois --fix", argv: []string{"doctor", "dois", "--fix"}, flag: "fix"},
		{name: "dois --apply", argv: []string{"doctor", "dois", "--apply"}, flag: "apply"},
		{name: "dois --yes", argv: []string{"doctor", "dois", "--yes"}, flag: "yes"},
		{name: "missing --enrich", argv: []string{"doctor", "missing", "--enrich"}, flag: "enrich"},
		{name: "missing --apply", argv: []string{"doctor", "missing", "--apply"}, flag: "apply"},
		{name: "missing --yes", argv: []string{"doctor", "missing", "--yes"}, flag: "yes"},
		{name: "citekeys --fix", argv: []string{"doctor", "citekeys", "--fix"}, flag: "fix"},
		{name: "citekeys --apply", argv: []string{"doctor", "citekeys", "--apply"}, flag: "apply"},
		{name: "citekeys --kind", argv: []string{"doctor", "citekeys", "--kind", "invalid"}, flag: "kind"},
		{name: "citekeys --item", argv: []string{"doctor", "citekeys", "--item", "AAAA1111"}, flag: "item"},
		{name: "citekeys --yes", argv: []string{"doctor", "citekeys", "--yes"}, flag: "yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			leaf := walkToLeaf(Commands(), tc.argv)
			if leaf == nil {
				t.Fatalf("could not locate leaf for %v", tc.argv)
			}
			for _, f := range leaf.Flags {
				if slices.Contains(f.Names(), tc.flag) {
					t.Fatalf("--%s is still declared on `%s`", tc.flag, strings.Join(tc.argv[:2], " "))
				}
			}

			argv := slices.Concat([]string{"zot", "--library", "personal"}, tc.argv)
			err := runZotTree(t, argv...)
			if err == nil {
				t.Fatalf("`%s` should fail on the removed flag, got nil", strings.Join(argv, " "))
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Errorf("error should name the unknown flag %q, got %q", tc.flag, err)
			}
		})
	}
}

// TestDoctorReadOnlyFlagsSurvive is the other half: removing the write arms
// must not cost the reports their own knobs.
func TestDoctorReadOnlyFlagsSurvive(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		argv  []string
		flags []string
	}{
		{argv: []string{"doctor", "dois"}, flags: []string{"limit"}},
		{argv: []string{"doctor", "missing"}, flags: []string{"limit", "field"}},
		{argv: []string{"doctor", "citekeys"}, flags: []string{"limit"}},
	} {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			t.Parallel()
			leaf := walkToLeaf(Commands(), tc.argv)
			if leaf == nil {
				t.Fatalf("could not locate leaf for %v", tc.argv)
			}
			for _, want := range tc.flags {
				found := false
				for _, f := range leaf.Flags {
					if slices.Contains(f.Names(), want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("--%s went missing from `%s`", want, strings.Join(tc.argv, " "))
				}
			}
			if leaf.Action == nil {
				t.Errorf("`%s` lost its Action — the report must still run", strings.Join(tc.argv, " "))
			}
		})
	}
}

// TestDoctorHelpAdvertisesNoWrites keeps the prose honest. A description
// that still shows `--fix --apply` teaches a command line that now errors,
// which is worse than no help at all.
func TestDoctorHelpAdvertisesNoWrites(t *testing.T) {
	t.Parallel()
	banned := []string{"--fix", "--apply", "--enrich", "--yes"}
	for _, argv := range [][]string{
		{"doctor"},
		{"doctor", "dois"},
		{"doctor", "missing"},
		{"doctor", "citekeys"},
		{"doctor", "invalid"},
		{"doctor", "orphans"},
		{"doctor", "duplicates"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			t.Parallel()
			leaf := walkToLeaf(Commands(), argv)
			if leaf == nil {
				t.Fatalf("could not locate leaf for %v", argv)
			}
			text := leaf.Usage + "\n" + leaf.Description
			for _, b := range banned {
				if strings.Contains(text, b) {
					t.Errorf("`%s` help still advertises %s", strings.Join(argv, " "), b)
				}
			}
		})
	}
}

// `doctor pdfs` is not registered at all any more. It was a stub naming
// `zot doctor pdfs` while the refusal stubs existed; the 2026-08-14
// standalone-local cut removed every stub, so it is now an unknown
// subcommand like any typo. shrink_test.go's TestRetiredVerbsAreUnknownCommands
// is where that is pinned, alongside every other retired verb.
