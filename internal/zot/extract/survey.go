package extract

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
)

// ContentKey projects a HashPDF fingerprint onto its content-only
// identity by dropping the mtime field: "<size>-<mtime>-<sha16>" →
// "<size>-<sha16>". Two attachments holding the same PDF bytes almost
// always differ in mtime (separate downloads/imports), so the full
// fingerprint cannot answer "is this the same document" — only this
// projection can. The sha16 covers the first 1 MiB only (see HashPDF);
// good enough for an advisory duplicate warning that is never acted on
// automatically. Returns "" for an unparseable or empty hash; callers
// must never group on "" (a hash failure is not evidence of sameness).
func ContentKey(hash string) string {
	parts := strings.Split(hash, "-")
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return ""
	}
	return parts[0] + "-" + parts[2]
}

// Disposition is what a planned run would do to one candidate.
type Disposition string

// The closed disposition vocabulary, stable in --json output.
const (
	// DispExtract — docling would run on this item's PDF.
	DispExtract Disposition = "extract"
	// DispPostCached — extracted markdown is already on disk (cache or
	// layout dir); only --apply's note posting remains.
	DispPostCached Disposition = "post-cached"
	// DispArtifactsOnly — a Zotero note exists but the layout key dir is
	// missing: docling runs for the artifacts, no note is posted.
	DispArtifactsOnly Disposition = "artifacts-only"
	// DispDone — note and (in layout mode) key dir are both present.
	DispDone Disposition = "done"
	// DispSkip — an existing Zotero note settles the item (classic mode).
	DispSkip Disposition = "skip"
	// DispError — hashing/planning failed; the item is reported, never run.
	DispError Disposition = "error"
)

// PageCounter reports a PDF's page count without extracting it. This is
// the scheduling-workstream seam: nil means the estimator has not landed
// and the survey degrades to counts-only (no buckets, no ETA). An error
// for one PDF is not fatal — that item counts as unknown.
type PageCounter func(pdfPath string) (pages int, err error)

// SurveyItem is one candidate's classification in a Survey.
type SurveyItem struct {
	ParentKey   string      `json:"parent_key"`
	PDFKey      string      `json:"pdf_key"`
	PDFName     string      `json:"pdf_name"`
	PDFPath     string      `json:"-"`
	Hash        string      `json:"-"`
	Content     string      `json:"-"` // ContentKey(Hash); "" = no identity
	Disposition Disposition `json:"disposition"`
	Pages       int         `json:"pages,omitempty"` // 0 = unknown
	Err         string      `json:"error,omitempty"`
}

// DuplicateMember is one item of a DuplicateGroup.
type DuplicateMember struct {
	ParentKey   string      `json:"parent_key"`
	PDFKey      string      `json:"pdf_key"`
	PDFName     string      `json:"pdf_name"`
	Disposition Disposition `json:"disposition"`
}

// DuplicateGroup reports two or more items whose PDFs share identical
// content (by ContentKey) where at least one member would burn a docling
// run this batch. Duplicates are warned, never auto-deduped: layout
// artifacts are key-named (StageKeyPDF stems every output by parent
// key, Finalize rebuilds <Dir>/<KEY>/ wholesale), so one extraction
// cannot be posted under two parents without a copy-and-rewrite path
// that does not exist; the markdown cache is keyed (PDFKey, hash), so
// even the cheap path never collapses; and the two parents may be
// legitimately distinct records (preprint vs. version of record) the
// user asked for individually. Merging is the user's call — `sci zot
// doctor duplicates` is the triage surface.
type DuplicateGroup struct {
	Content string            `json:"content"`
	Pages   int               `json:"pages,omitempty"`
	Queued  int               `json:"queued"` // members that would run docling this run
	Members []DuplicateMember `json:"members"`
}

// PageBucket is one row of the plan's page-count histogram.
type PageBucket struct {
	Label string `json:"label"` // e.g. "101-300", "301+"
	Min   int    `json:"min"`
	Max   int    `json:"max"` // 0 = unbounded
	Items int    `json:"items"`
	Pages int    `json:"pages"`
}

