package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/extract"
)

// ExtractLibPlanResult is emitted by `zot extract-lib --plan`: a
// read-only description of the run a bare invocation (or --apply) would
// perform. Deliberately NOT a mode flag on ExtractLibResult — that
// shape is a completed-run contract and stays byte-stable (pinned by
// TestExtractLibResult_JSONKeysAreFrozen).
type ExtractLibPlanResult struct {
	Mode      string `json:"mode"`  // always "plan"
	Apply     bool   `json:"apply"` // always false — one field to branch on
	Device    string `json:"device"`
	Jobs      int    `json:"jobs"`
	LayoutDir string `json:"layout_dir,omitempty"`

	Candidates      int  `json:"candidates"`
	AlreadyDone     int  `json:"already_done"`
	LayoutDone      *int `json:"layout_done"` // null in classic mode
	Cached          int  `json:"cached"`
	NeedsExtraction int  `json:"needs_extraction"`
	ArtifactsOnly   int  `json:"artifacts_only"`
	Skipped         int  `json:"skipped"`
	PlanErrors      int  `json:"plan_errors"`
	NoteTooLong     int  `json:"note_too_long"`

	Limit     int `json:"limit"`
	Selected  int `json:"selected"`
	Remaining int `json:"remaining"`

	WouldInvalidateCache int `json:"would_invalidate_cache,omitempty"`
	WouldClearLayoutDone int `json:"would_clear_layout_done,omitempty"`

	Pages *PlanPages `json:"pages"` // null when the estimator is unavailable
	ETA   *PlanETA   `json:"eta"`   // null when pages is null or nothing is queued

	Duplicates      []extract.DuplicateGroup `json:"duplicates,omitempty"`
	DuplicateGroups int                      `json:"duplicate_groups"`
	DuplicateWasted int                      `json:"duplicate_wasted_extractions"`

	Errors map[string]string `json:"errors,omitempty"` // parentKey → plan error
}

// PlanPages summarizes the queued documents' page counts.
type PlanPages struct {
	Known        int                  `json:"known"`   // items with a parsed count
	Unknown      int                  `json:"unknown"` // items the parser couldn't read
	Total        int                  `json:"total"`   // pages (extrapolated when Unknown > 0)
	Extrapolated bool                 `json:"extrapolated"`
	Buckets      []extract.PageBucket `json:"buckets"`
}

// PlanETA is the rough wall-clock estimate for the queued extractions.
// Basis always states the assumption so nobody mistakes a guess for a
// measurement — and, when the rate came from the layout corpus, says how
// many documents it was measured over.
type PlanETA struct {
	Seconds int    `json:"seconds"`
	Human   string `json:"human"`
	Jobs    int    `json:"jobs"`
	Basis   string `json:"basis"`
	// SecondsPerPage is the rate used. CalibratedFrom is the number of
	// finished extractions it was measured over, omitted entirely when
	// the rate is the device guess.
	SecondsPerPage float64 `json:"seconds_per_page"`
	CalibratedFrom int     `json:"calibrated_from,omitempty"`
}

// NewExtractLibPlanResult projects a Survey into the --plan result
// shape. device/jobs are the values the eventual run would use.
func NewExtractLibPlanResult(s extract.Survey, device string, jobs int, layoutDir string) ExtractLibPlanResult {
	r := ExtractLibPlanResult{
		Mode:                 "plan",
		Device:               device,
		Jobs:                 jobs,
		LayoutDir:            layoutDir,
		Candidates:           s.Candidates,
		AlreadyDone:          s.AlreadyDone,
		LayoutDone:           s.LayoutDone,
		Cached:               s.Cached,
		NeedsExtraction:      s.NeedsExtraction,
		ArtifactsOnly:        s.ArtifactsOnly,
		Skipped:              s.Skipped,
		PlanErrors:           s.PlanErrors,
		NoteTooLong:          s.NoteTooLong,
		Limit:                s.Limit,
		Selected:             len(s.Selected),
		Remaining:            s.Remaining,
		WouldInvalidateCache: s.WouldInvalidateCache,
		WouldClearLayoutDone: s.WouldClearLayoutDone,
		Duplicates:           s.Duplicates,
		DuplicateGroups:      len(s.Duplicates),
		DuplicateWasted:      s.DuplicateWasted,
		Errors:               s.Errors,
	}
	if s.PagesKnownItems > 0 || s.PagesUnknownItems > 0 {
		r.Pages = &PlanPages{
			Known:        s.PagesKnownItems,
			Unknown:      s.PagesUnknownItems,
			Total:        s.PagesTotal,
			Extrapolated: s.Extrapolated,
			Buckets:      s.Buckets,
		}
	}
	if r.Pages != nil && s.ETA > 0 {
		// Name the rate's provenance in the basis line: a median over the
		// user's own extractions and a hardcoded per-device guess deserve
		// very different amounts of trust.
		rateSource := fmt.Sprintf("(%s guess)", device)
		if s.CalibrationSamples > 0 {
			rateSource = fmt.Sprintf("(median of %d extractions)", s.CalibrationSamples)
		}
		r.ETA = &PlanETA{
			Seconds:        int(s.ETA.Seconds()),
			Human:          s.ETA.Truncate(time.Minute).String(),
			Jobs:           max(jobs, 1),
			SecondsPerPage: s.SecondsPerPage,
			CalibratedFrom: s.CalibrationSamples,
			Basis: fmt.Sprintf("%.1fs/page %s × %d pages ÷ %d jobs — rough",
				s.SecondsPerPage, rateSource, s.PagesTotal, max(jobs, 1)),
		}
	}
	return r
}

