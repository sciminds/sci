package extract

import "context"

// TagAdder is the narrow Zotero-write interface BackfillParentTag needs.
// Implemented by *api.Client. Defining a sub-interface (instead of
// reusing NoteWriter) keeps the dependency surface honest: the backfill
// loop never creates notes.
type TagAdder interface {
	AddTagToItem(ctx context.Context, itemKey, tag string) error
}

// BackfillResult tallies what BackfillParentTag did. Tagged lists keys
// that were successfully patched (or that already had the tag — the
// underlying AddTagToItem dedups). Failed maps each errored key to its
// error so callers can render per-item failures.
type BackfillResult struct {
	Tagged []string
	Failed map[string]error
}

// BackfillParentTag adds tag to every key in parents via w. Idempotent:
// AddTagToItem must short-circuit when the tag is already present (the
// *api.Client implementation inspects the current tag set first), so
// running this on every --apply is safe.
//
// onAdvance fires once per key (success or failure) so callers can
// drive a progress bar. Safe to be nil. Returns even after a partial
// failure — one bad parent doesn't abort the rest.
func BackfillParentTag(
	ctx context.Context,
	w TagAdder,
	parents []string,
	tag string,
	onAdvance func(key string, err error),
) BackfillResult {
	res := BackfillResult{Failed: map[string]error{}}
	for _, k := range parents {
		if err := ctx.Err(); err != nil {
			res.Failed[k] = err
			if onAdvance != nil {
				onAdvance(k, err)
			}
			continue
		}
		err := w.AddTagToItem(ctx, k, tag)
		if err != nil {
			res.Failed[k] = err
		} else {
			res.Tagged = append(res.Tagged, k)
		}
		if onAdvance != nil {
			onAdvance(k, err)
		}
	}
	return res
}
