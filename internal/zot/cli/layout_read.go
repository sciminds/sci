package cli

// layout_read.go — serving extraction markdown from the per-key layout
// store when Zotero has no note for the item.
//
// An extraction lands in two independent stores (see the zot CLAUDE.md
// "Two independent stores" note): the Zotero child note and the layout
// dir under extract.dir. Either can be missing on its own, and one case
// is permanent — markdown over Zotero's ~500KB note limit can never be
// posted, so those papers live in the layout store alone. The
// note-reading verbs consult it as a last resort so a fully extracted
// paper is never reported as having no text.

import (
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/sciminds/cli/internal/zot/local"
)

// SourceLayout labels text served from the layout store rather than
// from a Zotero note or the content index. It is deliberately not a
// [content.Source]: nothing ever indexes it, so it can't appear in a
// by-source breakdown — it only ever describes one read.
const SourceLayout = "layout"

// layoutExtraction returns key's extracted markdown from the layout
// store. ok is false — with no error — whenever the store can't answer:
// extract.dir unconfigured, no completed extraction for key, or key not
// in db's library. Only a dir that passes [extract.KeyLayout.Done] is
// served, so an interrupted or crashed extraction reads as absent
// instead of as a truncated paper.
//
// The scope check is why db is a parameter. A layout dir is keyed by
// item key alone — the store has no library dimension, and personal and
// shared extractions land in the same extract.dir — so without it
// `content read KEY --library personal` happily answers with a shared
// library's paper.
func layoutExtraction(cfg *zot.Config, db local.Reader, key string) (body string, ok bool, err error) {
	if cfg == nil || cfg.Extract.Dir == "" {
		return "", false, nil
	}
	layout := &extract.KeyLayout{Dir: cfg.Extract.Dir}
	if !layout.Done(key) {
		return "", false, nil
	}
	if _, err := db.Read(key); err != nil {
		if _, missing := errors.AsType[*local.ItemNotFoundError](err); missing {
			return "", false, nil
		}
		return "", false, err
	}
	raw, err := os.ReadFile(layout.MarkdownPath(key))
	if err != nil {
		return "", false, fmt.Errorf("read layout markdown for %s: %w", key, err)
	}
	return string(raw), true, nil
}
