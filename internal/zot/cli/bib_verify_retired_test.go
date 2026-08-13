package cli

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// TestBibVerifyIsGone pins the second half of the read-only boundary: `bib`
// answers from the local library and stops. --verify reached OpenAlex and
// doi.org to classify what the library lacks, which is upstream lookup
// wearing a bibliography's clothes — it retired to the zot side, and the
// unresolved list is bib's whole answer now.
//
// The flag has to FAIL rather than be ignored: a manuscript check that
// silently skips verification reads as "nothing to worry about", which is
// the one wrong answer this surface must never give.
func TestBibVerifyIsGone(t *testing.T) {
	t.Parallel()

	leaf := walkToLeaf(Commands(), []string{"bib"})
	if leaf == nil {
		t.Fatal("bib command went missing")
	}
	for _, f := range leaf.Flags {
		if slices.Contains(f.Names(), "verify") {
			t.Error("--verify is still declared on `bib`")
		}
	}

	err := doctorRoot().Run(context.Background(),
		[]string{"zot", "--library", "personal", "bib", "draft.md", "--verify"})
	if err == nil {
		t.Fatal("`bib --verify` should fail on the removed flag, got nil")
	}
	if !strings.Contains(err.Error(), "verify") {
		t.Errorf("error should name the unknown flag, got %q", err)
	}
}

// TestBibHelpAdvertisesNoVerify keeps the prose from teaching a command line
// that now errors.
func TestBibHelpAdvertisesNoVerify(t *testing.T) {
	t.Parallel()
	leaf := walkToLeaf(Commands(), []string{"bib"})
	if leaf == nil {
		t.Fatal("bib command went missing")
	}
	if text := leaf.Usage + "\n" + leaf.Description; strings.Contains(text, "--verify") {
		t.Error("`bib` help still advertises --verify")
	}
	for _, sec := range guideContent().Sections {
		for _, e := range sec.Entries {
			if strings.Contains(e.Cmd+e.Note, "--verify") {
				t.Errorf("guide entry %q still advertises --verify", e.Goal)
			}
		}
	}
}
