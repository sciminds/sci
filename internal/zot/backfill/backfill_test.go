package backfill_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/backfill"
	"github.com/sciminds/sci/internal/zot/client"
)

// fakeServer stands in for the Zotero Web API.
//
// It records ONLY p.Data, and never calls Rebuild. That is the real
// contract and the earlier version of this fake got it wrong: it called
// Rebuild on every patch, so an Apply that left Data empty and expected
// Rebuild to fill it passed the tests and then POSTed 709 empty patches
// against the live library, reporting "applied 709 of 709". A fake that
// is more helpful than the system it doubles will certify a bug.
type fakeServer struct {
	server map[string]client.Item
	got    map[string]client.ItemData
	// listed counts read round-trips, so a version that skips the read
	// cannot pass unnoticed.
	listed int
	// capture hands a patch's Rebuild hook to the test WITHOUT calling it,
	// so a test can exercise the 412 path deliberately while the fake keeps
	// behaving like the server on the happy path.
	capture func(key string, hook func(*client.Item) (client.ItemData, error))
}

func (f *fakeServer) ListItems(_ context.Context, opts api.ListItemsOptions) ([]client.Item, error) {
	f.listed++
	out := make([]client.Item, 0, len(opts.ItemKeys))
	for _, k := range opts.ItemKeys {
		if it, ok := f.server[k]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeServer) UpdateItemsBatch(_ context.Context, patches []api.ItemPatch) (map[string]error, error) {
	if f.got == nil {
		f.got = map[string]client.ItemData{}
	}
	out := map[string]error{}
	for _, p := range patches {
		if _, ok := f.server[p.Key]; !ok {
			out[p.Key] = os.ErrNotExist
			continue
		}
		// No Rebuild call: Zotero only triggers it via a 412, and a patch
		// whose Data is empty writes nothing.
		if f.capture != nil {
			f.capture(p.Key, p.Rebuild)
		}
		f.got[p.Key] = p.Data
		out[p.Key] = nil
	}
	return out, nil
}

func str(s string) *string { return &s }

func write(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plan.ndjson")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtraIsComposedFromTheServerNotThePlan(t *testing.T) {
	t.Parallel()
	// The item has notes in Extra that the plan has never seen — added on
	// another device, or by a human, after the snapshot was dumped.
	f := &fakeServer{server: map[string]client.Item{
		"AAA11111": {Data: client.ItemData{
			Key: str("AAA11111"), ItemType: "journalArticle",
			Extra: str("PMID: 12345\nRead: 2026-03-01"),
		}},
	}}
	p := write(t, `{"library":"personal","item_key":"AAA11111","doi":"10.1/x","doi_source":"zot/title_exact+crossref","why":"agree"}`)
	plans, err := backfill.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backfill.Apply(context.Background(), f, f, plans); err != nil {
		t.Fatal(err)
	}

	got := f.got["AAA11111"]
	// Zotero has no field-level write: a PATCH carrying `extra` replaces
	// the WHOLE field. Composing it from zot's mirror would silently erase
	// both of these lines, and the version guard would not catch it because
	// the plan was built from a legitimately-read version.
	if got.Extra == nil {
		t.Fatal("no extra written")
	}
	for _, want := range []string{"PMID: 12345", "Read: 2026-03-01", "DOI-source: zot/title_exact+crossref"} {
		if !strings.Contains(*got.Extra, want) {
			t.Errorf("extra = %q, missing %q", *got.Extra, want)
		}
	}
	if got.DOI == nil || *got.DOI != "10.1/x" {
		t.Errorf("doi = %v", got.DOI)
	}
}

func TestReapplyingAPlanDoesNotStackProvenanceLines(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{
		"BBB22222": {Data: client.ItemData{
			Key: str("BBB22222"), ItemType: "journalArticle",
			Extra: str("DOI-source: zot/title_year\nPMID: 999"),
		}},
	}}
	p := write(t, `{"library":"personal","item_key":"BBB22222","doi":"10.1/y","doi_source":"zot/title_exact+crossref","why":"agree"}`)
	plans, _ := backfill.Read(p)
	if _, err := backfill.Apply(context.Background(), f, f, plans); err != nil {
		t.Fatal(err)
	}
	// Re-running a plan is normal -- a partial failure, a corrected
	// adjudication -- and provenance that accumulates one line per run
	// stops being provenance and becomes a log.
	got := *f.got["BBB22222"].Extra
	if n := strings.Count(got, "DOI-source:"); n != 1 {
		t.Errorf("extra has %d DOI-source lines:\n%s", n, got)
	}
	if !strings.Contains(got, "zot/title_exact+crossref") {
		t.Errorf("the stale provenance survived: %q", got)
	}
	if !strings.Contains(got, "PMID: 999") {
		t.Errorf("rewriting the provenance line ate a neighbour: %q", got)
	}
}

