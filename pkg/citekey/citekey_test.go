package citekey

import (
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestEnrich_PopulatesCitekey(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:   "ABCD1234",
		Title: "Deep Learning for Neuroimaging",
		Date:  "2020-03-15",
		Creators: []local.Creator{
			{Type: "author", First: "Jane", Last: "Smith"},
		},
	}
	Enrich(it)
	if it.Citekey != "smith2020-deeplearneur-ABCD1234" {
		t.Errorf("Citekey = %q, want synthesized v2 form", it.Citekey)
	}
}

func TestEnrich_HonorsExtraCitationKey(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:    "ABCD1234",
		Title:  "Anything",
		Date:   "2020",
		Fields: map[string]string{"extra": "Citation Key: legacyPin1900\n"},
		Creators: []local.Creator{
			{Type: "author", First: "Jane", Last: "Smith"},
		},
	}
	Enrich(it)
	if it.Citekey != "legacyPin1900" {
		t.Errorf("Citekey = %q, want legacyPin1900 from Extra", it.Citekey)
	}
}

func TestEnrich_NilSafe(t *testing.T) {
	t.Parallel()
	Enrich(nil) // must not panic
}

func TestSynthesize_Basic(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:   "ABCD1234",
		Title: "Deep Learning for Neuroimaging",
		Date:  "2020-03-15",
		Creators: []local.Creator{
			{Type: "author", First: "Jane", Last: "Smith"},
		},
	}
	got := Synthesize(it)
	// v2 format: {author}{year}-{up to 3 title words × 4 chars}-{ZOTKEY}.
	// Title "Deep Learning for Neuroimaging" → deep + lear + neur (for is stopword).
	want := "smith2020-deeplearneur-ABCD1234"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_SkipsStopwords(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"The Default Mode Network": "smith2020-defamodenetw-ABCD1234",
		"On the Origin of Species": "smith2020-origspec-ABCD1234",
		"A Theory of Justice":      "smith2020-theojust-ABCD1234",
		"An Introduction to fMRI":  "smith2020-intrfmri-ABCD1234",
		"Of Mice and Men":          "smith2020-micemen-ABCD1234",
		"To Be or Not to Be":       "smith2020-benotbe-ABCD1234",
	}
	for title, want := range cases {
		it := &local.Item{
			Key:      "ABCD1234",
			Title:    title,
			Date:     "2020",
			Creators: []local.Creator{{Type: "author", Last: "Smith"}},
		}
		if got := Synthesize(it); got != want {
			t.Errorf("title=%q: got %q, want %q", title, got, want)
		}
	}
}

func TestSynthesize_AllStopwordsFallsBackToFirstWord(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "The The",
		Date:     "1980",
		Creators: []local.Creator{{Type: "author", Last: "Band"}},
	}
	// No non-stopword available — fall back to first raw word.
	want := "band1980-the-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_InstitutionalAuthor(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Mission Report",
		Date:     "1969",
		Creators: []local.Creator{{Type: "author", Name: "NASA"}},
	}
	want := "nasa1969-missrepo-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_ASCIIFold(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Études sur l'hystérie",
		Date:     "1895",
		Creators: []local.Creator{{Type: "author", Last: "Freud"}},
	}
	// ASCII fold: É → e, é → e. Apostrophe stripped. "sur" is not in the
	// english stopword list so it survives; "l'hystérie" folds to lhysterie
	// then truncates to lhys.
	want := "freud1895-etudsurlhys-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_NoAuthor(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:   "ABCD1234",
		Title: "Anonymous Pamphlet",
		Date:  "1776",
	}
	want := "anon1776-anonpamp-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_NoYear(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Undated Manuscript",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	// No year — author has no digits fused to it, then words, then ZOTKEY.
	want := "smith-undamanu-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_NoTitle(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	want := "smith2020-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_NoTitleNoYear(t *testing.T) {
	t.Parallel()
	// Degenerate case: with neither year nor title, the result collapses
	// to {author}-{ZOTKEY} — the Zotero key suffix remains the uniqueness
	// anchor and the v2 regex still matches.
	it := &local.Item{
		Key:      "ABCD1234",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	want := "smith-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_UsesFirstAuthor(t *testing.T) {
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
	want := "alpha2020-multauthpape-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSynthesize_EditorsCountAsCreators(t *testing.T) {
	t.Parallel()
	// When there are no authors, fall back to editors.
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Edited Volume",
		Date:     "2020",
		Creators: []local.Creator{{Type: "editor", Last: "Doe"}},
	}
	want := "doe2020-editvolu-ABCD1234"
	if got := Synthesize(it); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolve_HonorsPinnedNative(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Whatever",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
		Fields:   map[string]string{"citationKey": "mySpecialKey2020"},
	}
	key, synth := Resolve(it)
	if synth {
		t.Error("expected synthesized=false for pinned key")
	}
	if key != "mySpecialKey2020" {
		t.Errorf("key = %q, want mySpecialKey2020", key)
	}
}

