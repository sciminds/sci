package link

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sciminds/sci/internal/zot/bib"
	"github.com/sciminds/sci/pkg/local"
)

func match(kind bib.RefKind, value, key, title string) bib.RefMatch {
	return bib.RefMatch{
		Ref:  bib.Ref{Raw: value, Kind: kind, Value: value},
		Item: local.Item{Key: key, Title: title},
	}
}

func TestPlanSuggest_ProposesEachReferencedItem(t *testing.T) {
	t.Parallel()
	got := PlanSuggest("NOTE0001", []bib.RefMatch{
		match(bib.KindZoteroKey, "PAPER001", "PAPER001", "Prediction error"),
		match(bib.KindDOI, "10.1/xyz", "PAPER002", "Successor representations"),
	}, nil, local.ItemRelationSet{})

	want := []Suggestion{
		{Key: "PAPER001", Title: "Prediction error", Via: []bib.RefKind{bib.KindZoteroKey}, Status: StatusProposed},
		{Key: "PAPER002", Title: "Successor representations", Via: []bib.RefKind{bib.KindDOI}, Status: StatusProposed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlanSuggest =\n  %+v\nwant\n  %+v", got, want)
	}
}

// The relation already exists → reported, not rewritten. This is what makes
// a second run of `link suggest` read as "nothing to do" rather than
// silently redoing ten writes.
func TestPlanSuggest_SubtractsExistingRelations(t *testing.T) {
	t.Parallel()
	got := PlanSuggest("NOTE0001", []bib.RefMatch{
		match(bib.KindZoteroKey, "PAPER001", "PAPER001", "Prediction error"),
		match(bib.KindZoteroKey, "PAPER002", "PAPER002", "Successor representations"),
	}, nil, local.ItemRelationSet{Related: []string{"PAPER001"}})

	if len(got) != 2 {
		t.Fatalf("PlanSuggest = %+v, want 2 suggestions", got)
	}
	// Proposed sorts ahead of already-linked.
	if got[0].Key != "PAPER002" || got[0].Status != StatusProposed {
		t.Errorf("first = %+v, want PAPER002 proposed", got[0])
	}
	if got[1].Key != "PAPER001" || got[1].Status != StatusAlreadyLinked {
		t.Errorf("second = %+v, want PAPER001 already-linked", got[1])
	}
}

// Zotero rejects a self-relation, and a note citing itself is a reference
// to the reader, not a link.
func TestPlanSuggest_DropsTheNotesOwnKey(t *testing.T) {
	t.Parallel()
	got := PlanSuggest("NOTE0001", []bib.RefMatch{
		match(bib.KindZoteroKey, "NOTE0001", "NOTE0001", "The note itself"),
		match(bib.KindZoteroKey, "PAPER001", "PAPER001", "Prediction error"),
	}, nil, local.ItemRelationSet{})

	if len(got) != 1 || got[0].Key != "PAPER001" {
		t.Errorf("PlanSuggest = %+v, want just PAPER001", got)
	}
}

// One paper cited three ways is one link — with every way it was cited
// recorded, in first-appearance order and without repeats.
func TestPlanSuggest_DedupesByKeyMergingVia(t *testing.T) {
	t.Parallel()
	got := PlanSuggest("NOTE0001", []bib.RefMatch{
		match(bib.KindZoteroKey, "PAPER001", "PAPER001", "Prediction error"),
		match(bib.KindDOI, "10.1/xyz", "PAPER001", "Prediction error"),
		match(bib.KindZoteroKey, "PAPER001", "PAPER001", "Prediction error"),
		match(bib.KindCitekey, "smith2024", "PAPER001", "Prediction error"),
	}, nil, local.ItemRelationSet{})

	if len(got) != 1 {
		t.Fatalf("PlanSuggest = %+v, want a single suggestion", got)
	}
	want := []bib.RefKind{bib.KindZoteroKey, bib.KindDOI, bib.KindCitekey}
	if !reflect.DeepEqual(got[0].Via, want) {
		t.Errorf("Via = %v, want %v", got[0].Via, want)
	}
}

func TestPlanSuggest_UnresolvedRefsSurface(t *testing.T) {
	t.Parallel()
	got := PlanSuggest("NOTE0001",
		[]bib.RefMatch{match(bib.KindZoteroKey, "PAPER001", "PAPER001", "Prediction error")},
		[]bib.Unresolved{
			{Ref: bib.Ref{Raw: "zotero://select/library/items/ZZZZ9999", Kind: bib.KindZoteroKey, Value: "ZZZZ9999"}, Reason: "no match"},
			{Ref: bib.Ref{Raw: "@smith2020", Kind: bib.KindCitekey, Value: "smith2020"}, Reason: "ambiguous (2 candidates)", Candidates: []string{"AAAA1111", "BBBB2222"}},
		},
		local.ItemRelationSet{})

	if len(got) != 3 {
		t.Fatalf("PlanSuggest = %+v, want 3 suggestions", got)
	}
	// Unresolved trails everything actionable.
	if got[1].Status != StatusUnresolved || got[1].Ref != "zotero://select/library/items/ZZZZ9999" {
		t.Errorf("got[1] = %+v, want the dangling zotero:// ref", got[1])
	}
	if got[2].Reason != "ambiguous (2 candidates)" || len(got[2].Candidates) != 2 {
		t.Errorf("got[2] = %+v, want the ambiguity with its candidates", got[2])
	}
	// An unresolved reference has no item, so no key to link to.
	if got[1].Key != "" || got[2].Key != "" {
		t.Errorf("unresolved suggestions carry a key: %+v", got[1:])
	}
}

func TestPlanSuggest_EmptyPlan(t *testing.T) {
	t.Parallel()
	got := PlanSuggest("NOTE0001", nil, nil, local.ItemRelationSet{})
	if len(got) != 0 {
		t.Errorf("PlanSuggest = %+v, want empty", got)
	}
}

func TestDryRun_CountsWithoutWriting(t *testing.T) {
	t.Parallel()
	ss := []Suggestion{
		{Key: "PAPER001", Status: StatusProposed},
		{Key: "PAPER002", Status: StatusAlreadyLinked},
		{Ref: "@nope", Status: StatusUnresolved},
	}
	res := DryRun("NOTE0001", ss)

	if res.Applied {
		t.Error("Applied = true on a dry run")
	}
	if len(res.Outcomes) != 0 {
		t.Errorf("Outcomes = %+v, want none on a dry run", res.Outcomes)
	}
	want := Totals{Proposed: 1, AlreadyLinked: 1, Unresolved: 1}
	if res.Totals != want {
		t.Errorf("Totals = %+v, want %+v", res.Totals, want)
	}
}

// fakeWriter records the pairs it was asked to link and can fail on demand.
type fakeWriter struct {
	linked [][2]string
	failOn string
}

func (f *fakeWriter) LinkItems(_ context.Context, a, b string) error {
	if b == f.failOn {
		return errors.New("boom")
	}
	f.linked = append(f.linked, [2]string{a, b})
	return nil
}

func TestApply_WritesOnlyProposedLinks(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{}
	ss := []Suggestion{
		{Key: "PAPER001", Status: StatusProposed},
		{Key: "PAPER002", Status: StatusAlreadyLinked},
		{Ref: "@nope", Status: StatusUnresolved},
	}

	res, err := Apply(context.Background(), w, "NOTE0001", ss)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][2]string{{"NOTE0001", "PAPER001"}}; !reflect.DeepEqual(w.linked, want) {
		t.Errorf("linked = %v, want %v", w.linked, want)
	}
	if !res.Applied || res.Totals.Succeeded != 1 || res.Totals.Failed != 0 {
		t.Errorf("Result = %+v, want 1 succeeded", res)
	}
	// The plan is carried through unchanged so a dry-run and its apply diff
	// cleanly.
	if !reflect.DeepEqual(res.Suggestions, ss) {
		t.Errorf("Suggestions mutated by Apply: %+v", res.Suggestions)
	}
}

