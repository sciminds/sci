package content

import (
	"cmp"
	"strings"

	"github.com/samber/lo"
)

// Candidate is what the library can offer for one top-level item, before
// a source has been chosen. Both sources are optional and most items
// have both; [Candidate.Choose] resolves the pair to one.
//
// It carries locators rather than text — 5,000 candidates fit in memory,
// 5,000 paper bodies do not — so [Build] can load bodies one at a time.
type Candidate struct {
	ItemKey string

	// DoclingNoteID is the local row id of the item's docling extraction
	// note, or 0 if it has none.
	DoclingNoteID  int64
	DoclingVersion int64

	// AttachmentKey is the item's PDF attachment key, used to locate
	// Zotero's .zotero-ft-cache on disk. Empty if the item has no
	// indexed attachment.
	AttachmentKey string
	ZoteroVersion int64
}

// Choose resolves a candidate's two possible sources to the one that
// should be indexed, returning false when the item has no text at all.
//
// This single function is where the "graceful fallback" lives. Because
// it runs at index time, the search path never has to know that two
// sources exist.
func (c Candidate) Choose() (Source, int64, bool) {
	switch {
	case c.DoclingNoteID != 0:
		return SourceDocling, c.DoclingVersion, true
	case c.AttachmentKey != "":
		return SourceZotero, c.ZoteroVersion, true
	default:
		return "", 0, false
	}
}

// Plan is the diff between the library and the index: what to add,
// refresh, and drop. Producing it reads no bodies, so it is cheap enough
// to compute on every search.
type Plan struct {
	Add       []Candidate
	Update    []Candidate
	Delete    []string
	Unchanged int
}

// Total is how many items the plan will touch.
func (p Plan) Total() int { return len(p.Add) + len(p.Update) + len(p.Delete) }

// Empty reports whether the index is already up to date.
func (p Plan) Empty() bool { return p.Total() == 0 }

// NewPlan diffs the library's candidates against the index's current
// state.
//
// An item is reindexed when either its source or its version differs.
// Checking the source matters on its own: gaining a docling extraction
// is an upgrade even when the new note's version number is lower than
// the Zotero attachment it replaces.
func NewPlan(candidates []Candidate, indexed map[string]DocState) Plan {
	return newPlan(candidates, indexed, false)
}

// NewRebuildPlan is [NewPlan] with every indexed item forced to Update.
// It is what an [IndexFormat] bump calls for: the versions still match,
// but the text behind them was normalized by rules this code no longer
// uses, so "unchanged version" no longer means "unchanged text".
func NewRebuildPlan(candidates []Candidate, indexed map[string]DocState) Plan {
	return newPlan(candidates, indexed, true)
}

func newPlan(candidates []Candidate, indexed map[string]DocState, rebuild bool) Plan {
	var p Plan
	seen := make(map[string]bool, len(candidates))

	for _, c := range candidates {
		seen[c.ItemKey] = true
		source, version, ok := c.Choose()
		if !ok {
			// The item lost its text (extraction trashed, attachment
			// removed). Drop it rather than serving what it used to say.
			if _, indexedNow := indexed[c.ItemKey]; indexedNow {
				p.Delete = append(p.Delete, c.ItemKey)
			}
			continue
		}
		prior, indexedNow := indexed[c.ItemKey]
		switch {
		case !indexedNow:
			p.Add = append(p.Add, c)
		case rebuild || prior.Source != source || prior.Version != version:
			p.Update = append(p.Update, c)
		default:
			p.Unchanged++
		}
	}

	// Anything the index knows about that the library no longer lists.
	for key := range indexed {
		if !seen[key] {
			p.Delete = append(p.Delete, key)
		}
	}
	return p
}

// LoadFunc fetches the raw text for a candidate's chosen source. Build
// calls it once per item, so implementations should read exactly one
// note body or one cache file.
type LoadFunc func(c Candidate, src Source) (string, error)

