package fix

import (
	"context"
	"errors"
	"testing"

	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/local"
)

// fakeWriter is a CitekeyWriter stub that records every patch it
// receives and lets each test inject per-item errors. Kept minimal —
// we're testing the orchestrator's plumbing, not the HTTP batcher
// (which has its own tests in internal/zot/api/items_test.go).
type fakeWriter struct {
	received []api.ItemPatch
	perItem  map[string]error // optional: item key → error to return
	wholeErr error            // optional: return this for the whole request
}

func (f *fakeWriter) UpdateItemsBatch(_ context.Context, patches []api.ItemPatch) (map[string]error, error) {
	f.received = append(f.received, patches...)
	if f.wholeErr != nil {
		return nil, f.wholeErr
	}
	out := make(map[string]error, len(patches))
	for _, p := range patches {
		out[p.Key] = f.perItem[p.Key] // nil by default = success
	}
	return out, nil
}

// sampleLib builds a hand-rolled library where every citekey
// classification bucket is represented by at least one item. Shared
// across planner tests so the expectations are easy to cross-reference.
func sampleLib() []local.Item {
	mk := func(key, title, date, last, stored string) local.Item {
		it := local.Item{
			Key:   key,
			Title: title,
			Date:  date,
			Creators: []local.Creator{
				{Type: "author", Last: last, OrderIdx: 0},
			},
			Fields: map[string]string{},
		}
		if stored != "" {
			it.Fields["citationKey"] = stored
		}
		return it
	}
	return []local.Item{
		// CANONICAL — passes v2 spec, not a collision.
		mk("AAAA1111", "Deep Learning", "2020", "Smith", "smith2020-deeplear-AAAA1111"),
		// NON-CANONICAL — BBT camelCase.
		mk("BBBB2222", "Some Paper", "2021", "Jones", "jonesSomePaper2021"),
		// INVALID — contains whitespace.
		mk("CCCC3333", "Broken", "2019", "Brown", "brown 2019"),
		// COLLISION pair — two items share the same (canonical!) key.
		mk("DDDD4444", "First Copy", "2022", "Adams", "adams2022-dup-ZZZZ9999"),
		mk("EEEE5555", "Second Copy", "2022", "Adams", "adams2022-dup-ZZZZ9999"),
		// UNSTORED — no citationKey field, no extra BBT line.
		mk("FFFF6666", "Unkeyed Thing", "2018", "Zed", ""),
	}
}

func TestPlanCitekeys_DefaultKindsCoverAllBuckets(t *testing.T) {
	t.Parallel()
	targets := PlanCitekeys(sampleLib(), CitekeyOptions{})

	byItem := map[string]CitekeyTarget{}
	for _, tg := range targets {
		byItem[tg.ItemKey] = tg
	}
	// AAAA1111 is canonical and uncontested — must NOT appear.
	if _, ok := byItem["AAAA1111"]; ok {
		t.Errorf("canonical item AAAA1111 should not be a target")
	}
	// Every other item should be targeted.
	wantReasons := map[string]string{
		"BBBB2222": "non-canonical",
		"CCCC3333": "invalid",
		"DDDD4444": "collision",
		"EEEE5555": "collision",
		"FFFF6666": "unstored",
	}
	for key, want := range wantReasons {
		tg, ok := byItem[key]
		if !ok {
			t.Errorf("missing target for %s", key)
			continue
		}
		if tg.Reason != want {
			t.Errorf("%s reason = %q, want %q", key, tg.Reason, want)
		}
	}
	if len(targets) != len(wantReasons) {
		t.Errorf("targets count = %d, want %d", len(targets), len(wantReasons))
	}
}

func TestPlanCitekeys_SynthesizesCanonicalNewKeys(t *testing.T) {
	t.Parallel()
	// Every target's NewKey must itself pass the v2 spec — otherwise the
	// fix would write back something that the read-only check would
	// immediately re-flag. Also verifies that the Zotero key suffix is
	// the actual item's key (not some shared prefix from a collision).
	targets := PlanCitekeys(sampleLib(), CitekeyOptions{})
	for _, tg := range targets {
		// Each NewKey should end in "-<ItemKey>" since synthesized v2
		// keys always append the 8-char ZOTKEY. Check the suffix cheaply.
		suffix := "-" + tg.ItemKey
		if len(tg.NewKey) < len(suffix) || tg.NewKey[len(tg.NewKey)-len(suffix):] != suffix {
			t.Errorf("%s NewKey %q does not end with %q", tg.ItemKey, tg.NewKey, suffix)
		}
	}
}

