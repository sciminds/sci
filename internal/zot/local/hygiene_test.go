package local

import (
	"testing"
)

func TestScanFieldPresence(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.ScanFieldPresence()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 content items (attachment + trashed excluded)", len(rows))
	}

	byKey := map[string]ItemFieldPresence{}
	for _, r := range rows {
		byKey[r.Key] = r
	}

	// Item 10 — fully populated journalArticle with PDF attachment.
	a := byKey["AAAA1111"]
	if !a.HasTitle || !a.HasDOI || !a.HasAbstract || !a.HasDate || !a.HasURL {
		t.Errorf("AAAA1111 presence wrong: %+v", a)
	}
	if a.CreatorCount != 2 {
		t.Errorf("AAAA1111 creator_count = %d, want 2", a.CreatorCount)
	}
	if a.TagCount != 2 {
		t.Errorf("AAAA1111 tag_count = %d, want 2", a.TagCount)
	}
	if a.PDFCount != 1 {
		t.Errorf("AAAA1111 pdf_count = %d, want 1", a.PDFCount)
	}

	// Item 20 — sparse: only title/date/pub, no DOI/abstract/url/tags/pdf.
	b := byKey["BBBB2222"]
	if !b.HasTitle {
		t.Errorf("BBBB2222 should have title: %+v", b)
	}
	if b.HasDOI || b.HasAbstract || b.HasURL {
		t.Errorf("BBBB2222 should have no DOI/abstract/url: %+v", b)
	}
	if !b.HasDate {
		t.Errorf("BBBB2222 should have date: %+v", b)
	}
	if b.CreatorCount != 1 {
		t.Errorf("BBBB2222 creator_count = %d, want 1 (NASA)", b.CreatorCount)
	}
	if b.TagCount != 0 || b.PDFCount != 0 {
		t.Errorf("BBBB2222 counts wrong: tags=%d pdfs=%d", b.TagCount, b.PDFCount)
	}

	// Item 30 — book with bare year and one tag.
	c := byKey["CCCC3333"]
	if !c.HasTitle {
		t.Errorf("CCCC3333 should have title: %+v", c)
	}
	if c.HasDOI || c.HasAbstract || c.HasURL || c.PDFCount != 0 {
		t.Errorf("CCCC3333 should have only date+1 tag: %+v", c)
	}
	if c.CreatorCount != 1 {
		t.Errorf("CCCC3333 creator_count = %d, want 1", c.CreatorCount)
	}
	if c.TagCount != 1 {
		t.Errorf("CCCC3333 tag_count = %d, want 1", c.TagCount)
	}
}

func TestScanFieldValues(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Ask for DOI + date — only items that have those fields set
	// should come back, one row per (item, field) pair.
	rows, err := db.ScanFieldValues([]string{"DOI", "date"})
	if err != nil {
		t.Fatal(err)
	}

	// Expected (from fixture seed):
	//   AAAA1111 DOI=10.1000/abc123, date=2024-03-15 March 15, 2024
	//   BBBB2222 date=2024-03-15 March 15, 2024
	//   CCCC3333 date=2023
	// → 4 rows total.
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(rows), rows)
	}

	type key struct{ k, f string }
	by := map[key]FieldValue{}
	for _, r := range rows {
		by[key{r.Key, r.Field}] = r
	}
	if by[key{"AAAA1111", "DOI"}].Value != "10.1000/abc123" {
		t.Errorf("AAAA1111 DOI = %q", by[key{"AAAA1111", "DOI"}].Value)
	}
	if by[key{"AAAA1111", "DOI"}].Title != "Deep Learning for Neuroimaging" {
		t.Errorf("AAAA1111 title not carried: %q", by[key{"AAAA1111", "DOI"}].Title)
	}
	if by[key{"CCCC3333", "date"}].Value != "2023" {
		t.Errorf("CCCC3333 date = %q", by[key{"CCCC3333", "date"}].Value)
	}
	// BBBB2222 has no DOI — must not appear for that field.
	if _, ok := by[key{"BBBB2222", "DOI"}]; ok {
		t.Error("BBBB2222 should not have a DOI row")
	}
}

func TestScanFieldValues_EmptyFieldsReturnsNothing(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.ScanFieldValues(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("empty fields should return nothing, got %d rows", len(rows))
	}
}

func TestScanDuplicateCandidates(t *testing.T) {
	dir := buildFixture(t)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	cands, err := db.ScanDuplicateCandidates()
	if err != nil {
		t.Fatal(err)
	}
	// 3 content items — attachment and trashed row excluded.
	if len(cands) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(cands), cands)
	}
	byKey := map[string]DuplicateCandidate{}
	for _, c := range cands {
		byKey[c.Key] = c
	}
	a := byKey["AAAA1111"]
	if a.Title != "Deep Learning for Neuroimaging" {
		t.Errorf("A title = %q", a.Title)
	}
	if a.DOI != "10.1000/abc123" {
		t.Errorf("A DOI = %q", a.DOI)
	}
	if a.PDFCount != 1 {
		t.Errorf("A pdf_count = %d, want 1", a.PDFCount)
	}
	// B has no DOI — empty string, not an error.
	b := byKey["BBBB2222"]
	if b.DOI != "" {
		t.Errorf("B DOI = %q, want empty", b.DOI)
	}
}