// SurveyInput carries everything BuildSurvey needs. Items is PlanBatch's
// output, unfiltered; Candidates/AlreadyDone/LayoutDone are captured by
// the CLI at its pre-plan reject so the survey and the reject can't
// disagree.
type SurveyInput struct {
	Items     []BatchItem
	Cache     *MarkdownCache
	Layout    *KeyLayout // nil = classic mode
	Apply     bool
	Reextract bool
	Limit     int
	Jobs      int    // --jobs as passed (0 = single process)
	Device    string // docling device, for the ETA rate
	Pages     PageCounter

	// SecondsPerPage overrides the Device guess for the ETA — the
	// corpus-observed rate from [CalibrateSecondsPerPage]. 0 means fall
	// back to [SecondsPerPage]. CalibrationSamples is how many documents
	// it was measured over, carried so the plan can attribute it.
	SecondsPerPage     float64
	CalibrationSamples int

	Candidates  int  // parents with a PDF, before any filtering
	AlreadyDone int  // dropped pre-plan (note exists; and dir exists in layout mode)
	LayoutDone  *int // layout.Done count over ALL candidates; nil in classic mode
}

// Survey is the read-only description of what a run would do — the
// single source both `--plan` and the live run consume. Selected is
// exactly the []BatchItem ExecuteBatch should receive, in order, and
// CachedIdx indexes into it; the live run reading those same two fields
// is what keeps a plan from lying.
type Survey struct {
	Selected  []BatchItem
	CachedIdx map[int]bool

	Items      []SurveyItem // every planned candidate, aligned with SurveyInput.Items
	Duplicates []DuplicateGroup

	Candidates  int
	AlreadyDone int
	LayoutDone  *int

	Cached          int
	NeedsExtraction int
	ArtifactsOnly   int
	Skipped         int
	PlanErrors      int

	Limit     int
	Remaining int // selected candidates cut by --limit

	WouldInvalidateCache int // --reextract: cache entries a run would delete
	WouldClearLayoutDone int // --reextract: .done markers a run would drop

	DuplicateWasted int // Σ over groups of (Queued-1)

	// Page/ETA aggregates; zero-valued when Pages was nil or nothing is
	// queued. Item counts, except PagesTotal (pages).
	PagesKnownItems   int
	PagesUnknownItems int
	PagesTotal        int
	Extrapolated      bool
	Buckets           []PageBucket
	ETA               time.Duration // 0 = unknown

	// SecondsPerPage is the rate ETA was computed at, and
	// CalibrationSamples the number of finished extractions behind it —
	// 0 when the rate is the device guess rather than a measurement.
	SecondsPerPage     float64
	CalibrationSamples int

	Errors map[string]string // parentKey → plan error
}

