package enrich

import (
	"context"
	"errors"
	"testing"

	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/client"
)

type fakeWriter struct {
	patches []api.ItemPatch
	results map[string]error
	err     error // whole-request error
}

func (w *fakeWriter) UpdateItemsBatch(_ context.Context, patches []api.ItemPatch) (map[string]error, error) {
	if w.err != nil {
		return nil, w.err
	}
	w.patches = append(w.patches, patches...)
	return w.results, nil
}

func TestApply_zeroTargetsIsNoOp(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{}
	res, err := Apply(context.Background(), w, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 0 || res.Failed != 0 {
		t.Errorf("res = %+v", res)
	}
	if len(w.patches) != 0 {
		t.Errorf("writer got patches on empty input: %+v", w.patches)
	}
}

func TestApply_submitsOnePatchPerTarget(t *testing.T) {
	t.Parallel()
	targets := []Target{
		{ItemKey: "AAA", Version: 1, ItemType: "journalArticle", Fills: map[string]string{"title": "T1"}},
		{ItemKey: "BBB", Version: 2, ItemType: "preprint", Fills: map[string]string{"abstract": "x"}},
	}
	w := &fakeWriter{results: map[string]error{"AAA": nil, "BBB": nil}}
	res, err := Apply(context.Background(), w, targets)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 2 || res.Failed != 0 {
		t.Errorf("res = %+v", res)
	}
	if len(w.patches) != 2 {
		t.Errorf("want 2 patches, got %d", len(w.patches))
	}
	if w.patches[0].Key != "AAA" || w.patches[0].Version != 1 || w.patches[0].ItemType != "journalArticle" {
		t.Errorf("patch[0] = %+v", w.patches[0])
	}
}

func TestApply_recordsPerItemErrors(t *testing.T) {
	t.Parallel()
	targets := []Target{
		{ItemKey: "AAA", Version: 1, ItemType: "journalArticle"},
		{ItemKey: "BBB", Version: 2, ItemType: "preprint"},
	}
	w := &fakeWriter{results: map[string]error{
		"AAA": nil,
		"BBB": errors.New("412 Precondition Failed"),
	}}
	res, err := Apply(context.Background(), w, targets)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 1 || res.Failed != 1 {
		t.Errorf("applied=%d failed=%d", res.Applied, res.Failed)
	}
	if res.Errors["BBB"] == "" {
		t.Errorf("Errors[BBB] missing, got %+v", res.Errors)
	}
	if _, ok := res.Errors["AAA"]; ok {
		t.Errorf("successful item must not appear in Errors: %+v", res.Errors)
	}
}

func TestApply_propagatesWholeRequestError(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{err: errors.New("network")}
	_, err := Apply(context.Background(), w, []Target{{ItemKey: "A"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// Enrichment's premise is "these fields are blank". A 412 means someone
// filled something in the meantime, so the payload is re-derived against
// the server's copy: fields still blank are filled, fields since populated
// are dropped rather than overwritten with OpenAlex's version.
func TestApply_RebuildDropsFieldsFilledOnTheServer(t *testing.T) {
	t.Parallel()
	title := "OpenAlex Title"
	abstract := "OpenAlex abstract"
	w := &fakeWriter{results: map[string]error{"AAA": nil}}
	targets := []Target{{
		ItemKey: "AAA", Version: 1, ItemType: "journalArticle",
		Fills: map[string]string{"title": title, "abstract": abstract},
		Data:  client.ItemData{ItemType: "journalArticle", Title: &title, AbstractNote: &abstract},
	}}
	if _, err := Apply(context.Background(), w, targets); err != nil {
		t.Fatal(err)
	}
	rebuild := w.patches[0].Rebuild
	if rebuild == nil {
		t.Fatal("enrich patch carries no Rebuild hook")
	}

	theirs := "A Title Someone Typed By Hand"
	data, err := rebuild(&client.Item{Data: client.ItemData{
		ItemType: "journalArticle",
		Title:    &theirs, // filled since the plan
		// AbstractNote still absent.
	}})
	if err != nil {
		t.Fatal(err)
	}
	if data.Title != nil {
		t.Errorf("rebuilt patch still sets Title = %q; it was filled on the server", *data.Title)
	}
	if data.AbstractNote == nil || *data.AbstractNote != abstract {
		t.Errorf("rebuilt patch dropped the still-blank abstract: %v", data.AbstractNote)
	}
}
