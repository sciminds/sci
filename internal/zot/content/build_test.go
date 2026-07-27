package content

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestCandidateChoosePrefersDocling(t *testing.T) {
	tests := []struct {
		name        string
		cand        Candidate
		wantSource  Source
		wantVersion int64
		wantOK      bool
	}{
		{
			name:        "docling wins over zotero",
			cand:        Candidate{ItemKey: "A", DoclingNoteID: 10, DoclingVersion: 3, AttachmentKey: "ATT1", ZoteroVersion: 99},
			wantSource:  SourceDocling,
			wantVersion: 3,
			wantOK:      true,
		},
		{
			name:        "zotero when no extraction",
			cand:        Candidate{ItemKey: "A", AttachmentKey: "ATT1", ZoteroVersion: 99},
			wantSource:  SourceZotero,
			wantVersion: 99,
			wantOK:      true,
		},
		{
			name:        "docling when no attachment",
			cand:        Candidate{ItemKey: "A", DoclingNoteID: 10, DoclingVersion: 3},
			wantSource:  SourceDocling,
			wantVersion: 3,
			wantOK:      true,
		},
		{
			name:   "neither source available",
			cand:   Candidate{ItemKey: "A"},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, ver, ok := tt.cand.Choose()
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if src != tt.wantSource || ver != tt.wantVersion {
				t.Errorf("got (%q, %d), want (%q, %d)", src, ver, tt.wantSource, tt.wantVersion)
			}
		})
	}
}

func TestNewPlanClassifies(t *testing.T) {
	cands := []Candidate{
		{ItemKey: "NEWITEM1", DoclingNoteID: 1, DoclingVersion: 5},
		{ItemKey: "SAMEVER1", DoclingNoteID: 2, DoclingVersion: 7},
		{ItemKey: "DRIFTED1", DoclingNoteID: 3, DoclingVersion: 9},
		{ItemKey: "NOSOURCE" /* neither */},
	}
	indexed := map[string]DocState{
		"SAMEVER1": {Source: SourceDocling, Version: 7},
		"DRIFTED1": {Source: SourceDocling, Version: 4},
		"NOSOURCE": {Source: SourceZotero, Version: 1},  // text disappeared
		"GONEITEM": {Source: SourceDocling, Version: 1}, // item left the library
	}

	p := NewPlan(cands, indexed)

	if keys := candKeys(p.Add); !slices.Equal(keys, []string{"NEWITEM1"}) {
		t.Errorf("Add = %v, want [NEWITEM1]", keys)
	}
	if keys := candKeys(p.Update); !slices.Equal(keys, []string{"DRIFTED1"}) {
		t.Errorf("Update = %v, want [DRIFTED1]", keys)
	}
	slices.Sort(p.Delete)
	if !slices.Equal(p.Delete, []string{"GONEITEM", "NOSOURCE"}) {
		t.Errorf("Delete = %v, want [GONEITEM NOSOURCE]", p.Delete)
	}
	if p.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", p.Unchanged)
	}
}

// The case plain version comparison gets wrong: an item indexed from
// Zotero's cache gains a docling extraction whose version number happens
// to be lower. It must still be reindexed, or we keep serving the worse
// text forever.
func TestNewPlanCatchesSourceUpgradeAtLowerVersion(t *testing.T) {
	cands := []Candidate{
		{ItemKey: "UPGRADE1", DoclingNoteID: 1, DoclingVersion: 2, AttachmentKey: "ATT1", ZoteroVersion: 500},
	}
	indexed := map[string]DocState{
		"UPGRADE1": {Source: SourceZotero, Version: 500},
	}

	p := NewPlan(cands, indexed)
	if keys := candKeys(p.Update); !slices.Equal(keys, []string{"UPGRADE1"}) {
		t.Fatalf("Update = %v, want [UPGRADE1] — a source upgrade must reindex", keys)
	}
}

func TestPlanTotalCountsWork(t *testing.T) {
	p := Plan{
		Add:    []Candidate{{ItemKey: "A"}, {ItemKey: "B"}},
		Update: []Candidate{{ItemKey: "C"}},
		Delete: []string{"D"},
	}
	if p.Total() != 4 {
		t.Errorf("Total = %d, want 4", p.Total())
	}
	if p.Empty() {
		t.Error("Empty = true, want false")
	}
	if !(Plan{Unchanged: 9}).Empty() {
		t.Error("a plan with only unchanged items should be Empty")
	}
}

func TestBuildIndexesAddsAndUpdates(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "DRIFTED1", Source: SourceDocling, Version: 1, Body: "stale text"})

	p := Plan{
		Add:    []Candidate{{ItemKey: "NEWITEM1", DoclingNoteID: 1, DoclingVersion: 5}},
		Update: []Candidate{{ItemKey: "DRIFTED1", DoclingNoteID: 2, DoclingVersion: 9}},
	}
	load := func(c Candidate, _ Source) (string, error) {
		return "fresh body for " + c.ItemKey, nil
	}

	res, err := Build(ix, p, load, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Added != 1 || res.Updated != 1 {
		t.Errorf("Added/Updated = %d/%d, want 1/1", res.Added, res.Updated)
	}

	hits, err := ix.Search(Query{Text: "fresh", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits, want 2", len(hits))
	}
	if hits, _ := ix.Search(Query{Text: "stale", Limit: 10}); len(hits) != 0 {
		t.Errorf("stale text still indexed: %+v", hits)
	}
}

func TestBuildAppliesDeletes(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "GONEITEM", Source: SourceDocling, Version: 1, Body: "orphan text"})

	res, err := Build(ix, Plan{Delete: []string{"GONEITEM"}}, failLoad(t), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", res.Deleted)
	}
	if hits, _ := ix.Search(Query{Text: "orphan", Limit: 10}); len(hits) != 0 {
		t.Errorf("deleted item still matches: %+v", hits)
	}
}

