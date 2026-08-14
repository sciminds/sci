package local

import "testing"

func TestDoclingNoteBodyByItemID(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	got, err := db.DoclingNoteBodyByItemID()
	if err != nil {
		t.Fatal(err)
	}
	// Item 10 has a docling-tagged child note (item 90).
	body, ok := got[10]
	if !ok {
		t.Fatal("expected docling note body for itemID=10")
	}
	if body != "<p>Extracted via docling.</p>" {
		t.Errorf("body = %q", body)
	}
	// Items without docling notes should be absent.
	if _, ok := got[20]; ok {
		t.Error("itemID=20 should not have a docling note")
	}
	if _, ok := got[30]; ok {
		t.Error("itemID=30 should not have a docling note")
	}
}