func TestAnItemThatGainedADOIIsAbandonedNotOverwritten(t *testing.T) {
	t.Parallel()
	// The plan's whole premise is "this item has no DOI". If the server
	// has one now -- a human typed it, a publisher import filled it -- then
	// that DOI is better evidence than anything inferred here, and the
	// premise the plan was built on no longer holds.
	f := &fakeServer{server: map[string]client.Item{
		"CCC33333": {Data: client.ItemData{
			Key: str("CCC33333"), ItemType: "journalArticle",
			DOI: str("10.9/authoritative"),
		}},
	}}
	p := write(t, `{"library":"personal","item_key":"CCC33333","doi":"10.1/inferred","doi_source":"zot/title_exact","why":"silent"}`)
	plans, _ := backfill.Read(p)
	res, err := backfill.Apply(context.Background(), f, f, plans)
	if err != nil {
		t.Fatal(err)
	}
	if _, wrote := f.got["CCC33333"]; wrote {
		t.Error("overwrote a DOI that arrived from a better source")
	}
	if res.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", res.Skipped)
	}
	if res.Applied != 0 {
		t.Errorf("applied = %d, want 0", res.Applied)
	}
}

func TestAPlanCarryingNoDOIIsRefusedBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{}}
	p := write(t, `{"library":"personal","item_key":"DDD44444","doi":"","doi_source":"zot/manual","why":"oops"}`)
	if _, err := backfill.Read(p); err == nil {
		t.Fatal("a plan row with no DOI was accepted")
	}
	if len(f.got) != 0 {
		t.Error("something was written")
	}
}

func TestPatchesAreGroupedByTheirOwnLibrary(t *testing.T) {
	t.Parallel()
	// zot's corpus spans both libraries and one plan describes both, so
	// the row's own library is the only thing that says where a key lives.
	// Applying every row against one scope makes the other library's items
	// come back "not found" -- which reads as a broken plan rather than as
	// a misrouted write, and silently leaves half the backfill undone.
	p := write(t,
		`{"library":"personal","item_key":"AAA11111","doi":"10.1/a","doi_source":"zot/x","why":"w"}`,
		`{"library":"shared","item_key":"BBB22222","doi":"10.1/b","doi_source":"zot/x","why":"w"}`,
		`{"library":"personal","item_key":"CCC33333","doi":"10.1/c","doi_source":"zot/x","why":"w"}`,
	)
	plans, err := backfill.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	groups := backfill.ByLibrary(plans)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want personal and shared", len(groups))
	}
	if n := len(groups["personal"]); n != 2 {
		t.Errorf("personal has %d rows, want 2", n)
	}
	if n := len(groups["shared"]); n != 1 {
		t.Errorf("shared has %d rows, want 1", n)
	}
}

func TestAPlanRowWithNoLibraryIsRefused(t *testing.T) {
	t.Parallel()
	// Defaulting a missing library to "personal" would write a shared
	// item's DOI onto whatever personal key happened to collide, or --
	// more likely -- fail confusingly. Neither is better than refusing.
	p := write(t, `{"item_key":"AAA11111","doi":"10.1/a","doi_source":"zot/x","why":"w"}`)
	if _, err := backfill.Read(p); err == nil {
		t.Fatal("a plan row with no library was accepted")
	}
}

func TestAPatchActuallyCarriesTheDOIItPlanned(t *testing.T) {
	t.Parallel()
	// The regression that matters most. Apply once POSTed patches whose
	// Data was empty, trusting Rebuild to fill them -- but Rebuild fires
	// only on a 412. Zotero accepts an empty patch and returns success, so
	// the run reported "applied 709 of 709" while writing nothing at all.
	// A write path must be asserted on the bytes it sends, not on the
	// status it gets back.
	f := &fakeServer{server: map[string]client.Item{
		"AAA11111": {Data: client.ItemData{Key: str("AAA11111"), ItemType: "journalArticle"}},
	}}
	p := write(t, `{"library":"personal","item_key":"AAA11111","doi":"10.1/real","doi_source":"zot/title_exact","why":"w"}`)
	plans, _ := backfill.Read(p)
	res, err := backfill.Apply(context.Background(), f, f, plans)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := f.got["AAA11111"]
	if !ok {
		t.Fatal("nothing was sent")
	}
	if body.DOI == nil || *body.DOI != "10.1/real" {
		t.Errorf("patch carried DOI %v — an empty patch is a silent no-op", body.DOI)
	}
	if body.Extra == nil || !strings.Contains(*body.Extra, "DOI-source: zot/title_exact") {
		t.Errorf("patch carried extra %v", body.Extra)
	}
	if f.listed == 0 {
		t.Error("Apply never read the server; Extra cannot be composed without it")
	}
	if res.Applied != 1 {
		t.Errorf("applied = %d", res.Applied)
	}
}