// One paper that won't link must not cost the others.
func TestApply_PerItemFailureDoesNotStopTheRun(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{failOn: "PAPER001"}
	ss := []Suggestion{
		{Key: "PAPER001", Status: StatusProposed},
		{Key: "PAPER002", Status: StatusProposed},
	}

	res, err := Apply(context.Background(), w, "NOTE0001", ss)
	if err != nil {
		t.Fatalf("Apply returned an error for a per-item failure: %v", err)
	}
	if res.Totals.Succeeded != 1 || res.Totals.Failed != 1 {
		t.Errorf("Totals = %+v, want 1 succeeded / 1 failed", res.Totals)
	}
	if res.Outcomes[0].Linked || res.Outcomes[0].Error == "" {
		t.Errorf("Outcomes[0] = %+v, want the failure recorded", res.Outcomes[0])
	}
	if !res.Outcomes[1].Linked {
		t.Errorf("Outcomes[1] = %+v, want PAPER002 linked anyway", res.Outcomes[1])
	}
}

func TestApply_NothingToDo(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{}
	res, err := Apply(context.Background(), w, "NOTE0001",
		[]Suggestion{{Key: "PAPER001", Status: StatusAlreadyLinked}})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.linked) != 0 {
		t.Errorf("linked = %v, want no writes", w.linked)
	}
	if !res.Applied || res.Totals.AlreadyLinked != 1 {
		t.Errorf("Result = %+v, want an applied result reporting 1 already-linked", res)
	}
}

func TestApply_ReportsProgress(t *testing.T) {
	t.Parallel()
	var seen [][2]int
	_, err := Apply(context.Background(), &fakeWriter{}, "NOTE0001", []Suggestion{
		{Key: "PAPER001", Status: StatusProposed},
		{Key: "PAPER002", Status: StatusProposed},
	}, ApplyOptions{OnProgress: func(done, total int) {
		seen = append(seen, [2]int{done, total})
	}})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][2]int{{1, 2}, {2, 2}}; !reflect.DeepEqual(seen, want) {
		t.Errorf("progress = %v, want %v", seen, want)
	}
}
