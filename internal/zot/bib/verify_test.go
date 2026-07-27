package bib

import (
	"context"
	"errors"
	"testing"
)

// fakeLookup records what it was asked and replays canned answers keyed by
// the reference's normalized value.
type fakeLookup struct {
	matches map[string]*Match
	errs    map[string]error
	asked   []string
}

func (f *fakeLookup) ResolveRef(_ context.Context, ref Ref) (*Match, error) {
	f.asked = append(f.asked, ref.Value)
	if err, ok := f.errs[ref.Value]; ok {
		return nil, err
	}
	if m, ok := f.matches[ref.Value]; ok {
		return m, nil
	}
	return nil, ErrNotFound
}

func TestVerify_AmbiguousNeedsNoLookup(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{}
	got := Verify(context.Background(), []Unresolved{{
		Ref:        Ref{Raw: "[[Carey 2009]]", Kind: KindWikilink, Value: "Carey 2009"},
		Reason:     "ambiguous (2 candidates)",
		Candidates: []string{"DDDD4444", "EEEE5555"},
	}}, lk)

	if len(got) != 1 {
		t.Fatalf("verified = %d, want 1", len(got))
	}
	if got[0].Status != StatusAmbiguous {
		t.Errorf("status = %q, want %q", got[0].Status, StatusAmbiguous)
	}
	// The library already holds the answer — asking OpenAlex would be a
	// wasted request and could invent a third "candidate".
	if len(lk.asked) != 0 {
		t.Errorf("lookup called for an ambiguous ref: %v", lk.asked)
	}
}

func TestVerify_ExternalWhenUpstreamHasIt(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{matches: map[string]*Match{
		"10.1016/j.tics.2007.05.005": {
			OpenAlexID: "W2119803923",
			DOI:        "10.1016/j.tics.2007.05.005",
			Title:      "The proactive brain: using analogies and associations to generate predictions",
			Year:       2007,
			Venue:      "Trends in Cognitive Sciences",
		},
	}}
	got := Verify(context.Background(), []Unresolved{{
		Ref:    Ref{Raw: "10.1016/j.tics.2007.05.005", Kind: KindDOI, Value: "10.1016/j.tics.2007.05.005"},
		Reason: "no match",
	}}, lk)

	if got[0].Status != StatusExternal {
		t.Fatalf("status = %q, want %q", got[0].Status, StatusExternal)
	}
	if got[0].Match == nil || got[0].Match.Year != 2007 {
		t.Fatalf("match = %+v", got[0].Match)
	}
}

// TestVerify_NotFoundIsTheHallucinationSignal pins the distinction that makes
// --verify worth running: an identifier that resolves nowhere upstream is a
// citation that probably does not exist.
func TestVerify_NotFoundIsTheHallucinationSignal(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{}
	got := Verify(context.Background(), []Unresolved{{
		Ref:    Ref{Raw: "10.1234/invented.2024.99999", Kind: KindDOI, Value: "10.1234/invented.2024.99999"},
		Reason: "no match",
	}}, lk)

	if got[0].Status != StatusNotFound {
		t.Errorf("status = %q, want %q", got[0].Status, StatusNotFound)
	}
	if got[0].Match != nil {
		t.Errorf("match = %+v, want nil", got[0].Match)
	}
}

// TestVerify_TransportErrorIsNotNotFound keeps a flaky network from being
// reported as a fabricated citation — the two have opposite meanings.
func TestVerify_TransportErrorIsNotNotFound(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{errs: map[string]error{
		"10.1000/real": errors.New("openalex: 503 service unavailable"),
	}}
	got := Verify(context.Background(), []Unresolved{{
		Ref:    Ref{Raw: "10.1000/real", Kind: KindDOI, Value: "10.1000/real"},
		Reason: "no match",
	}}, lk)

	if got[0].Status != StatusError {
		t.Errorf("status = %q, want %q", got[0].Status, StatusError)
	}
	if got[0].Error == "" {
		t.Error("error message not surfaced")
	}
}