// One unreadable .zotero-ft-cache must not abandon a 5,000-item build.
func TestBuildRecordsPerItemFailuresAndContinues(t *testing.T) {
	ix := openTestIndex(t)
	p := Plan{Add: []Candidate{
		{ItemKey: "GOODITM1", DoclingNoteID: 1, DoclingVersion: 1},
		{ItemKey: "BADITEM1", AttachmentKey: "ATT1", ZoteroVersion: 1},
		{ItemKey: "GOODITM2", DoclingNoteID: 2, DoclingVersion: 1},
	}}
	load := func(c Candidate, _ Source) (string, error) {
		if c.ItemKey == "BADITEM1" {
			return "", errors.New("cache file missing")
		}
		return "body of " + c.ItemKey, nil
	}

	res, err := Build(ix, p, load, Options{})
	if err != nil {
		t.Fatalf("Build returned a hard error, want per-item failure: %v", err)
	}
	if res.Added != 2 {
		t.Errorf("Added = %d, want 2", res.Added)
	}
	if got := res.Failed["BADITEM1"]; got == "" {
		t.Error("Failed[BADITEM1] is empty, want the loader error")
	}
	if len(res.Failed) != 1 {
		t.Errorf("Failed has %d entries, want 1: %v", len(res.Failed), res.Failed)
	}
}

// An item whose PDF yielded no extractable text (scanned images, a
// broken cache file) should be skipped, not indexed as an empty
// document that can never match anything.
func TestBuildSkipsBlankBodies(t *testing.T) {
	ix := openTestIndex(t)
	p := Plan{Add: []Candidate{
		{ItemKey: "BLANKITM", AttachmentKey: "ATT1", ZoteroVersion: 1},
		{ItemKey: "REALITEM", DoclingNoteID: 1, DoclingVersion: 1},
	}}
	load := func(c Candidate, _ Source) (string, error) {
		if c.ItemKey == "BLANKITM" {
			return "   \n\t ", nil
		}
		return "real content", nil
	}

	res, err := Build(ix, p, load, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Added != 1 {
		t.Errorf("Added = %d, want 1", res.Added)
	}
	// It does not count as coverage and has no readable text…
	stats, err := ix.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 {
		t.Errorf("Stats.Total = %d, want 1 — a blank row is not coverage", stats.Total)
	}
	if _, _, ok, _ := ix.Body("BLANKITM"); ok {
		t.Error("Body reported text for a blank item")
	}
	// …but its version is on record, so the next plan leaves it alone
	// instead of re-planning an item that can never be indexed.
	st, _ := ix.State()
	if _, ok := st["BLANKITM"]; !ok {
		t.Error("blank item was not recorded; it would be re-planned forever")
	}
}

// An item whose text goes blank on reindex — a failed extraction, or a
// note that was nothing but the provenance header — must lose its row,
// not keep serving what it used to say.
func TestBuildPurgesItemsWhoseTextWentBlank(t *testing.T) {
	ix := openTestIndex(t)
	mustUpsert(t, ix, Doc{ItemKey: "WENTBLNK", Source: SourceDocling, Version: 1,
		Body: "text that is about to disappear"})

	p := Plan{Update: []Candidate{{ItemKey: "WENTBLNK", DoclingNoteID: 1, DoclingVersion: 2}}}
	res, err := Build(ix, p, func(Candidate, Source) (string, error) { return "  \n ", nil }, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	hits, _ := ix.Search(Query{Text: "disappear", Limit: 10})
	if len(hits) != 0 {
		t.Errorf("stale text still matches: %+v", hits)
	}
	if _, _, ok, _ := ix.Body("WENTBLNK"); ok {
		t.Error("Body still returns text the item no longer has")
	}
	st, _ := ix.State()
	if got := st["WENTBLNK"].Version; got != 2 {
		t.Errorf("recorded version = %d, want 2 (the version that came back blank)", got)
	}
}

func TestBuildCountsBySourceAndReportsProgress(t *testing.T) {
	ix := openTestIndex(t)
	p := Plan{Add: []Candidate{
		{ItemKey: "DOCLING1", DoclingNoteID: 1, DoclingVersion: 1},
		{ItemKey: "DOCLING2", DoclingNoteID: 2, DoclingVersion: 1},
		{ItemKey: "ZOTERO01", AttachmentKey: "ATT1", ZoteroVersion: 1},
	}}
	load := func(c Candidate, src Source) (string, error) {
		return fmt.Sprintf("%s text from %s", c.ItemKey, src), nil
	}

	var progress []int
	res, err := Build(ix, p, load, Options{
		BatchSize: 2,
		Progress:  func(done, total int) { progress = append(progress, done) },
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.BySource[SourceDocling] != 2 || res.BySource[SourceZotero] != 1 {
		t.Errorf("BySource = %v, want 2 docling / 1 zotero", res.BySource)
	}
	if len(progress) == 0 {
		t.Error("Progress was never called")
	}
	if last := progress[len(progress)-1]; last != 3 {
		t.Errorf("final progress = %d, want 3", last)
	}
}

func TestBuildEmptyPlan(t *testing.T) {
	ix := openTestIndex(t)
	res, err := Build(ix, Plan{}, failLoad(t), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Added+res.Updated+res.Deleted != 0 {
		t.Errorf("empty plan did work: %+v", res)
	}
}

func candKeys(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ItemKey
	}
	return out
}

// failLoad returns a loader that fails the test if it is ever called.
func failLoad(t *testing.T) LoadFunc {
	t.Helper()
	return func(c Candidate, _ Source) (string, error) {
		t.Errorf("loader called unexpectedly for %s", c.ItemKey)
		return "", nil
	}
}
