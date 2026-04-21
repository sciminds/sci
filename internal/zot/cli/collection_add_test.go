package cli

// Unit tests for the pure helpers behind `zot collection add`'s bulk path.
// The full CLI Action is covered by the existing library-scope wiring tests;
// here we target:
//
//   - parseKeysFromReader: normalization, blanks, comments, de-duplication
//   - buildCollectionAddPatches: merge-or-skip logic given local.Item state

import (
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestParseKeysFromReader_StripsBlanksAndComments(t *testing.T) {
	t.Parallel()
	in := strings.NewReader(`
ABCDEF12
# a comment
BCDEFG23

  CDEFGH34
#another
`)
	got, err := parseKeysFromReader(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ABCDEF12", "BCDEFG23", "CDEFGH34"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKeysFromReader_DeduplicatesPreservingOrder(t *testing.T) {
	t.Parallel()
	in := strings.NewReader("AAA\nBBB\nAAA\nCCC\nBBB\n")
	got, err := parseKeysFromReader(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AAA", "BBB", "CCC"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKeysFromReader_EmptyInput(t *testing.T) {
	t.Parallel()
	got, err := parseKeysFromReader(strings.NewReader("\n\n  \n#only comments\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestBuildCollectionAddPatches_MergesAndSkipsMembers(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "AAA11111", Version: 10, Type: "journalArticle", Collections: []string{"OTHER01"}},
		{Key: "BBB22222", Version: 20, Type: "book", Collections: []string{"TARGET01", "OTHER01"}},
		{Key: "CCC33333", Version: 30, Type: "journalArticle"}, // no collections
	}
	patches, alreadyMember := buildCollectionAddPatches(items, "TARGET01")

	if !slices.Equal(alreadyMember, []string{"BBB22222"}) {
		t.Errorf("alreadyMember = %v, want [BBB22222]", alreadyMember)
	}

	if len(patches) != 2 {
		t.Fatalf("len(patches) = %d, want 2", len(patches))
	}

	// AAA: existing [OTHER01] → merged should be [OTHER01, TARGET01].
	// CCC: no existing → merged should be [TARGET01].
	byKey := map[string]int{}
	for i, p := range patches {
		byKey[p.Key] = i
	}
	aaa := patches[byKey["AAA11111"]]
	if aaa.Version != 10 || aaa.ItemType != "journalArticle" {
		t.Errorf("AAA Version/ItemType = %d/%q, want 10/journalArticle", aaa.Version, aaa.ItemType)
	}
	if aaa.Data.Collections == nil {
		t.Fatal("AAA Data.Collections nil")
	}
	gotA := *aaa.Data.Collections
	if !slices.Equal(gotA, []string{"OTHER01", "TARGET01"}) {
		t.Errorf("AAA merged = %v, want [OTHER01 TARGET01]", gotA)
	}

	ccc := patches[byKey["CCC33333"]]
	gotC := *ccc.Data.Collections
	if !slices.Equal(gotC, []string{"TARGET01"}) {
		t.Errorf("CCC merged = %v, want [TARGET01]", gotC)
	}
}

func TestBuildCollectionAddPatches_AllAlreadyMembers(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "AAA11111", Version: 1, Type: "journalArticle", Collections: []string{"TARGET01"}},
		{Key: "BBB22222", Version: 2, Type: "journalArticle", Collections: []string{"TARGET01", "X"}},
	}
	patches, alreadyMember := buildCollectionAddPatches(items, "TARGET01")
	if len(patches) != 0 {
		t.Errorf("patches = %v, want none", patches)
	}
	if !slices.Equal(alreadyMember, []string{"AAA11111", "BBB22222"}) {
		t.Errorf("alreadyMember = %v", alreadyMember)
	}
}