// TestVerify_UncheckableKindsAreNotAccused — a citekey or wikilink carries no
// identifier OpenAlex can resolve. Saying "not found" would accuse a
// perfectly good reference; "unchecked" is the honest answer.
func TestVerify_UncheckableKindsAreNotAccused(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{}
	unres := []Unresolved{
		{Ref: Ref{Raw: "@smith2030-madeup-ZZZZ9999", Kind: KindCitekey, Value: "smith2030-madeup-ZZZZ9999"}, Reason: "no match"},
		{Ref: Ref{Raw: "[[Some Vault Note]]", Kind: KindWikilink, Value: "Some Vault Note"}, Reason: "no match"},
		{Ref: Ref{Raw: "https://example.org/x", Kind: KindURL, Value: "https://example.org/x"}, Reason: "no match"},
	}
	for i, v := range Verify(context.Background(), unres, lk) {
		if v.Status != StatusUnchecked {
			t.Errorf("[%d] %s: status = %q, want %q", i, v.Kind, v.Status, StatusUnchecked)
		}
	}
	if len(lk.asked) != 0 {
		t.Errorf("lookup called for uncheckable kinds: %v", lk.asked)
	}
}

func TestVerify_ArxivIsChecked(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{matches: map[string]*Match{
		"1706.03762": {OpenAlexID: "W2963403868", Title: "Attention Is All You Need", Year: 2017},
	}}
	got := Verify(context.Background(), []Unresolved{{
		Ref:    Ref{Raw: "arXiv:1706.03762", Kind: KindArxiv, Value: "1706.03762"},
		Reason: "no match",
	}}, lk)
	if got[0].Status != StatusExternal {
		t.Errorf("status = %q, want %q", got[0].Status, StatusExternal)
	}
}

// TestVerify_RetractionSurvivesToTheResult — citing a retracted paper is the
// other failure --verify can catch for free, since OpenAlex carries the flag.
func TestVerify_RetractionSurvivesToTheResult(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{matches: map[string]*Match{
		"10.1016/bad": {OpenAlexID: "W1", Title: "Withdrawn Result", Retracted: true},
	}}
	got := Verify(context.Background(), []Unresolved{{
		Ref:    Ref{Raw: "10.1016/bad", Kind: KindDOI, Value: "10.1016/bad"},
		Reason: "no match",
	}}, lk)
	if !got[0].Match.Retracted {
		t.Error("retraction flag lost")
	}
}

func TestVerify_PreservesInputOrder(t *testing.T) {
	t.Parallel()
	lk := &fakeLookup{}
	unres := []Unresolved{
		{Ref: Ref{Kind: KindDOI, Value: "10.1/a"}, Reason: "no match"},
		{Ref: Ref{Kind: KindCitekey, Value: "b"}, Reason: "no match"},
		{Ref: Ref{Kind: KindDOI, Value: "10.1/c"}, Reason: "no match"},
	}
	got := Verify(context.Background(), unres, lk)
	for i, want := range []string{"10.1/a", "b", "10.1/c"} {
		if got[i].Value != want {
			t.Errorf("[%d] value = %q, want %q", i, got[i].Value, want)
		}
	}
}

func TestFixCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    Verified
		want string
	}{
		{
			name: "external with DOI is a resubmittable add",
			v: Verified{
				Unresolved: Unresolved{Ref: Ref{Kind: KindDOI, Value: "10.1016/j.tics.2007.05.005"}},
				Status:     StatusExternal,
				Match:      &Match{OpenAlexID: "W2119803923", DOI: "10.1016/j.tics.2007.05.005"},
			},
			want: "sci zot --library personal item add --openalex 10.1016/j.tics.2007.05.005",
		},
		{
			name: "external without a DOI falls back to the OpenAlex id",
			v: Verified{
				Unresolved: Unresolved{Ref: Ref{Kind: KindArxiv, Value: "1706.03762"}},
				Status:     StatusExternal,
				Match:      &Match{OpenAlexID: "W2963403868"},
			},
			want: "sci zot --library personal item add --openalex W2963403868",
		},
		{
			// One resubmittable command that shows both candidates side by
			// side — `item read` takes a single key, so an OR-group search is
			// the only shape that stays verbatim-runnable.
			name: "ambiguous fix shows the competing keys side by side",
			v: Verified{
				Unresolved: Unresolved{
					Ref:        Ref{Kind: KindWikilink, Value: "Carey 2009"},
					Candidates: []string{"DDDD4444", "EEEE5555"},
				},
				Status: StatusAmbiguous,
			},
			want: `sci zot --library personal search "@key:DDDD4444 | @key:EEEE5555"`,
		},
		{
			name: "ambiguous with no recorded candidates has no fix",
			v:    Verified{Status: StatusAmbiguous},
			want: "",
		},
		{
			name: "not-found has no fix — there is nothing to resubmit",
			v:    Verified{Status: StatusNotFound},
			want: "",
		},
		{
			name: "unchecked has no fix",
			v:    Verified{Status: StatusUnchecked},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FixCommand(tt.v, "personal"); got != tt.want {
				t.Errorf("FixCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- ChainLookup -----------------------------------------------------------
//
// The first smoke test against a real manuscript accused two genuine papers
// (Carey's OSO monograph, an unindexed arXiv preprint) of being fabricated,
// because OpenAlex is a citation index with coverage gaps, not a registry.
// These pin the fallback that keeps a coverage gap from reading as fraud.

func TestChainLookup_FirstHitWins(t *testing.T) {
	t.Parallel()
	first := &fakeLookup{matches: map[string]*Match{"10.1/a": {Title: "from first"}}}
	second := &fakeLookup{matches: map[string]*Match{"10.1/a": {Title: "from second"}}}
	got, err := ChainLookup{first, second}.ResolveRef(context.Background(), Ref{Kind: KindDOI, Value: "10.1/a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "from first" {
		t.Errorf("title = %q", got.Title)
	}
	if len(second.asked) != 0 {
		t.Error("second lookup consulted after the first hit")
	}
}

func TestChainLookup_FallsThroughNotFound(t *testing.T) {
	t.Parallel()
	// OpenAlex doesn't index the work; the DOI registry does.
	index := &fakeLookup{}
	registry := &fakeLookup{matches: map[string]*Match{
		"10.1093/acprof:oso/9780195367638.001.0001": {Title: "The Origin of Concepts", Year: 2009},
	}}
	got, err := ChainLookup{index, registry}.ResolveRef(context.Background(),
		Ref{Kind: KindDOI, Value: "10.1093/acprof:oso/9780195367638.001.0001"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Origin of Concepts" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestChainLookup_NotFoundOnlyWhenEveryLinkAgrees(t *testing.T) {
	t.Parallel()
	_, err := ChainLookup{&fakeLookup{}, &fakeLookup{}}.ResolveRef(context.Background(),
		Ref{Kind: KindDOI, Value: "10.1234/invented"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestChainLookup_RealErrorBeatsNotFound — if one link 404s and another
// simply failed, the reference's standing is unknown. Reporting not-found
// would turn a network blip into an accusation.
func TestChainLookup_RealErrorBeatsNotFound(t *testing.T) {
	t.Parallel()
	index := &fakeLookup{} // 404s
	registry := &fakeLookup{errs: map[string]error{"10.1/x": errors.New("dial tcp: timeout")}}
	_, err := ChainLookup{index, registry}.ResolveRef(context.Background(), Ref{Kind: KindDOI, Value: "10.1/x"})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want a real error", err)
	}
}

func TestChainLookup_EmptyIsNotFound(t *testing.T) {
	t.Parallel()
	_, err := ChainLookup{}.ResolveRef(context.Background(), Ref{Kind: KindDOI, Value: "10.1/x"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
