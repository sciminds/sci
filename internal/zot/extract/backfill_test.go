package extract

import (
	"context"
	"errors"
	"testing"
)

// fakeTagAdder records every AddTagToItem call. errOn lets a test inject
// a per-key failure without erroring the rest of the batch.
type fakeTagAdder struct {
	calls []tagCall
	errOn map[string]error
}

func (f *fakeTagAdder) AddTagToItem(_ context.Context, item, tag string) error {
	f.calls = append(f.calls, tagCall{item: item, tag: tag})
	if e, ok := f.errOn[item]; ok {
		return e
	}
	return nil
}

func TestBackfillParentTag_HappyPath(t *testing.T) {
	t.Parallel()
	w := &fakeTagAdder{}
	res := BackfillParentTag(context.Background(), w, []string{"P1", "P2", "P3"}, MarkdownTag, nil)

	if len(res.Tagged) != 3 {
		t.Errorf("Tagged = %v, want 3", res.Tagged)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %v, want empty", res.Failed)
	}
	if len(w.calls) != 3 {
		t.Errorf("AddTagToItem calls = %d, want 3", len(w.calls))
	}
	for _, c := range w.calls {
		if c.tag != MarkdownTag {
			t.Errorf("call %+v: tag = %q, want %q", c, c.tag, MarkdownTag)
		}
	}
}

// TestBackfillParentTag_PartialFailure: one bad parent does not abort
// the rest — the loop carries on and records the error per-key.
func TestBackfillParentTag_PartialFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("412 conflict")
	w := &fakeTagAdder{errOn: map[string]error{"P2": boom}}

	var advanced []string
	res := BackfillParentTag(
		context.Background(),
		w,
		[]string{"P1", "P2", "P3"},
		MarkdownTag,
		func(key string, err error) { advanced = append(advanced, key) },
	)

	if len(res.Tagged) != 2 {
		t.Errorf("Tagged = %v, want 2 (P1 + P3)", res.Tagged)
	}
	if !errors.Is(res.Failed["P2"], boom) {
		t.Errorf("Failed[P2] = %v, want %v", res.Failed["P2"], boom)
	}
	if len(advanced) != 3 {
		t.Errorf("onAdvance fired %d times, want 3 (one per key incl. failure)", len(advanced))
	}
}

// TestBackfillParentTag_ContextCancellation: a cancelled context stops
// further AddTagToItem calls but still records remaining keys as failed.
func TestBackfillParentTag_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := &fakeTagAdder{}
	res := BackfillParentTag(ctx, w, []string{"P1", "P2"}, MarkdownTag, nil)

	if len(w.calls) != 0 {
		t.Errorf("calls = %d, want 0 (context already cancelled)", len(w.calls))
	}
	if len(res.Failed) != 2 {
		t.Errorf("Failed = %v, want 2 ctx errors", res.Failed)
	}
}
