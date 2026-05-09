package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/savedsearch"
)

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