// Options tunes a build.
type Options struct {
	// BatchSize is how many docs are written per transaction.
	// Zero means DefaultBatchSize.
	BatchSize int
	// Progress, if set, is called after each batch with the number of
	// items processed so far and the plan's total.
	Progress func(done, total int)
}

// DefaultBatchSize balances transaction overhead against how much text
// is held in memory at once. Paper bodies average tens of kilobytes, so
// a few hundred per batch is tens of megabytes.
const DefaultBatchSize = 200

// Result reports what a build did.
type Result struct {
	Added    int            `json:"added"`
	Updated  int            `json:"updated"`
	Deleted  int            `json:"deleted"`
	Skipped  int            `json:"skipped"`
	BySource map[Source]int `json:"by_source"`
	// Failed maps item key to the error that stopped it. A per-item
	// failure never aborts the build — one unreadable cache file must
	// not cost a five-thousand-item index.
	Failed map[string]string `json:"failed,omitempty"`
}

// Build executes a plan against the index.
//
// Errors loading an individual item are recorded in [Result.Failed] and
// the build continues; only a failure to write the index itself is
// returned as an error.
func Build(ix *Index, p Plan, load LoadFunc, opts Options) (Result, error) {
	batchSize := cmp.Or(opts.BatchSize, DefaultBatchSize)
	res := Result{BySource: map[Source]int{}, Failed: map[string]string{}}

	if len(p.Delete) > 0 {
		if err := ix.Delete(p.Delete); err != nil {
			return res, err
		}
		res.Deleted = len(p.Delete)
	}

	// Adds and updates differ only in how they are counted — Upsert
	// handles both — so walk them as one stream and tag each entry.
	type work struct {
		cand  Candidate
		isNew bool
	}
	stream := append(
		lo.Map(p.Add, func(c Candidate, _ int) work { return work{cand: c, isNew: true} }),
		lo.Map(p.Update, func(c Candidate, _ int) work { return work{cand: c} })...,
	)

	done := res.Deleted
	total := p.Total()
	batch := make([]Doc, 0, batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := ix.Upsert(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for _, w := range stream {
		done++
		source, version, ok := w.cand.Choose()
		if !ok {
			// NewPlan never routes a sourceless candidate here, but a
			// hand-built plan could.
			res.Skipped++
			continue
		}
		body, err := load(w.cand, source)
		if err != nil {
			res.Failed[w.cand.ItemKey] = err.Error()
			continue
		}
		if strings.TrimSpace(body) == "" {
			// A scanned PDF with no OCR, a truncated cache file, or an
			// extraction note that is nothing but its provenance header.
			//
			// Record it as an empty document rather than skipping the
			// write. Two things depend on that. It replaces whatever the
			// item used to say, so a paper whose text disappears stops
			// matching what it no longer has. And it stamps the version,
			// so the next plan sees the item as up to date instead of
			// re-planning it forever — an item that can never be indexed
			// must not report itself as pending work on every run.
			//
			// Coverage stays honest because [Index.Stats] counts only
			// rows that have text.
			batch = append(batch, Doc{
				ItemKey: w.cand.ItemKey,
				Source:  source,
				Version: version,
			})
			res.Skipped++
			if len(batch) >= batchSize {
				if err := flush(); err != nil {
					return res, err
				}
			}
			continue
		}

		batch = append(batch, Doc{
			ItemKey: w.cand.ItemKey,
			Source:  source,
			Version: version,
			Body:    body,
		})
		if w.isNew {
			res.Added++
		} else {
			res.Updated++
		}
		res.BySource[source]++

		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return res, err
			}
			if opts.Progress != nil {
				opts.Progress(done, total)
			}
		}
	}
	if err := flush(); err != nil {
		return res, err
	}
	if opts.Progress != nil {
		opts.Progress(done, total)
	}

	if len(res.Failed) == 0 {
		res.Failed = nil
	}
	return res, nil
}