// BuildSurvey classifies every planned item, applies the cache filter
// and --limit exactly as the live run does, and derives the duplicate
// groups and page aggregates. Read-only: it stats the cache and the
// layout and never writes — under --reextract it reports what a run
// would invalidate instead of deleting anything.
func BuildSurvey(in SurveyInput) Survey {
	s := Survey{
		CachedIdx:   map[int]bool{},
		Candidates:  in.Candidates,
		AlreadyDone: in.AlreadyDone,
		LayoutDone:  in.LayoutDone,
		Limit:       in.Limit,
		// A measured rate displaces the device guess; the sample count
		// rides along so the plan can say which one it used.
		SecondsPerPage:     cmp.Or(in.SecondsPerPage, SecondsPerPage(in.Device)),
		CalibrationSamples: in.CalibrationSamples,
	}
	if in.SecondsPerPage <= 0 {
		s.CalibrationSamples = 0
	}
	layoutMode := in.Layout != nil

	// ── Classify + cache-filter (mirrors the live run item for item) ──
	for _, it := range in.Items {
		si := SurveyItem{
			ParentKey: it.Request.ParentKey,
			PDFKey:    it.Request.PDFKey,
			PDFName:   it.Request.PDFName,
			PDFPath:   it.Request.PDFPath,
			Hash:      it.Hash,
			Content:   ContentKey(it.Hash),
		}
		keep := true
		switch {
		case it.Err != nil:
			si.Disposition = DispError
			si.Err = it.Err.Error()
			s.PlanErrors++
			if s.Errors == nil {
				s.Errors = map[string]string{}
			}
			s.Errors[it.Request.ParentKey] = it.Err.Error()
		case it.Plan.Action == ActionSkip:
			switch {
			case layoutMode && !in.Layout.Done(it.Request.ParentKey):
				si.Disposition = DispArtifactsOnly
				s.ArtifactsOnly++
			case layoutMode:
				si.Disposition = DispDone
				s.Skipped++
			default:
				si.Disposition = DispSkip
				s.Skipped++
			}
		default: // ActionCreate
			switch {
			case layoutMode && in.Layout.Done(it.Request.ParentKey):
				// Note missing, dir present — the run posts from the
				// layout markdown without docling.
				si.Disposition = DispPostCached
				s.Cached++
			case layoutMode:
				// The markdown cache is never consulted in layout mode
				// (it can't reproduce the DoclingDocument JSON).
				si.Disposition = DispExtract
				s.NeedsExtraction++
			default:
				if _, hit := in.Cache.Get(it.Request.PDFKey, it.Hash); hit && !in.Reextract {
					si.Disposition = DispPostCached
					s.Cached++
					if in.Apply {
						s.CachedIdx[len(s.Selected)] = true
					} else {
						// Cache-only mode: nothing left to do — drop it.
						keep = false
					}
				} else {
					si.Disposition = DispExtract
					s.NeedsExtraction++
				}
			}
		}
		if keep {
			s.Selected = append(s.Selected, it)
		}
		s.Items = append(s.Items, si)
	}

	// ── --limit, after filtering, with CachedIdx re-indexed ──
	preLimit := len(s.Selected)
	if in.Limit > 0 && in.Limit < len(s.Selected) {
		s.Selected = s.Selected[:in.Limit]
		for i := range s.CachedIdx {
			if i >= in.Limit {
				delete(s.CachedIdx, i)
			}
		}
	}
	s.Remaining = preLimit - len(s.Selected)

	// ── --reextract impact, over the post-limit selection (the live
	// run's deletion loop runs after --limit) ──
	if in.Reextract {
		for _, it := range s.Selected {
			if it.Err != nil {
				continue
			}
			if _, ok := in.Cache.Get(it.Request.PDFKey, it.Hash); ok && it.Hash != "" {
				s.WouldInvalidateCache++
			}
			if layoutMode && in.Layout.Done(it.Request.ParentKey) {
				s.WouldClearLayoutDone++
			}
		}
	}

	// ── Page counting + ETA, over the post-limit queue (so --plan's
	// estimator cost is bounded by the same --limit as the run) ──
	if in.Pages != nil {
		idxByKey := map[string]int{}
		for i, si := range s.Items {
			idxByKey[si.ParentKey] = i
		}
		for _, it := range s.Selected {
			i, ok := idxByKey[it.Request.ParentKey]
			if !ok {
				continue
			}
			d := s.Items[i].Disposition
			if d != DispExtract && d != DispArtifactsOnly {
				continue
			}
			if n, err := in.Pages(s.Items[i].PDFPath); err == nil && n > 0 {
				s.Items[i].Pages = n
				s.PagesKnownItems++
				s.PagesTotal += n
			} else {
				s.PagesUnknownItems++
			}
		}
		if s.PagesKnownItems > 0 && s.PagesUnknownItems > 0 {
			s.PagesTotal = s.PagesTotal * (s.PagesKnownItems + s.PagesUnknownItems) / s.PagesKnownItems
			s.Extrapolated = true
		}
		queued := lo.Filter(s.Items, func(si SurveyItem, _ int) bool {
			return si.Disposition == DispExtract || si.Disposition == DispArtifactsOnly
		})
		s.Buckets = PageBuckets(queued)
		if s.PagesKnownItems > 0 {
			s.ETA = EstimateDurationAt(s.PagesTotal, in.Jobs, s.SecondsPerPage)
		}
	}

	s.Duplicates = duplicateGroups(s.Items)
	s.DuplicateWasted = lo.SumBy(s.Duplicates, func(g DuplicateGroup) int {
		return max(g.Queued-1, 0)
	})
	return s
}