// JSON implements cmdutil.Result.
func (r ExtractLibPlanResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ExtractLibPlanResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s extract-lib plan — nothing will be written\n", uikit.SymArrow)
	if r.Candidates == 0 {
		fmt.Fprintf(&b, "      no items with PDF attachments found\n")
		return b.String()
	}
	fmt.Fprintf(&b, "      candidates:     %d item(s) with a PDF attachment\n", r.Candidates)
	fmt.Fprintf(&b, "      already done:   %d\n", r.AlreadyDone)
	if r.LayoutDone != nil {
		fmt.Fprintf(&b, "      layout done:    %d key dir(s) complete\n", *r.LayoutDone)
	}
	if r.Cached > 0 {
		fmt.Fprintf(&b, "      cached:         %d (markdown on disk; only --apply would post these)\n", r.Cached)
	}
	fmt.Fprintf(&b, "      to extract:     %d\n", r.NeedsExtraction)
	if r.ArtifactsOnly > 0 {
		fmt.Fprintf(&b, "      artifacts only: %d (note exists, key dir missing)\n", r.ArtifactsOnly)
	}
	if r.Skipped > 0 {
		fmt.Fprintf(&b, "      skipped:        %d\n", r.Skipped)
	}
	if r.NoteTooLong > 0 {
		fmt.Fprintf(&b, "      note too long:  %d (Zotero rejected these — not retried; --reextract to force)\n", r.NoteTooLong)
	}
	if r.Limit > 0 {
		fmt.Fprintf(&b, "      selected:       %d (--limit %d; %d remaining)\n", r.Selected, r.Limit, r.Remaining)
	}
	if r.WouldInvalidateCache > 0 || r.WouldClearLayoutDone > 0 {
		fmt.Fprintf(&b, "      --reextract would invalidate %d cache entr(ies) and %d .done marker(s)\n",
			r.WouldInvalidateCache, r.WouldClearLayoutDone)
	}

	if r.Pages == nil {
		fmt.Fprintf(&b, "      pages:          unavailable (page-count estimator not wired) — no ETA\n")
	} else {
		extrapolated := ""
		if r.Pages.Extrapolated {
			extrapolated = fmt.Sprintf(" (%d unknown, extrapolated)", r.Pages.Unknown)
		}
		fmt.Fprintf(&b, "      pages:          %d across %d item(s)%s\n", r.Pages.Total, r.Pages.Known, extrapolated)
		for _, bk := range r.Pages.Buckets {
			fmt.Fprintf(&b, "        %-9s %4d item(s) %6d pages\n", bk.Label, bk.Items, bk.Pages)
		}
		if r.ETA != nil {
			fmt.Fprintf(&b, "      rough ETA:      %s at %d job(s) — %s\n", r.ETA.Human, r.ETA.Jobs, r.ETA.Basis)
		}
	}

	if len(r.Duplicates) > 0 {
		fmt.Fprintf(&b, "\n  duplicate PDF content (%d group(s)):\n", len(r.Duplicates))
		for _, g := range r.Duplicates {
			pages := ""
			if g.Pages > 0 {
				pages = fmt.Sprintf("%d pages · ", g.Pages)
			}
			fmt.Fprintf(&b, "      %s%d item(s), %d queued this run\n", pages, len(g.Members), g.Queued)
			for _, m := range g.Members {
				fmt.Fprintf(&b, "        %s  %-30s %s\n", m.ParentKey, m.PDFName, m.Disposition)
			}
		}
		fmt.Fprintf(&b, "      sci extracts each item separately — layout artifacts are key-named,\n")
		fmt.Fprintf(&b, "      so one extraction cannot be posted to two parents.\n")
	}

	if r.LayoutDir != "" {
		fmt.Fprintf(&b, "\n      artifacts → %s\n", r.LayoutDir)
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "      %s %d plan error(s):\n", uikit.SymFail, len(r.Errors))
		for _, k := range slices.Sorted(maps.Keys(r.Errors)) {
			fmt.Fprintf(&b, "        %s: %s\n", k, r.Errors[k])
		}
	}
	fmt.Fprintf(&b, "\n      run without --plan to extract into the cache, or add --apply to post notes\n")
	return b.String()
}

// Warnings implements cmdutil.Warner: duplicate PDF content is a
// data-quality caveat an agent should act on before trusting the plan,
// and recorded note-length rejections explain why some candidates are
// permanently excluded from the run.
func (r ExtractLibPlanResult) Warnings() []cmdutil.Warning {
	var out []cmdutil.Warning
	if len(r.Duplicates) > 0 {
		out = append(out, cmdutil.Warning{
			Code: cmdutil.CodeDuplicate,
			Message: fmt.Sprintf("%d PDF(s) are attached to more than one item — %d extraction(s) would run on bytes already queued under another key",
				r.DuplicateGroups, r.DuplicateWasted),
			Fix: "sci zot doctor duplicates",
		})
	}
	if r.NoteTooLong > 0 {
		out = append(out, NoteTooLongWarning(r.NoteTooLong))
	}
	return out
}

// NoteTooLongWarning is the shared envelope warning for extractions
// whose note Zotero rejected for length — emitted by both the --plan
// result and the live-run result so the two speak the same vocabulary.
// The completed-run JSON shape is frozen (TestExtractLibResult_JSONKeysAreFrozen),
// so the live run carries this fact in the warning channel only.
func NoteTooLongWarning(n int) cmdutil.Warning {
	return cmdutil.Warning{
		Code: cmdutil.CodeRuntime,
		Message: fmt.Sprintf("%d extraction note(s) exceed Zotero's note length limit — skipped, not retried (a server-side limit; the markdown stays available locally, and --reextract clears the verdict)",
			n),
	}
}
