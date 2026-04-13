package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/zot/extract"
)

// ExtractPlanResult describes the dry-run preview of `zot item extract`:
// what would happen if `--apply` were set. No docling invocation, no
// API write.
type ExtractPlanResult struct {
	ParentKey string `json:"parent_key"`
	PDFKey    string `json:"pdf_key"`
	PDFName   string `json:"pdf_name"`
	PDFHash   string `json:"pdf_hash"`
	Action    string `json:"action"` // "create" | "skip"
	Reason    string `json:"reason"`
	OutputDir string `json:"output_dir,omitempty"` // set only when --out was passed
	FullMode  bool   `json:"full_mode"`            // true when --out was passed
}

// JSON implements cmdutil.Result.
func (r ExtractPlanResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ExtractPlanResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s would %s note for %s\n", ui.SymArrow, r.Action, r.PDFName)
	fmt.Fprintf(&b, "      parent:   %s\n", r.ParentKey)
	fmt.Fprintf(&b, "      pdf:      %s (sha256:%s)\n", r.PDFKey, r.PDFHash)
	fmt.Fprintf(&b, "      reason:   %s\n", r.Reason)
	if r.FullMode {
		fmt.Fprintf(&b, "      mode:     full extraction (md + json + images + csv tables)\n")
		fmt.Fprintf(&b, "      out:      %s\n", r.OutputDir)
	} else {
		fmt.Fprintf(&b, "      mode:     zotero (markdown note, temp dir)\n")
	}
	fmt.Fprintln(&b, "      run with --apply to execute")
	return b.String()
}

// ExtractApplyResult describes the outcome of a completed extract action.
type ExtractApplyResult struct {
	ParentKey   string        `json:"parent_key"`
	PDFKey      string        `json:"pdf_key"`
	PDFName     string        `json:"pdf_name"`
	Action      string        `json:"action"`
	Reason      string        `json:"reason"`
	NoteKey     string        `json:"note_key"`
	ToolVersion string        `json:"tool_version,omitempty"`
	Duration    time.Duration `json:"duration_ns,omitempty"`

	// Populated only in full mode (--out passed).
	OutputDir string   `json:"output_dir,omitempty"`
	Markdown  string   `json:"markdown,omitempty"`
	JSONDoc   string   `json:"json,omitempty"`
	Images    []string `json:"images,omitempty"`
	Tables    []string `json:"tables,omitempty"`
}

// JSON implements cmdutil.Result.
func (r ExtractApplyResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ExtractApplyResult) Human() string {
	var b strings.Builder
	switch r.Action {
	case string(actionSkip):
		fmt.Fprintf(&b, "  %s skipped %s — %s\n", ui.SymArrow, r.PDFName, r.Reason)
		return b.String()
	case string(actionCreate):
		fmt.Fprintf(&b, "  %s created note %s for %s\n", ui.SymOK, r.NoteKey, r.PDFName)
	default:
		fmt.Fprintf(&b, "  %s %s %s (%s)\n", ui.SymOK, r.Action, r.NoteKey, r.PDFName)
	}
	if r.ToolVersion != "" && r.Duration > 0 {
		fmt.Fprintf(&b, "      %s in %s\n", r.ToolVersion, r.Duration.Truncate(time.Second))
	}
	if r.OutputDir != "" {
		fmt.Fprintf(&b, "      artifacts: %s\n", r.OutputDir)
		if r.Markdown != "" {
			fmt.Fprintf(&b, "        md:     %s\n", r.Markdown)
		}
		if r.JSONDoc != "" {
			fmt.Fprintf(&b, "        json:   %s\n", r.JSONDoc)
		}
		if len(r.Images) > 0 {
			fmt.Fprintf(&b, "        images: %d PNG(s)\n", len(r.Images))
		}
		if len(r.Tables) > 0 {
			fmt.Fprintf(&b, "        tables: %d CSV(s)\n", len(r.Tables))
		}
	}
	return b.String()
}

// ExtractArtifactResult is what `zot item extract ... --out DIR --no-note`
// emits: the full docling output without any Zotero note creation.
type ExtractArtifactResult struct {
	ParentKey   string        `json:"parent_key"`
	PDFKey      string        `json:"pdf_key"`
	PDFName     string        `json:"pdf_name"`
	OutputDir   string        `json:"output_dir"`
	Markdown    string        `json:"markdown"`
	JSONDoc     string        `json:"json,omitempty"`
	Images      []string      `json:"images,omitempty"`
	Tables      []string      `json:"tables,omitempty"`
	ToolVersion string        `json:"tool_version"`
	Duration    time.Duration `json:"duration_ns"`
}

// JSON implements cmdutil.Result.
func (r ExtractArtifactResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ExtractArtifactResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s extracted %s → %s\n", ui.SymOK, r.PDFName, r.OutputDir)
	fmt.Fprintf(&b, "      md:     %s\n", r.Markdown)
	if r.JSONDoc != "" {
		fmt.Fprintf(&b, "      json:   %s\n", r.JSONDoc)
	}
	if len(r.Images) > 0 {
		sorted := slices.Clone(r.Images)
		slices.Sort(sorted)
		fmt.Fprintf(&b, "      images: %d PNG(s)\n", len(sorted))
	}
	if len(r.Tables) > 0 {
		fmt.Fprintf(&b, "      tables: %d CSV(s)\n", len(r.Tables))
	}
	fmt.Fprintf(&b, "      %s in %s\n", r.ToolVersion, r.Duration.Truncate(time.Second))
	return b.String()
}

// ExtractLibResult is emitted by `zot extract-lib` — the aggregate
// outcome of a bulk extraction run.
type ExtractLibResult struct {
	Total    int               `json:"total"`
	Created  int               `json:"created"`
	Skipped  int               `json:"skipped"`
	Cached   int               `json:"cached"`
	Failed   int               `json:"failed"`
	Errors   map[string]string `json:"errors,omitempty"` // parentKey → error
	Duration time.Duration     `json:"duration_ns"`
}

// JSON implements cmdutil.Result.
func (r ExtractLibResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r ExtractLibResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s extract-lib complete\n", ui.SymOK)
	fmt.Fprintf(&b, "      total:    %d\n", r.Total)
	fmt.Fprintf(&b, "      created:  %d\n", r.Created)
	fmt.Fprintf(&b, "      skipped:  %d (docling note exists)\n", r.Skipped)
	if r.Cached > 0 {
		fmt.Fprintf(&b, "      cached:   %d (docling skipped, note re-posted)\n", r.Cached)
	}
	if r.Failed > 0 {
		fmt.Fprintf(&b, "      %s failed:  %d\n", ui.SymFail, r.Failed)
	}
	if len(r.Errors) > 0 {
		keys := slices.Sorted(maps.Keys(r.Errors))
		for _, k := range keys {
			fmt.Fprintf(&b, "      %s %s: %s\n", ui.SymFail, k, r.Errors[k])
		}
	}
	fmt.Fprintf(&b, "      duration: %s\n", r.Duration.Truncate(time.Second))
	return b.String()
}

// Convenience string aliases so the CLI layer can set Action without
// importing extract just for its enum labels.
type actionLabel string

const (
	actionCreate actionLabel = "create"
	actionSkip   actionLabel = "skip"
)

// ActionLabel maps an extract.Action enum to its stable string name
// used in result JSON. Defined here so the zot package owns the
// user-visible vocabulary.
func ActionLabel(a extract.Action) string {
	switch a {
	case extract.ActionCreate:
		return string(actionCreate)
	case extract.ActionSkip:
		return string(actionSkip)
	}
	return "unknown"
}
