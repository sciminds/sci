package fix

import (
	"context"
	"errors"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

// sampleDOILib returns a hand-rolled set of items covering every DOI
// shape the planner needs to handle: clean (skip), Frontiers /abstract
// (fix), PLOS .tNNN (fix), PNAS /-/DCSupplemental (fix), unrelated 404
// (skip — not a known subobject).
func sampleDOILib() []local.Item {
	return []local.Item{
		{Key: "OK1", Type: "journalArticle", Version: 10, Title: "Clean", DOI: "10.1038/nature12373"},
		{Key: "F1", Type: "journalArticle", Version: 11, Title: "Frontiers", DOI: "10.3389/fnhum.2013.00015/abstract"},
		{Key: "P1", Type: "journalArticle", Version: 12, Title: "PLOS table", DOI: "10.1371/journal.pcbi.1000808.t001"},
		{Key: "N1", Type: "journalArticle", Version: 13, Title: "PNAS supp", DOI: "10.1073/pnas.0908104107/-/DCSupplemental"},
		{Key: "X1", Type: "journalArticle", Version: 14, Title: "Unrelated", DOI: "10.1234/missing"},
		{Key: "NODOI", Type: "journalArticle", Version: 15, Title: "no doi"},
	}
}

func TestPlanDOIs_FiltersToFixableSubobjects(t *testing.T) {
	t.Parallel()
	targets := PlanDOIs(sampleDOILib())
	wantNew := map[string]string{
		"F1": "10.3389/fnhum.2013.00015",
		"P1": "10.1371/journal.pcbi.1000808",
		"N1": "10.1073/pnas.0908104107",
	}
	if len(targets) != len(wantNew) {
		t.Fatalf("targets = %d, want %d: %+v", len(targets), len(wantNew), targets)
	}
	byKey := map[string]DOITarget{}
	for _, tg := range targets {
		byKey[tg.ItemKey] = tg
	}
	for k, want := range wantNew {
		tg, ok := byKey[k]
		if !ok {
			t.Errorf("missing target %s", k)
			continue
		}
		if tg.NewDOI != want {
			t.Errorf("%s NewDOI = %q, want %q", k, tg.NewDOI, want)
		}
		if tg.OldDOI == tg.NewDOI {
			t.Errorf("%s old/new should differ", k)
		}
	}
	// Clean and unrelated rows must NOT appear.
	for _, k := range []string{"OK1", "X1", "NODOI"} {
		if _, ok := byKey[k]; ok {
			t.Errorf("%s should not be a target", k)
		}
	}
}

func TestPlanDOIs_CarriesVersionAndItemType(t *testing.T) {
	t.Parallel()
	items := []local.Item{{
		Key:     "F1",
		Type:    "journalArticle",
		Version: 99,
		Title:   "Frontiers",
		DOI:     "10.3389/fnhum.2013.00015/abstract",
	}}
	targets := PlanDOIs(items)
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	if targets[0].Version != 99 {
		t.Errorf("Version = %d, want 99", targets[0].Version)
	}
	if targets[0].ItemType != "journalArticle" {
		t.Errorf("ItemType = %q, want journalArticle", targets[0].ItemType)
	}
}

// fakeDOIWriter mirrors fakeWriter from citekeys_test.go but is kept
// in its own type so the tests can run in parallel without sharing state.
type fakeDOIWriter struct {
	fakeWriter
}

func TestApplyDOIs_BatchesPatchesAndSetsDOI(t *testing.T) {
	t.Parallel()
	targets := []DOITarget{
		{ItemKey: "F1", Title: "F", OldDOI: "10.3389/x/abstract", NewDOI: "10.3389/x", Version: 11, ItemType: "journalArticle"},
		{ItemKey: "P1", Title: "P", OldDOI: "10.1371/y.t001", NewDOI: "10.1371/y", Version: 12, ItemType: "journalArticle"},
	}
	w := &fakeDOIWriter{}
	res, err := ApplyDOIs(context.Background(), w, targets)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Error("Applied should be true")
	}
	if len(w.received) != 2 {
		t.Fatalf("writer got %d patches, want 2", len(w.received))
	}
	for _, p := range w.received {
		if p.Data.DOI == nil || *p.Data.DOI == "" {
			t.Errorf("%s: DOI not set", p.Key)
		}
		var want DOITarget
		for _, tg := range targets {
			if tg.ItemKey == p.Key {
				want = tg
			}
		}
		if got := *p.Data.DOI; got != want.NewDOI {
			t.Errorf("%s DOI = %q, want %q", p.Key, got, want.NewDOI)
		}
		if p.Version != want.Version {
			t.Errorf("%s Version = %d, want %d", p.Key, p.Version, want.Version)
		}
		if p.ItemType != want.ItemType {
			t.Errorf("%s ItemType = %q, want %q", p.Key, p.ItemType, want.ItemType)
		}
	}
	if res.Totals.Succeeded != 2 || res.Totals.Failed != 0 {
		t.Errorf("totals = %+v, want 2/0", res.Totals)
	}
}

func TestApplyDOIs_RecordsPerItemFailures(t *testing.T) {
	t.Parallel()
	targets := []DOITarget{
		{ItemKey: "F1", NewDOI: "10.3389/x"},
		{ItemKey: "P1", NewDOI: "10.1371/y"},
	}
	w := &fakeDOIWriter{fakeWriter: fakeWriter{perItem: map[string]error{
		"P1": errors.New("412 conflict"),
	}}}
	res, err := ApplyDOIs(context.Background(), w, targets)
	if err != nil {
		t.Fatal(err)
	}
	if res.Totals.Succeeded != 1 || res.Totals.Failed != 1 {
		t.Errorf("totals = %+v, want 1/1", res.Totals)
	}
	var failed DOIOutcome
	for _, oc := range res.Outcomes {
		if oc.ItemKey == "P1" {
			failed = oc
		}
	}
	if failed.Applied {
		t.Error("P1 should have Applied=false")
	}
	if failed.Error == "" {
		t.Error("P1 should carry an error message")
	}
}

func TestApplyDOIs_WholeRequestErrorSurfaces(t *testing.T) {
	t.Parallel()
	targets := []DOITarget{{ItemKey: "F1", NewDOI: "10.3389/x"}}
	w := &fakeDOIWriter{fakeWriter: fakeWriter{wholeErr: errors.New("connection refused")}}
	if _, err := ApplyDOIs(context.Background(), w, targets); err == nil {
		t.Error("expected whole-request error to propagate")
	}
}

func TestApplyDOIs_EmptyIsNoop(t *testing.T) {
	t.Parallel()
	w := &fakeDOIWriter{}
	res, err := ApplyDOIs(context.Background(), w, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Error("empty apply should still report Applied=true")
	}
	if len(w.received) != 0 {
		t.Errorf("writer should not be called for empty targets, got %d", len(w.received))
	}
}

func TestDryRunDOIs_FlagsNotApplied(t *testing.T) {
	t.Parallel()
	targets := []DOITarget{{ItemKey: "F1", NewDOI: "10.3389/x"}}
	res := DryRunDOIs(targets)
	if res.Applied {
		t.Error("dry-run result should have Applied=false")
	}
	if len(res.Outcomes) != 0 {
		t.Errorf("dry-run should carry no outcomes, got %d", len(res.Outcomes))
	}
}