// duplicateGroups groups every surveyed item by content identity and
// reports groups of ≥2 where at least one member would run docling this
// batch — a group where nothing is queued wastes nothing and is noise.
func duplicateGroups(items []SurveyItem) []DuplicateGroup {
	withID := lo.Filter(items, func(si SurveyItem, _ int) bool { return si.Content != "" })
	groups := lo.GroupBy(withID, func(si SurveyItem) string { return si.Content })

	var out []DuplicateGroup
	for content, members := range groups {
		if len(members) < 2 {
			continue
		}
		queued := lo.CountBy(members, func(si SurveyItem) bool { return si.Disposition == DispExtract })
		if queued == 0 {
			continue
		}
		slices.SortFunc(members, func(a, b SurveyItem) int { return cmp.Compare(a.ParentKey, b.ParentKey) })
		pages := 0
		if known, ok := lo.Find(members, func(si SurveyItem) bool { return si.Pages > 0 }); ok {
			pages = known.Pages
		}
		out = append(out, DuplicateGroup{
			Content: content,
			Pages:   pages,
			Queued:  queued,
			Members: lo.Map(members, func(si SurveyItem, _ int) DuplicateMember {
				return DuplicateMember{
					ParentKey:   si.ParentKey,
					PDFKey:      si.PDFKey,
					PDFName:     si.PDFName,
					Disposition: si.Disposition,
				}
			}),
		})
	}
	slices.SortFunc(out, func(a, b DuplicateGroup) int {
		return cmp.Or(
			cmp.Compare(b.Queued, a.Queued),
			cmp.Compare(b.Pages, a.Pages),
			cmp.Compare(a.Content, b.Content),
		)
	})
	return out
}

// pageBucketBounds are the fixed histogram boundaries for PageBuckets.
// Max 0 means unbounded.
var pageBucketBounds = []PageBucket{
	{Label: "1-10", Min: 1, Max: 10},
	{Label: "11-25", Min: 11, Max: 25},
	{Label: "26-50", Min: 26, Max: 50},
	{Label: "51-100", Min: 51, Max: 100},
	{Label: "101-300", Min: 101, Max: 300},
	{Label: "301+", Min: 301, Max: 0},
}

// PageBuckets partitions items with a known page count into the fixed
// histogram rows; empty buckets are omitted.
func PageBuckets(items []SurveyItem) []PageBucket {
	buckets := slices.Clone(pageBucketBounds)
	for _, si := range items {
		if si.Pages <= 0 {
			continue
		}
		for i := range buckets {
			if si.Pages >= buckets[i].Min && (buckets[i].Max == 0 || si.Pages <= buckets[i].Max) {
				buckets[i].Items++
				buckets[i].Pages += si.Pages
				break
			}
		}
	}
	return lo.Filter(buckets, func(b PageBucket, _ int) bool { return b.Items > 0 })
}

// SecondsPerPage is the coarse per-page docling cost used only to
// render a rough ETA under --plan. It never schedules work and is never
// used as a timeout; values are order-of-magnitude, not measurements of
// any particular machine.
func SecondsPerPage(device string) float64 {
	switch ResolveDevice(device) {
	case "cpu":
		return 4.0
	case "cuda":
		return 0.8
	default: // mps
		return 1.5
	}
}

// EstimateDuration converts a total page count into a rough wall-clock
// estimate at the given parallelism and device rate. jobs ≤ 1 means a
// single docling process.
func EstimateDuration(totalPages, jobs int, device string) time.Duration {
	return EstimateDurationAt(totalPages, jobs, SecondsPerPage(device))
}
