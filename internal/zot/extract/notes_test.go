package extract

import (
	"context"
	"errors"
	"testing"
)

// fakeLister is a ChildLister stub that returns a fixed child-note list
// regardless of which parent key is passed. PlanExtract's dedupe logic
// only inspects note body contents, so per-parent routing isn't tested
// here — that's the job of the api layer's GetItemChildren wiring.
type fakeLister struct {
	children []ChildNote
	err      error
}

func (f *fakeLister) ListNoteChildren(_ context.Context, _ string) ([]ChildNote, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.children, nil
}

// sentinelNote builds a ChildNote whose body contains the sci-extract
// sentinel for the given (pdfKey, hash) pair. Lets the test drive the
// dedupe logic without re-rendering a full note through MarkdownToNoteHTML.
func sentinelNote(noteKey, pdfKey, hash string) ChildNote {
	return ChildNote{
		Key:  noteKey,
		Body: "<h1>Prev</h1>\n<p>old body</p>\n<!-- sci-extract:" + pdfKey + ":" + hash + " -->\n<hr>\n<p>...</p>",
	}
}

func baseReq() PlanRequest {
	return PlanRequest{
		ParentKey: "PARENT1",
		PDFKey:    "PDF1",
		PDFName:   "paper.pdf",
		PDFHash:   "abc123",
	}
}

func TestPlanExtract_NoExistingNote_Create(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{children: nil}
	p, err := PlanExtract(context.Background(), lister, baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate", p.Action)
	}
	if p.ExistingNote != "" {
		t.Errorf("ExistingNote = %q, want empty", p.ExistingNote)
	}
}

func TestPlanExtract_UnrelatedNote_Create(t *testing.T) {
	t.Parallel()
	// Parent has a user-authored note but no sci-extract sentinel.
	lister := &fakeLister{children: []ChildNote{
		{Key: "USER1", Body: "<p>my handwritten reading notes</p>"},
	}}
	p, err := PlanExtract(context.Background(), lister, baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate (unrelated note must not block)", p.Action)
	}
}

func TestPlanExtract_MatchingSentinel_Skip(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{children: []ChildNote{
		sentinelNote("NOTE1", "PDF1", "abc123"),
	}}
	p, err := PlanExtract(context.Background(), lister, baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if p.Action != ActionSkip {
		t.Errorf("action = %v, want ActionSkip", p.Action)
	}
	if p.ExistingNote != "NOTE1" {
		t.Errorf("ExistingNote = %q, want NOTE1", p.ExistingNote)
	}
}

func TestPlanExtract_HashDrift_Replace(t *testing.T) {
	t.Parallel()
	// Sentinel matches our pdfKey but the embedded hash is stale.
	lister := &fakeLister{children: []ChildNote{
		sentinelNote("NOTE1", "PDF1", "OLDHASH"),
	}}
	p, err := PlanExtract(context.Background(), lister, baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if p.Action != ActionReplace {
		t.Errorf("action = %v, want ActionReplace", p.Action)
	}
	if p.ExistingNote != "NOTE1" {
		t.Errorf("ExistingNote = %q, want NOTE1", p.ExistingNote)
	}
	if p.Reason == "" {
		t.Errorf("Reason empty; want a drift explanation")
	}
}

func TestPlanExtract_DifferentPDF_Create(t *testing.T) {
	t.Parallel()
	// Sentinel is for a different PDF key — parent has two PDFs,
	// we're extracting a second one and must not trample the first.
	lister := &fakeLister{children: []ChildNote{
		sentinelNote("NOTE1", "OTHERPDF", "zzz999"),
	}}
	p, err := PlanExtract(context.Background(), lister, baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate", p.Action)
	}
	if p.ExistingNote != "" {
		t.Errorf("ExistingNote = %q, want empty (different PDF, different note)", p.ExistingNote)
	}
}

func TestPlanExtract_Force_OverridesSkip(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{children: []ChildNote{
		sentinelNote("NOTE1", "PDF1", "abc123"),
	}}
	req := baseReq()
	req.Force = true
	p, err := PlanExtract(context.Background(), lister, req)
	if err != nil {
		t.Fatal(err)
	}
	// With force + a matching sentinel we replace (don't create a
	// duplicate) so the existing note key stays stable and no delete
	// is needed — consistent with the "never hard-delete" rule.
	if p.Action != ActionReplace {
		t.Errorf("action = %v, want ActionReplace (force + matching sentinel)", p.Action)
	}
	if p.ExistingNote != "NOTE1" {
		t.Errorf("ExistingNote = %q, want NOTE1", p.ExistingNote)
	}
}

func TestPlanExtract_Force_CreatesWhenNoExisting(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{children: nil}
	req := baseReq()
	req.Force = true
	p, err := PlanExtract(context.Background(), lister, req)
	if err != nil {
		t.Fatal(err)
	}
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate (force but nothing to replace)", p.Action)
	}
}

func TestPlanExtract_ListerError_Propagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("network down")
	lister := &fakeLister{err: boom}
	_, err := PlanExtract(context.Background(), lister, baseReq())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wraps %v", err, boom)
	}
}
