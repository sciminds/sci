package zot

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// ImportResult is the CLI return shape for `zot import`. Its Human() line
// matches the terse `✓ …` / `→ …` style used by WriteResult + ReadResult so
// the user sees a consistent feel across the command surface. JSON mode
// emits the full structure for LLM agents and scripts.
//
// The parent item's Zotero key is deliberately NOT included — desktop's
// connector getRecognizedItem endpoint doesn't expose it. Consumers that
// need the key can look it up with `zot search <title>` after the fact.
type ImportResult struct {
	Path       string `json:"path"`
	Recognized bool   `json:"recognized"`
	Title      string `json:"title,omitempty"`
	ItemType   string `json:"item_type,omitempty"`
	Message    string `json:"message,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ImportResult) JSON() any { return r }

// Human implements cmdutil.Result. Two forms depending on outcome:
//   - recognized: `  ✓ imported "<title>" (journalArticle)`
//   - other: `  → <message>` so the user sees exactly what desktop did (or didn't).
func (r ImportResult) Human() string {
	if r.Recognized {
		line := "imported"
		if r.Title != "" {
			line += fmt.Sprintf(" %q", r.Title)
		}
		if r.ItemType != "" {
			line += fmt.Sprintf(" (%s)", r.ItemType)
		}
		return fmt.Sprintf("  %s %s\n", uikit.SymOK, line)
	}
	msg := r.Message
	if msg == "" {
		msg = "imported (no details)"
	}
	return fmt.Sprintf("  %s %s\n", uikit.SymArrow, msg)
}

// ImportBatchItem is the per-file outcome inside a batch result. Mirrors
// the connector layer's ItemResult with stable JSON tags for agents and
// scripts.
type ImportBatchItem struct {
	Path       string `json:"path"`
	Recognized bool   `json:"recognized"`
	Title      string `json:"title,omitempty"`
	ItemType   string `json:"item_type,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ImportBatchResult is what `zot import` returns when given a directory or
// multiple files. Counters are in tension with Items (each Item maps to
// exactly one of Recognized / Imported / Failed); both are emitted so JSON
// consumers get the summary without having to re-tally.
//
// Skipped is the count of non-PDF files encountered during the directory
// walk (always 0 for explicit file lists). Surfaced in the human summary
// so the user can sanity-check that the walk skipped what they expected.
type ImportBatchResult struct {
	Items      []ImportBatchItem `json:"items"`
	Total      int               `json:"total"`
	Recognized int               `json:"recognized"`
	Imported   int               `json:"imported"`
	Failed     int               `json:"failed"`
	Skipped    int               `json:"skipped_non_pdf"`
	Duration   string            `json:"duration"`
}

// JSON implements cmdutil.Result.
func (r ImportBatchResult) JSON() any { return r }

// maxBatchFailureLines caps how many failed-item lines Human() prints
// before collapsing the rest into "+N more". Full per-item detail is
// always available via --json.
const maxBatchFailureLines = 10

// Human implements cmdutil.Result. Layout:
//
//	✓ imported 47/50 PDFs in 4m12s — 38 recognized, 9 not recognized, 3 failed
//	  (skipped 7 non-PDF file(s) during walk)
//	✗ Smith2022.pdf — recognition timed out
//	✗ Jones2021.pdf — upload: status 500
//	✗ … +1 more (see --json for the full list)
//
// Failure lines are capped at maxBatchFailureLines so a 500-PDF run with
// 200 fails doesn't drown the terminal. The cap is a render-only choice;
// JSON output always carries every Item.
func (r ImportBatchResult) Human() string {
	var b strings.Builder

	successful := r.Recognized + r.Imported
	sym := uikit.SymOK
	if successful == 0 && r.Failed > 0 {
		sym = uikit.SymWarn
	}
	fmt.Fprintf(&b, "  %s imported %d/%d PDFs in %s — %d recognized, %d not recognized, %d failed\n",
		sym, successful, r.Total, r.Duration, r.Recognized, r.Imported, r.Failed)

	if r.Skipped > 0 {
		fmt.Fprintf(&b, "    (skipped %d non-PDF file(s) during walk)\n", r.Skipped)
	}

	// Failure detail lines, capped to keep terminal output bounded.
	failures := 0
	for _, it := range r.Items {
		if it.Error == "" {
			continue
		}
		if failures >= maxBatchFailureLines {
			remaining := r.Failed - failures
			fmt.Fprintf(&b, "  %s … +%d more (see --json for the full list)\n", uikit.SymArrow, remaining)
			break
		}
		fmt.Fprintf(&b, "  %s %s — %s\n", uikit.SymFail, filepath.Base(it.Path), it.Error)
		failures++
	}

	return b.String()
}
