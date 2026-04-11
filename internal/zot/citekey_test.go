package zot

import (
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestSynthesizeCiteKey_Basic(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:   "ABCD1234",
		Title: "Deep Learning for Neuroimaging",
		Date:  "2020-03-15",
		Creators: []local.Creator{
			{Type: "author", First: "Jane", Last: "Smith"},
		},
	}
	got := synthesizeCiteKey(it)
	want := "smith2020deep-ABCD1234"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_SkipsStopwords(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"The Default Mode Network": "smith2020default-ABCD1234",
		"On the Origin of Species": "smith2020origin-ABCD1234",
		"A Theory of Justice":      "smith2020theory-ABCD1234",
		"An Introduction to fMRI":  "smith2020introduction-ABCD1234",
		"Of Mice and Men":          "smith2020mice-ABCD1234",
		"To Be or Not to Be":       "smith2020be-ABCD1234",
	}
	for title, want := range cases {
		it := &local.Item{
			Key:      "ABCD1234",
			Title:    title,
			Date:     "2020",
			Creators: []local.Creator{{Type: "author", Last: "Smith"}},
		}
		if got := synthesizeCiteKey(it); got != want {
			t.Errorf("title=%q: got %q, want %q", title, got, want)
		}
	}
}

func TestSynthesizeCiteKey_AllStopwordsFallsBackToFirstWord(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "The The",
		Date:     "1980",
		Creators: []local.Creator{{Type: "author", Last: "Band"}},
	}
	// No non-stopword available — fall back to first raw word.
	want := "band1980the-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_InstitutionalAuthor(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Mission Report",
		Date:     "1969",
		Creators: []local.Creator{{Type: "author", Name: "NASA"}},
	}
	want := "nasa1969mission-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_ASCIIFold(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Études sur l'hystérie",
		Date:     "1895",
		Creators: []local.Creator{{Type: "author", Last: "Freud"}},
	}
	// ASCII fold: É → e, é → e. Apostrophe stripped.
	want := "freud1895etudes-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_NoAuthor(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:   "ABCD1234",
		Title: "Anonymous Pamphlet",
		Date:  "1776",
	}
	want := "anon1776anonymous-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_NoYear(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Undated Manuscript",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	// No year — omit the year token.
	want := "smithundated-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_NoTitle(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	want := "smith2020-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_UsesFirstAuthor(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:   "ABCD1234",
		Title: "Multi Author Paper",
		Date:  "2020",
		Creators: []local.Creator{
			{Type: "author", Last: "Zulu", OrderIdx: 1},
			{Type: "author", Last: "Alpha", OrderIdx: 0},
		},
	}
	// First by OrderIdx, not slice order.
	want := "alpha2020multi-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesizeCiteKey_EditorsCountAsCreators(t *testing.T) {
	t.Parallel()
	// When there are no authors, fall back to editors.
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Edited Volume",
		Date:     "2020",
		Creators: []local.Creator{{Type: "editor", Last: "Doe"}},
	}
	want := "doe2020edited-ABCD1234"
	if got := synthesizeCiteKey(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCiteKey_HonorsPinnedNative(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Whatever",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
		Fields:   map[string]string{"citationKey": "mySpecialKey2020"},
	}
	key, synth := ResolveCiteKey(it)
	if synth {
		t.Error("expected synthesized=false for pinned key")
	}
	if key != "mySpecialKey2020" {
		t.Errorf("key = %q, want mySpecialKey2020", key)
	}
}

func TestResolveCiteKey_HonorsBBTExtra(t *testing.T) {
	t.Parallel()
	// Legacy BBT convention: "Citation Key: foo" inside the `extra` field.
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Whatever",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
		Fields: map[string]string{
			"extra": "tldr: short summary\nCitation Key: legacyKey2020\nsome other line",
		},
	}
	key, synth := ResolveCiteKey(it)
	if synth {
		t.Error("expected synthesized=false for BBT-extra key")
	}
	if key != "legacyKey2020" {
		t.Errorf("key = %q, want legacyKey2020", key)
	}
}

func TestResolveCiteKey_SynthesizesWhenAbsent(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Deep Learning",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	key, synth := ResolveCiteKey(it)
	if !synth {
		t.Error("expected synthesized=true")
	}
	if key != "smith2020deep-ABCD1234" {
		t.Errorf("key = %q", key)
	}
}

func TestExtractExtraNote_StripsStructuredLines(t *testing.T) {
	t.Parallel()
	// After we consume Citation Key, the remaining extra content should
	// still be available for the BibTeX `note` field.
	extra := "Citation Key: foo\nThis is a user's note.\nAnd a second line."
	got := extractExtraNote(extra)
	want := "This is a user's note.\nAnd a second line."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractExtraNote_Empty(t *testing.T) {
	t.Parallel()
	if got := extractExtraNote(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := extractExtraNote("Citation Key: foo\n"); got != "" {
		t.Errorf("got %q, want empty after stripping", got)
	}
}