func TestResolve_HonorsBBTExtra(t *testing.T) {
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
	key, synth := Resolve(it)
	if synth {
		t.Error("expected synthesized=false for BBT-extra key")
	}
	if key != "legacyKey2020" {
		t.Errorf("key = %q, want legacyKey2020", key)
	}
}

func TestResolve_SynthesizesWhenAbsent(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "ABCD1234",
		Title:    "Deep Learning",
		Date:     "2020",
		Creators: []local.Creator{{Type: "author", Last: "Smith"}},
	}
	key, synth := Resolve(it)
	if !synth {
		t.Error("expected synthesized=true")
	}
	if key != "smith2020-deeplear-ABCD1234" {
		t.Errorf("key = %q", key)
	}
}

func TestValidate_CanonicalShapes(t *testing.T) {
	t.Parallel()
	// Every canonical shape our synthesizer can produce must pass the
	// validator. If this table drifts from Synthesize, one of the two has
	// a bug.
	good := []string{
		"smith2020-deeplearneur-ABCD1234",
		"smith2020-deep-ABCD1234",
		"smith2020-ABCD1234",  // no words
		"smith-deep-ABCD1234", // no year
		"smith-ABCD1234",      // no year, no words
		"anon1776-anonpamp-ABCD1234",
		"nasa1969-missrepo-ABCD1234",
		"freud1895-etudsurlhys-ABCD1234",
	}
	for _, k := range good {
		st, reason := Validate(k)
		if st != Valid {
			t.Errorf("%q: status = %v (%q), want Valid", k, st, reason)
		}
	}
}

func TestValidate_RejectsIllegalChars(t *testing.T) {
	t.Parallel()
	// Every BibTeX metacharacter that breaks an entry header or value
	// grammar must be classified as Invalid, not just non-canonical.
	// Whitespace is separate from the bad-rune set but must also reject.
	bad := []string{
		"",                     // empty
		"smith 2020-ABCD1234",  // space
		"smith\t2020-ABCD1234", // tab
		"smith{2020-ABCD1234",  // brace
		"smith}2020-ABCD1234",
		"smith,2020-ABCD1234",
		"smith=2020-ABCD1234",
		"smith%2020-ABCD1234",
		"smith#2020-ABCD1234",
		"smith~2020-ABCD1234",
		`smith\2020-ABCD1234`,
		`smith"2020-ABCD1234`,
	}
	for _, k := range bad {
		st, reason := Validate(k)
		if st != Invalid {
			t.Errorf("%q: status = %v (%q), want Invalid", k, st, reason)
		}
	}
}

func TestValidate_FlagsNonCanonical(t *testing.T) {
	t.Parallel()
	// BibTeX-legal keys that don't match our spec are NonCanonical — a
	// soft warning. Covers BBT-style camelCase keys, hand-rolled keys,
	// and drifted v1 keys where the word segment wasn't truncated.
	soft := []string{
		"jollyResponseLynchMeasuring2021",         // BBT camelCase (no ZOTKEY suffix)
		"mySpecialKey2020",                        // hand-rolled
		"smith2020deep-ABCD1234",                  // v1 format (no hyphen before words)
		"smith2020-deeplearneuroimaging-ABCD1234", // words segment > 12 chars
		"Smith2020-deep-ABCD1234",                 // capitalized author
	}
	for _, k := range soft {
		st, _ := Validate(k)
		if st != NonCanonical {
			t.Errorf("%q: status = %v, want NonCanonical", k, st)
		}
	}
}

func TestExtractNote_StripsStructuredLines(t *testing.T) {
	t.Parallel()
	// After we consume Citation Key, the remaining extra content should
	// still be available for the BibTeX `note` field.
	extra := "Citation Key: foo\nThis is a user's note.\nAnd a second line."
	got := ExtractNote(extra)
	want := "This is a user's note.\nAnd a second line."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractNote_Empty(t *testing.T) {
	t.Parallel()
	if got := ExtractNote(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := ExtractNote("Citation Key: foo\n"); got != "" {
		t.Errorf("got %q, want empty after stripping", got)
	}
}