func TestPlanCitekeys_KindMaskNarrowsOutput(t *testing.T) {
	t.Parallel()
	// CitekeyInvalid | CitekeyCollision should skip non-canonical and unstored
	// rows entirely — the "safe subset" we'd pick if we wanted to fix
	// real errors without overwriting BBT keys by accident.
	targets := PlanCitekeys(sampleLib(), CitekeyOptions{Kinds: CitekeyInvalid | CitekeyCollision})
	gotReasons := map[string]int{}
	for _, tg := range targets {
		gotReasons[tg.Reason]++
	}
	if gotReasons["invalid"] != 1 {
		t.Errorf("invalid = %d, want 1", gotReasons["invalid"])
	}
	if gotReasons["collision"] != 2 {
		t.Errorf("collision = %d, want 2", gotReasons["collision"])
	}
	if gotReasons["non-canonical"] != 0 {
		t.Errorf("non-canonical should be filtered out, got %d", gotReasons["non-canonical"])
	}
	if gotReasons["unstored"] != 0 {
		t.Errorf("unstored should be filtered out, got %d", gotReasons["unstored"])
	}
}

func TestPlanCitekeys_ItemAllowListRestrictsScope(t *testing.T) {
	t.Parallel()
	// --item ABCD1234 semantics: only the listed keys are eligible.
	// Other items — even broken ones — are left alone so the user can
	// smoke-test a single fix before a full run.
	targets := PlanCitekeys(sampleLib(), CitekeyOptions{ItemKeys: []string{"CCCC3333"}})
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1 (only CCCC3333)", len(targets))
	}
	if targets[0].ItemKey != "CCCC3333" {
		t.Errorf("targeted %q, want CCCC3333", targets[0].ItemKey)
	}
	if targets[0].Reason != "invalid" {
		t.Errorf("reason = %q, want invalid", targets[0].Reason)
	}
}

func TestPlanCitekeys_InvalidBeatsCollision(t *testing.T) {
	t.Parallel()
	// If an item's stored key is both structurally invalid AND shared
	// with another item, the priority is invalid > collision — matching
	// the finding layer's ordering so doctor + fix tell the same story.
	items := []local.Item{
		{
			Key:      "AAAA1111",
			Title:    "A",
			Date:     "2020",
			Creators: []local.Creator{{Type: "author", Last: "A", OrderIdx: 0}},
			Fields:   map[string]string{"citationKey": "has space"},
		},
		{
			Key:      "BBBB2222",
			Title:    "B",
			Date:     "2020",
			Creators: []local.Creator{{Type: "author", Last: "B", OrderIdx: 0}},
			Fields:   map[string]string{"citationKey": "has space"},
		},
	}
	targets := PlanCitekeys(items, CitekeyOptions{})
	for _, tg := range targets {
		if tg.Reason != "invalid" {
			t.Errorf("%s reason = %q, want invalid", tg.ItemKey, tg.Reason)
		}
	}
}

func TestPlanCitekeys_ResolvesBBTFromExtra(t *testing.T) {
	t.Parallel()
	// An item with no native citationKey but a legacy BBT "Citation Key:"
	// line in `extra` is classified the same as if the line were native —
	// matches citekey.Resolve precedence so export and fix agree.
	items := []local.Item{{
		Key:      "AAAA1111",
		Title:    "Legacy",
		Date:     "2019",
		Creators: []local.Creator{{Type: "author", Last: "Old", OrderIdx: 0}},
		Fields: map[string]string{
			"extra": "tldr: summary\nCitation Key: oldCamelKey2019\n",
		},
	}}
	targets := PlanCitekeys(items, CitekeyOptions{})
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	if targets[0].Reason != "non-canonical" {
		t.Errorf("reason = %q, want non-canonical (BBT camelCase from extra)", targets[0].Reason)
	}
	if targets[0].OldKey != "oldCamelKey2019" {
		t.Errorf("OldKey = %q, want oldCamelKey2019", targets[0].OldKey)
	}
}

