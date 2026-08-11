package oacache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/pkg/openalex"
)

// A targeted sync fetches a handful of works and MERGES them into the cache
// that is already there. Everything in this file exists to make that merge
// safe, because the failure it can produce is the quietest one this package
// has: a body that parses, counts, and loads — and holds a third of the
// corpus.
//
// Three rules, and each one is a bug that has already been paid for
// somewhere in this pipeline:
//
//  1. A merge NEVER narrows. Records the run did not fetch are copied
//     through as their original BYTES, so the field mask that produced them
//     survives untouched. Decoding and re-encoding every record would turn
//     a field this struct does not model into a field the cache no longer
//     has, and a narrowed mask is invisible in the data it produces.
//  2. A merge needs a base. Writing a delta into an empty directory
//     produces a file with the name, the shape and the sidecar of a whole
//     cache, and zot has no way to tell it from one — see [LoadBase].
//  3. A merge says so. The sidecar names the base it merged over by digest,
//     records what the run actually spent, and carries the last FULL sync's
//     accounting forward so the unattended runner can still price a full
//     run after any number of deltas.

// Base is an existing cache body, held as its raw NDJSON lines.
//
// The lines are kept as bytes on purpose: a merge replaces the records it
// re-fetched and copies every other line through unchanged, so a record
// this build's [openalex.Work] cannot fully model survives a delta intact.
type Base struct {
	// File is the body's name inside the staging directory.
	File string
	// Meta is the sidecar that vouched for the body. Zero-valued when there
	// is no sidecar — a hand-copied artifact, which is how the reference
	// title pool arrived before this verb existed.
	Meta Meta
	// SHA256 is the digest of the bytes actually read, not the sidecar's
	// claim about them. They are compared at load; see [LoadBase].
	SHA256 string

	lines  [][]byte
	index  map[string]int
	dois   map[string]bool
	absent map[string]bool
}

