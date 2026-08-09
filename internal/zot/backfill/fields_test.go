package backfill_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/backfill"
	"github.com/sciminds/cli/internal/zot/client"
)

// A field plan is the second kind of row `zot` emits. Where a DOI plan
// writes one identifier and its provenance, a field plan fills whatever the
// item is missing that its matched OpenAlex work supplies -- abstract,
// volume, issue, pages, PMID.
//
// The premise is per FIELD, not per item, and that is the whole difference
// from the DOI path. A DOI plan's premise ("this item has no DOI") is one
// fact and an item either still has none or it does. A field plan carries
// five, and four of them can still hold when the fifth stops.

func TestAFieldPlanFillsOnlyWhatIsStillBlank(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{
		"AAA11111": {Data: client.ItemData{
			Key: str("AAA11111"), ItemType: "journalArticle",
			// The volume arrived from somewhere else since the plan was
			// built. The rest of the item is still empty.
			Volume: str("57"),
		}},
	}}
	p := write(t, `{"library":"personal","item_key":"AAA11111","item_type":"journalArticle",`+
		`"work_id":"W1","basis":"doi/publisher",`+
		`"fields":{"volume":"99","issue":"2","pages":"243-259"},"why":"w"}`)
	plans, err := backfill.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := backfill.Apply(context.Background(), f, f, plans)
	if err != nil {
		t.Fatal(err)
	}

	got := f.got["AAA11111"]
	if got.Volume != nil {
		t.Errorf("volume = %q, want it left alone -- the server's value is not ours to replace", *got.Volume)
	}
	if got.Issue == nil || *got.Issue != "2" {
		t.Errorf("issue = %v, want 2", got.Issue)
	}
	if got.Pages == nil || *got.Pages != "243-259" {
		t.Errorf("pages = %v, want 243-259", got.Pages)
	}
	if res.Applied != 1 {
		t.Errorf("applied = %d, want 1", res.Applied)
	}
	// One field of three had lost its premise. Reporting that is what makes
	// a plan of 4,840 fills and a result of 4,833 explainable.
	if res.FieldsWritten != 2 || res.FieldsSkipped != 1 {
		t.Errorf("fields written/skipped = %d/%d, want 2/1", res.FieldsWritten, res.FieldsSkipped)
	}
}

func TestAnItemWithNothingLeftToFillIsSkippedNotPatched(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{
		"BBB22222": {Data: client.ItemData{
			Key: str("BBB22222"), ItemType: "journalArticle",
			Issue: str("2"), Pages: str("1-9"),
		}},
	}}
	p := write(t, `{"library":"personal","item_key":"BBB22222","item_type":"journalArticle",`+
		`"work_id":"W2","basis":"doi/publisher","fields":{"issue":"2","pages":"1-9"},"why":"w"}`)
	plans, _ := backfill.Read(p)
	res, err := backfill.Apply(context.Background(), f, f, plans)
	if err != nil {
		t.Fatal(err)
	}
	// An empty PATCH is not a no-op, it is a write that reports success and
	// changes nothing -- the exact bug that once POSTed 709 empty payloads
	// and called it "applied 709 of 709".
	if _, wrote := f.got["BBB22222"]; wrote {
		t.Error("an item with nothing to fill was still patched")
	}
	if res.Skipped != 1 || res.Applied != 0 {
		t.Errorf("skipped/applied = %d/%d, want 1/0", res.Skipped, res.Applied)
	}
}

// TestAFieldOutsideTheGeneratedStructIsStillWritten.
//
// PMID is a real Zotero field -- 100 items in the live library carry one --
// and it is 3,054 of the 4,840 planned fills. The generated client does not
// model it: the OpenAPI spec sci generates from mentions PMID only in a
// comment about the Extra field. The generated type carries an
// AdditionalProperties escape hatch for exactly this, and its MarshalJSON
// inlines it, so the field goes on the wire under its own name rather than
// being smuggled into Extra as a line.
func TestAFieldOutsideTheGeneratedStructIsStillWritten(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{
		"CCC33333": {Data: client.ItemData{Key: str("CCC33333"), ItemType: "journalArticle"}},
	}}
	p := write(t, `{"library":"personal","item_key":"CCC33333","item_type":"journalArticle",`+
		`"work_id":"W3","basis":"doi/publisher","fields":{"PMID":"38382521"},"why":"w"}`)
	plans, _ := backfill.Read(p)
	if _, err := backfill.Apply(context.Background(), f, f, plans); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(f.got["CCC33333"])
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["PMID"] != "38382521" {
		t.Errorf("PATCH body = %s, want PMID on the wire", raw)
	}
	// And it must not have been folded into Extra, where zot's loader reads
	// provenance lines and where nothing would parse it as a field.
	if _, leaked := body["extra"]; leaked {
		t.Errorf("PMID leaked into extra: %s", raw)
	}
}

