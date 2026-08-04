package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/savedsearch"
)

// stubSourceLoaders builds pdfSourceLoaders from canned results, recording
// which loaders ran so tests can assert on fallback order.
type stubSourceLoaders struct {
	collItems []local.Item
	collErr   error
	ssItems   []local.Item
	ssErr     error

	collCalled bool
	ssCalled   bool
}

func (s *stubSourceLoaders) loaders() pdfSourceLoaders {
	return pdfSourceLoaders{
		collection: func(name string) ([]local.Item, string, func(), error) {
			s.collCalled = true
			return s.collItems, "collection:" + name, nil, s.collErr
		},
		savedSearch: func(name string) ([]local.Item, string, error) {
			s.ssCalled = true
			return s.ssItems, "saved-search:" + name, s.ssErr
		},
	}
}

func TestLoadEitherSource_CollectionFirstWins(t *testing.T) {
	t.Parallel()
	stub := &stubSourceLoaders{collItems: []local.Item{{Key: "AAAA1111"}}}
	items, label, _, err := loadEitherSource("missing-pdf", true, stub.loaders())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || label != "collection:missing-pdf" {
		t.Errorf("items=%d label=%q", len(items), label)
	}
	if stub.ssCalled {
		t.Error("saved-search loader ran despite collection success")
	}
}

func TestLoadEitherSource_FallsBackToSavedSearch(t *testing.T) {
	t.Parallel()
	stub := &stubSourceLoaders{
		collErr: cmdutil.Coded(cmdutil.CodeNotFound, "collection %q not found", "missing-pdf"),
		ssItems: []local.Item{{Key: "AAAA1111"}},
	}
	items, label, _, err := loadEitherSource("missing-pdf", true, stub.loaders())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || label != "saved-search:missing-pdf" {
		t.Errorf("items=%d label=%q", len(items), label)
	}
}

func TestLoadEitherSource_FallsBackToCollection(t *testing.T) {
	t.Parallel()
	stub := &stubSourceLoaders{
		ssErr:     fmt.Errorf("saved search %q %w", "refs", api.ErrNotFound),
		collItems: []local.Item{{Key: "AAAA1111"}},
	}
	items, label, _, err := loadEitherSource("refs", false, stub.loaders())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || label != "collection:refs" {
		t.Errorf("items=%d label=%q", len(items), label)
	}
}

func TestLoadEitherSource_BothMissing(t *testing.T) {
	t.Parallel()
	stub := &stubSourceLoaders{
		collErr: cmdutil.Coded(cmdutil.CodeNotFound, "collection %q not found", "nope"),
		ssErr:   fmt.Errorf("saved search %q %w", "nope", api.ErrNotFound),
	}
	_, _, _, err := loadEitherSource("nope", true, stub.loaders())
	if err == nil {
		t.Fatal("expected combined not-found error")
	}
	ce, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok || ce.Code != cmdutil.CodeNotFound {
		t.Fatalf("err = %v, want CodedError with CodeNotFound", err)
	}
	if !strings.Contains(ce.Message, "collection") || !strings.Contains(ce.Message, "saved search") {
		t.Errorf("message %q should name both source kinds", ce.Message)
	}
}

func TestLoadEitherSource_RealErrorNoFallback(t *testing.T) {
	t.Parallel()
	boom := errors.New("db exploded")
	stub := &stubSourceLoaders{collErr: boom}
	_, _, _, err := loadEitherSource("missing-pdf", true, stub.loaders())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the original failure", err)
	}
	if stub.ssCalled {
		t.Error("fell back on a non-not-found error")
	}
}

func TestLoadEitherSource_KeyShapedNeverFallsBack(t *testing.T) {
	t.Parallel()
	notFound := fmt.Errorf("saved search %q %w", "ABCD1234", api.ErrNotFound)
	stub := &stubSourceLoaders{ssErr: notFound}
	_, _, _, err := loadEitherSource("ABCD1234", false, stub.loaders())
	if !errors.Is(err, api.ErrNotFound) {
		t.Errorf("err = %v, want the saved-search not-found error", err)
	}
	if stub.collCalled {
		t.Error("keys are kind-specific — a key miss must not fall back")
	}
}

func TestIsZoteroKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"ABCD1234", true},
		{"AAAAAAAA", true},
		{"00000000", true},
		{"abcd1234", false}, // lowercase
		{"ABCD123", false},  // 7 chars
		{"ABCD12345", false},
		{"missing-pdf", false},
		{"", false},
		{"ABCD-234", false}, // hyphen
	}
	for _, tc := range cases {
		got := isZoteroKey(tc.in)
		if got != tc.want {
			t.Errorf("isZoteroKey(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestReadItemKeys_FromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	body := strings.Join([]string{
		"# leading comment",
		"AAAA1111",
		"",
		"BBBB2222",
		"  CCCC3333  ", // surrounded by whitespace
		"AAAA1111",     // duplicate — should dedupe
		"# trailing comment",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readItemKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AAAA1111", "BBBB2222", "CCCC3333"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestReadItemKeys_RejectsNonKeyLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	if err := os.WriteFile(path, []byte("AAAA1111\nnot-a-key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readItemKeys(path)
	if err == nil {
		t.Fatal("want error on bad line")
	}
	if !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "not-a-key") {
		t.Errorf("error should cite line 2 and value, got %q", err)
	}
}

func TestReadItemKeys_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := readItemKeys("/no/such/file/should/exist")
	if err == nil {
		t.Fatal("want error on missing file")
	}
}

func TestItemTypeFilterFromSavedSearch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   savedsearch.APIFilters
		want string
	}{
		{"both", savedsearch.APIFilters{ItemType: "journalArticle", NotItemType: "attachment"}, "journalArticle || -attachment"},
		{"is only", savedsearch.APIFilters{ItemType: "book"}, "book"},
		{"isNot only", savedsearch.APIFilters{NotItemType: "attachment"}, "-attachment"},
		{"none", savedsearch.APIFilters{}, ""},
	}
	for _, tc := range cases {
		got := itemTypeFilterFromSavedSearch(tc.in)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFilterChildless(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "AAAA0000", NumChildren: 0},
		{Key: "BBBB0001", NumChildren: 1},
		{Key: "CCCC0002", NumChildren: 0},
		{Key: "DDDD0003", NumChildren: 5},
	}
	got := filterChildless(items)
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2 (childless only)", len(got))
	}
	if got[0].Key != "AAAA0000" || got[1].Key != "CCCC0002" {
		t.Errorf("filtered keys = %q, %q; want AAAA0000, CCCC0002", got[0].Key, got[1].Key)
	}
}

func TestTagFilterFromSavedSearch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   savedsearch.APIFilters
		want string
	}{
		{"missing-pdf shape", savedsearch.APIFilters{NotTag: "has-markdown"}, "-has-markdown"},
		{"both", savedsearch.APIFilters{Tag: "ml", NotTag: "draft"}, "ml || -draft"},
		{"positive only", savedsearch.APIFilters{Tag: "ml"}, "ml"},
		{"none", savedsearch.APIFilters{}, ""},
	}
	for _, tc := range cases {
		got := tagFilterFromSavedSearch(tc.in)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