func TestApplyCitekeys_WritesEveryTargetCitationKey(t *testing.T) {
	t.Parallel()
	targets := []CitekeyTarget{
		{ItemKey: "AAAA1111", OldKey: "oldA", NewKey: "smith2020-aaa-AAAA1111", Reason: "non-canonical"},
		{ItemKey: "BBBB2222", OldKey: "", NewKey: "jones2019-bbb-BBBB2222", Reason: "unstored"},
	}
	w := &fakeWriter{}
	res, err := ApplyCitekeys(context.Background(), w, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Applied {
		t.Error("result should be marked Applied=true")
	}
	if len(w.received) != 2 {
		t.Fatalf("writer got %d patches, want 2", len(w.received))
	}
	// Every patch must set CitationKey to the target's NewKey and leave
	// every other field zero — the orchestrator is a single-field
	// overwrite only.
	for _, p := range w.received {
		if p.Data.CitationKey == nil || *p.Data.CitationKey == "" {
			t.Errorf("%s: CitationKey not set", p.Key)
		}
		var want string
		for _, tg := range targets {
			if tg.ItemKey == p.Key {
				want = tg.NewKey
			}
		}
		if got := *p.Data.CitationKey; got != want {
			t.Errorf("%s CitationKey = %q, want %q", p.Key, got, want)
		}
	}
	if res.Totals.Succeeded != 2 || res.Totals.Failed != 0 {
		t.Errorf("totals = %+v, want Succeeded=2 Failed=0", res.Totals)
	}
}

func TestApplyCitekeys_RecordsPerItemFailures(t *testing.T) {
	t.Parallel()
	// A per-item error from UpdateItemsBatch (e.g. 412 retry exhausted,
	// validation rejection) must land on the matching CitekeyOutcome and
	// bump Failed without tipping the whole result into an error — the
	// rest of the batch should still succeed.
	targets := []CitekeyTarget{
		{ItemKey: "AAAA1111", NewKey: "smith2020-aaa-AAAA1111", Reason: "invalid"},
		{ItemKey: "BBBB2222", NewKey: "jones2019-bbb-BBBB2222", Reason: "invalid"},
	}
	w := &fakeWriter{perItem: map[string]error{
		"BBBB2222": errors.New("412 conflict"),
	}}
	res, err := ApplyCitekeys(context.Background(), w, targets)
	if err != nil {
		t.Fatalf("batch err should surface as per-item, got %v", err)
	}
	if res.Totals.Succeeded != 1 || res.Totals.Failed != 1 {
		t.Errorf("totals = %+v, want Succeeded=1 Failed=1", res.Totals)
	}
	var failed CitekeyOutcome
	for _, oc := range res.Outcomes {
		if oc.ItemKey == "BBBB2222" {
			failed = oc
		}
	}
	if failed.Applied {
		t.Error("BBBB2222 should have Applied=false")
	}
	if failed.Error == "" {
		t.Error("BBBB2222 should carry an error message")
	}
}

func TestApplyCitekeys_WholeRequestErrorSurfaces(t *testing.T) {
	t.Parallel()
	// Network/HTTP failures (distinct from per-item 412s) must surface
	// as a top-level error so the CLI can abort the run rather than
	// pretend partial progress.
	targets := []CitekeyTarget{
		{ItemKey: "AAAA1111", NewKey: "smith2020-aaa-AAAA1111", Reason: "invalid"},
	}
	w := &fakeWriter{wholeErr: errors.New("connection refused")}
	if _, err := ApplyCitekeys(context.Background(), w, targets); err == nil {
		t.Error("expected whole-request error to propagate")
	}
}

func TestApplyCitekeys_EmptyTargetListIsNoop(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{}
	res, err := ApplyCitekeys(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Applied {
		t.Error("empty-apply should still report Applied=true (nothing-to-do is success)")
	}
	if len(w.received) != 0 {
		t.Errorf("writer should not be called for empty targets, got %d patches", len(w.received))
	}
}

func TestDryRunCitekeys_FlagsNotApplied(t *testing.T) {
	t.Parallel()
	// Dry-run path must never touch a writer and must clearly mark
	// itself as unapplied so CLI renderers can pick the right header.
	targets := []CitekeyTarget{
		{ItemKey: "AAAA1111", NewKey: "smith2020-aaa-AAAA1111", Reason: "invalid"},
		{ItemKey: "BBBB2222", NewKey: "jones2019-bbb-BBBB2222", Reason: "non-canonical"},
	}
	res := DryRunCitekeys(targets)
	if res.Applied {
		t.Error("dry-run result should have Applied=false")
	}
	if len(res.Outcomes) != 0 {
		t.Errorf("dry-run should carry no outcomes, got %d", len(res.Outcomes))
	}
	if res.Totals.PerReason["invalid"] != 1 || res.Totals.PerReason["non-canonical"] != 1 {
		t.Errorf("per-reason totals = %+v", res.Totals.PerReason)
	}
}

func TestPlanCitekeys_OrderingIsDeterministic(t *testing.T) {
	t.Parallel()
	// Planner output is sorted by (reason-rank, item-key) so dry-run
	// output is stable across runs and test goldens don't flake.
	targets := PlanCitekeys(sampleLib(), CitekeyOptions{})
	for i := 1; i < len(targets); i++ {
		prev, cur := targets[i-1], targets[i]
		if fixReasonRank(prev.Reason) > fixReasonRank(cur.Reason) {
			t.Errorf("reason rank out of order at %d: %s before %s",
				i, prev.Reason, cur.Reason)
		}
		if prev.Reason == cur.Reason && prev.ItemKey > cur.ItemKey {
			t.Errorf("item keys unsorted inside reason %q: %s before %s",
				prev.Reason, prev.ItemKey, cur.ItemKey)
		}
	}
}