// TestRebuildRechecksEveryFieldAgainstTheFreshCopy.
//
// A 412 means the item moved between the plan and the write. For a
// derivation the answer is to re-derive, not to restamp the version: the
// fields still blank on the server get written and the ones that filled in
// the meantime drop out. Refusing the whole patch (what the DOI path does)
// would be wrong here, because one field losing its premise says nothing
// about the other four.
func TestRebuildRechecksEveryFieldAgainstTheFreshCopy(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{
		"DDD44444": {Data: client.ItemData{Key: str("DDD44444"), ItemType: "journalArticle"}},
	}}
	p := write(t, `{"library":"personal","item_key":"DDD44444","item_type":"journalArticle",`+
		`"work_id":"W4","basis":"doi/publisher","fields":{"issue":"2","pages":"1-9"},"why":"w"}`)
	plans, _ := backfill.Read(p)

	var rebuild func(*client.Item) (client.ItemData, error)
	f.capture = func(key string, hook func(*client.Item) (client.ItemData, error)) {
		if key == "DDD44444" {
			rebuild = hook
		}
	}
	if _, err := backfill.Apply(context.Background(), f, f, plans); err != nil {
		t.Fatal(err)
	}
	if rebuild == nil {
		t.Fatal("no Rebuild hook was supplied — lint-guard rule 16 exists for this")
	}

	fresh := &client.Item{Data: client.ItemData{
		Key: str("DDD44444"), ItemType: "journalArticle",
		Issue: str("2"), // someone filled it between the plan and the write
	}}
	out, err := rebuild(fresh)
	if err != nil {
		t.Fatalf("rebuild refused a patch that still had work to do: %v", err)
	}
	if out.Issue != nil {
		t.Error("rebuild would have overwritten the issue that appeared")
	}
	if out.Pages == nil || *out.Pages != "1-9" {
		t.Errorf("rebuild dropped a field whose premise still held: %v", out.Pages)
	}
}

func TestARowMustCarryExactlyOneOfDOIAndFields(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"neither": `{"library":"personal","item_key":"EEE55555","why":"w"}`,
		"both":    `{"library":"personal","item_key":"EEE55555","doi":"10.1/x","doi_source":"zot/x","fields":{"issue":"2"},"why":"w"}`,
		"empty":   `{"library":"personal","item_key":"EEE55555","fields":{},"why":"w"}`,
	} {
		if _, err := backfill.Read(write(t, line)); err == nil {
			t.Errorf("%s: a row that writes nothing coherent was accepted", name)
		}
	}
}

// TestAnUnwritableFieldFailsThePlanNotTheLibrary.
//
// zot filters its plan against what the corpus shows a type carries, which
// under-approximates on purpose. sci is the side that knows Zotero's
// vocabulary, and a field it cannot set must stop the plan at read time —
// before a single write — rather than surfacing as a per-item API error
// halfway through a batch of 3,258.
func TestAnUnwritableFieldFailsThePlanNotTheLibrary(t *testing.T) {
	t.Parallel()
	_, err := backfill.Read(write(t, `{"library":"personal","item_key":"FFF66666",`+
		`"item_type":"journalArticle","work_id":"W6","basis":"doi/publisher",`+
		`"fields":{"nonesuch":"x"},"why":"w"}`))
	if err == nil {
		t.Fatal("a field sci cannot write was accepted")
	}
	if !strings.Contains(err.Error(), "nonesuch") {
		t.Errorf("error does not name the offending field: %v", err)
	}
}

func TestOneFileMayCarryBothKindsOfRow(t *testing.T) {
	t.Parallel()
	f := &fakeServer{server: map[string]client.Item{
		"AAA11111": {Data: client.ItemData{Key: str("AAA11111"), ItemType: "journalArticle"}},
		"BBB22222": {Data: client.ItemData{Key: str("BBB22222"), ItemType: "journalArticle"}},
	}}
	p := write(t,
		`{"library":"personal","item_key":"AAA11111","doi":"10.1/x","doi_source":"zot/x","why":"w"}`,
		`{"library":"personal","item_key":"BBB22222","item_type":"journalArticle","work_id":"W","basis":"doi/publisher","fields":{"issue":"7"},"why":"w"}`,
	)
	plans, err := backfill.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backfill.Apply(context.Background(), f, f, plans); err != nil {
		t.Fatal(err)
	}
	if got := f.got["AAA11111"]; got.DOI == nil || *got.DOI != "10.1/x" {
		t.Errorf("the DOI row did not apply: %+v", got)
	}
	if got := f.got["BBB22222"]; got.Issue == nil || *got.Issue != "7" {
		t.Errorf("the field row did not apply: %+v", got)
	}
}