// LoadBase reads dir/name and its sidecar so a delta can merge into them.
//
// A missing body returns an error wrapping [os.ErrNotExist], and the caller
// is expected to REFUSE the run rather than proceed. This is the whole
// safety story of a delta mode: a targeted sync that wrote its four records
// into an empty staging directory would produce openalex-works.ndjson with
// a valid sidecar, and the next build would load those four records as the
// entire OpenAlex corpus.
//
// A body whose sidecar does not vouch for it is also refused. The sidecar
// is written LAST precisely so a torn write is detectable, and merging over
// half a body would launder the tear into a complete-looking cache.
func LoadBase(dir, name string) (Base, error) {
	body := filepath.Join(dir, name)
	raw, err := os.ReadFile(body) //nolint:gosec // path is the caller's own --out
	if err != nil {
		return Base{}, err
	}
	b := Base{File: name, index: map[string]int{}, dois: map[string]bool{}}
	for line := range bytes.SplitSeq(bytes.TrimRight(raw, "\n"), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec struct {
			ID  string  `json:"id"`
			DOI *string `json:"doi"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return Base{}, fmt.Errorf("%s line %d is not a work record: %w", name, len(b.lines)+1, err)
		}
		if id := shortID(rec.ID); id != "" {
			b.index[id] = len(b.lines)
		}
		if rec.DOI != nil {
			b.dois[normDOI(*rec.DOI)] = true
		}
		b.lines = append(b.lines, line)
	}
	if b.SHA256, err = digest(body); err != nil {
		return Base{}, err
	}

	b.absent = map[string]bool{}
	sidecar, err := os.ReadFile(body + ".meta.json") //nolint:gosec // sibling of the body
	switch {
	case err == nil:
		if err := json.Unmarshal(sidecar, &b.Meta); err != nil {
			return Base{}, fmt.Errorf("%s.meta.json is not readable: %w", name, err)
		}
		for _, d := range b.Meta.NotFound {
			b.absent[normDOI(d)] = true
		}
		if b.Meta.SHA256 != "" && b.Meta.SHA256 != b.SHA256 {
			return Base{}, fmt.Errorf(
				"%s does not match the digest its sidecar recorded — the last write did not finish", name)
		}
	case os.IsNotExist(err):
		// No sidecar: an artifact copied in by hand. Mergeable, but there
		// is nothing to check the body against and nothing to carry.
	default:
		return Base{}, err
	}
	return b, nil
}

// Records is how many records the base body holds.
func (b Base) Records() int { return len(b.lines) }

// IDs is every OpenAlex id the base names, in short form (W123…).
func (b Base) IDs() map[string]bool {
	out := make(map[string]bool, len(b.index))
	for id := range b.index {
		out[id] = true
	}
	return out
}

// HasDOI reports whether the base already holds a record for this DOI,
// compared the way [Fetch] compares a response against its request.
func (b Base) HasDOI(doi string) bool { return b.dois[normDOI(doi)] }

// KnownAbsent reports whether a previous run already asked OpenAlex for
// this DOI and got nothing.
//
// That is a measurement worth keeping: about 44 DOIs in this library are
// monographs, chapters and preprints OpenAlex does not index, and they will
// never stop being missing from the cache. Without this, "fetch what the
// cache lacks" targets them on every single run and spends the same 45
// requests forever. Naming one with --keys asks again anyway, which is the
// way back in for a work OpenAlex has since indexed.
func (b Base) KnownAbsent(doi string) bool { return b.absent[normDOI(doi)] }

// Merge counts what folding fresh records into a base body did. Replaced
// and Added are separated because they answer different questions: one is
// "how much of the cache moved", the other "how much of it is new".
type Merge struct {
	Replaced int `json:"records_replaced"`
	Added    int `json:"records_added"`
	Total    int `json:"records_total"`
}

// BaseRef identifies the body a delta merged over.
//
// The digest is the point. A sidecar that reported only the result cannot
// say WHICH cache the untouched records came from, so a merge over a stale
// or wrong base reads exactly like a merge over the right one.
type BaseRef struct {
	File       string `json:"file"`
	Records    int    `json:"records"`
	SHA256     string `json:"sha256,omitempty"`
	ProducedAt string `json:"produced_at,omitempty"`
}

// Delta records that a body was MERGED rather than replaced, and what the
// run that merged it targeted and spent.
//
// Requests is the whole run's metered spend — the works arm plus the cited
// arm — because the caller that has to live inside a request cap needs the
// number it will be judged by, not one of its halves.
type Delta struct {
	Keys    []string `json:"keys,omitempty"`
	Missing bool     `json:"missing,omitempty"`
	// DOIsAsked are the identifiers this run looked up. It is what lets the
	// merge keep not_found honest: a DOI the base listed as absent and this
	// run never asked about STAYS listed, and one it did ask about is
	// re-answered by this run. Without it a delta could only append misses,
	// and a DOI OpenAlex finally indexed would be reported absent forever.
	DOIsAsked       []string `json:"dois_asked,omitempty"`
	ItemsTargeted   int      `json:"items_targeted"`
	Requests        int      `json:"requests"`
	RecordsReplaced int      `json:"records_replaced"`
	RecordsAdded    int      `json:"records_added"`
	Base            BaseRef  `json:"base"`
}

// WriteDelta merges res.Works into the works cache dir/[CacheFile] and
// writes the sidecar last, exactly as [Write] does.
//
// The base must have been loaded from the same directory. Records the run
// re-fetched are replaced in place; records it never saw keep their bytes.
//
// d is stamped with what the write did — the base it merged over and the
// records it replaced and added — because the sidecar and the caller's
// result must not be able to disagree about the same run. Taking it by
// value once left the sidecar naming its base and the CLI reporting none.
func WriteDelta(dir, scope string, res Result, base Base, d *Delta) (string, Merge, error) {
	return writeMerged(dir, base, res.Works, d, Meta{
		ProducedAt: time.Now().UTC().Format(time.RFC3339),
		ProducedBy: "sci zot openalex sync (delta)",
		Scope:      scope,
		Select:     WorkSelect,
		Stats:      res.Stats,
		NotFound:   MergeNotFound(base.Meta.NotFound, d.DOIsAsked, res.NotFound),
	})
}

// MergeNotFound folds a run's misses into the ones already recorded.
//
// The list answers "we asked OpenAlex and it has nothing", and that is a
// statement about the CORPUS, not about one run. A delta that published
// only its own answers would shrink it from ~90 DOIs to two, and the next
// targeted run would pay again for every monograph and preprint OpenAlex
// has never indexed — about 44 requests, every run, forever.
//
// A DOI this run DID ask about is re-answered by this run either way, so a
// record OpenAlex has finally indexed leaves the list rather than being
// stuck there.
func MergeNotFound(base, asked, now []string) []string {
	askedSet := lo.SliceToMap(asked, func(d string) (string, bool) { return normDOI(d), true })
	seen := map[string]bool{}
	out := lo.Filter(base, func(d string, _ int) bool {
		k := normDOI(d)
		keep := !askedSet[k] && !seen[k]
		seen[k] = seen[k] || keep
		return keep
	})
	for _, d := range now {
		k := normDOI(d)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	return out
}

// WriteCitedDelta merges hydrated reference records into the title pool
// dir/[CitedFile]. The pool is append-heavy by nature: a delta adds the
// works its new papers cite and leaves the other ~186k alone.
func WriteCitedDelta(dir, scope string, res CitedResult, base Base, d *Delta) (string, Merge, error) {
	return writeMerged(dir, base, res.Works, d, Meta{
		ProducedAt: time.Now().UTC().Format(time.RFC3339),
		ProducedBy: "sci zot openalex sync (cited works, delta)",
		Scope:      scope,
		Select:     CitedSelect,
	})
}

func writeMerged(dir string, base Base, fresh []openalex.Work, d *Delta, meta Meta) (string, Merge, error) {
	lines, m, err := mergeLines(base, fresh)
	if err != nil {
		return "", m, err
	}
	d.RecordsReplaced, d.RecordsAdded = m.Replaced, m.Added
	d.Base = BaseRef{
		File:       base.File,
		Records:    base.Records(),
		SHA256:     base.SHA256,
		ProducedAt: base.Meta.ProducedAt,
	}
	meta.RecordsTotal = m.Total
	stamped := *d
	meta.Delta = &stamped
	meta.FullSyncStats = carriedFullSyncStats(base.Meta)

	path, err := writeLines(dir, base.File, lines, meta)
	return path, m, err
}

// mergeLines folds fresh records into the base's lines: a record the base
// already names replaces it IN PLACE, a record it does not is appended.
//
// Replacing in place rather than appending is not cosmetic. Two lines with
// the same OpenAlex id give one work two rows, and zot refuses an ambiguous
// match — so a merge that appended would grow the file and silently REDUCE
// the number of papers that resolve.
func mergeLines(base Base, fresh []openalex.Work) ([][]byte, Merge, error) {
	lines := slices.Clone(base.lines)
	var m Merge
	seen := map[string]bool{}
	for i := range fresh {
		id := shortID(fresh[i].ID)
		if id != "" && seen[id] {
			continue
		}
		seen[id] = true
		raw, err := json.Marshal(fresh[i])
		if err != nil {
			return nil, m, fmt.Errorf("encode work %s: %w", fresh[i].ID, err)
		}
		if pos, ok := base.index[id]; ok && id != "" {
			lines[pos] = raw
			m.Replaced++
			continue
		}
		lines = append(lines, raw)
		m.Added++
	}
	m.Total = len(lines)
	return lines, m, nil
}

// carriedFullSyncStats keeps the last FULL sync's accounting reachable
// through any number of deltas.
//
// The unattended runner prices a full sync from `stats.requests` in this
// sidecar — ~4,200 requests, which is what makes its 50-request cap defer.
// A delta writes its own honest `stats` (two requests, say), so without
// this the first targeted run would tell the runner a full sync is free and
// the second would authorise one.
func carriedFullSyncStats(base Meta) *Stats {
	switch {
	case base.FullSyncStats != nil:
		return base.FullSyncStats
	case base.Delta != nil:
		// A delta whose own base carried nothing. Nothing to invent.
		return nil
	case base.Stats == (Stats{}):
		return nil
	default:
		s := base.Stats
		return &s
	}
}

// CitedRequestsPerLookup prices the reference-hydration arm of a plan.
//
// It is a measurement rounded up, not a guess: the full sync that produced
// this corpus spent 3,354 cited requests over 5,789 looked-up items —
// 0.58 per item — because references are hydrated 50 ids to a request and
// ids the pool already holds are skipped. One per lookup is therefore a
// ceiling with roughly 2x headroom, and a targeted sync into a warm pool
// spends far less.
const CitedRequestsPerLookup = 1

// Plan is what a sync WOULD cost, priced without making a request.
//
// Two totals, because two of the arms are certain and one is not.
// Requests is the arms every run pays — the batched DOI lookups, the title
// lookups, and the reference hydration — and it is the number a spend cap
// should be compared against. RequestsMax adds the fallback arm, which
// fires once per DOI OpenAlex turns out not to hold (measured at under 2%
// of them); folding its worst case into the headline would triple every
// estimate and defer runs that cost eight requests.
//
// Mode and ItemsTargeted are filled by the caller — this package prices a
// lookup plan and knows nothing about Zotero items.
type Plan struct {
	Mode            string `json:"mode,omitempty"`
	ItemsTargeted   int    `json:"items_targeted"`
	DOIs            int    `json:"dois"`
	DOIsUnbatchable int    `json:"dois_unbatchable"`
	Titles          int    `json:"titles"`
	Lookups         int    `json:"lookups"`
	DOIRequests     int    `json:"doi_requests"`
	TitleRequests   int    `json:"title_requests"`
	CitedRequests   int    `json:"cited_requests"`
	FallbackMax     int    `json:"fallback_max"`
	Requests        int    `json:"requests"`
	RequestsMax     int    `json:"requests_max"`
}

// Estimate prices a [Want] using the same batching [Fetch] and [FetchCited]
// use. It makes no request and needs no client, which is the point: an
// unattended caller has to know what a run costs BEFORE it is allowed to
// spend anything.
func Estimate(w Want) Plan {
	batchable, solo := lo.FilterReject(w.DOIs, func(d string, _ int) bool { return Batchable(d) })
	p := Plan{
		DOIs:            len(w.DOIs),
		DOIsUnbatchable: len(solo),
		Titles:          len(w.Titles),
		Lookups:         len(w.DOIs) + len(w.Titles),
		DOIRequests:     batches(len(batchable), doiBatch) + len(solo),
		TitleRequests:   len(w.Titles),
		FallbackMax:     len(w.DOIs),
	}
	p.CitedRequests = p.Lookups * CitedRequestsPerLookup
	p.Requests = p.DOIRequests + p.TitleRequests + p.CitedRequests
	p.RequestsMax = p.Requests + p.FallbackMax
	return p
}
