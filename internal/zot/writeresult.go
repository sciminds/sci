package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
)

// WriteResult is what a single write verb returns. Action is a short past
// participle ("created", "updated", "trashed", "added"), Kind names the entity
// ("item", "collection", "tag") and Target is its key or name.
//
// Data carries the entity as the SERVER materialized it — the write's own
// answer, not a re-read. The local mirror lags until Zotero desktop syncs, so
// a caller (an agent under --json above all) that wants to see what it just
// wrote has nowhere else to look.
type WriteResult struct {
	Action  string `json:"action"`
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// JSON implements cmdutil.Result.
func (r WriteResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r WriteResult) Human() string {
	msg := r.Message
	if msg == "" {
		msg = fmt.Sprintf("%s %s %s", r.Action, r.Kind, r.Target)
	}
	return fmt.Sprintf("  %s %s\n", uikit.SymOK, msg)
}

// BulkWriteResult reports per-target outcomes for a batch write. Success
// holds the targets that applied cleanly; Failed maps target → error message
// for the rest. A batch that lost some targets is still a success as far as
// the exit code goes — the per-target report is the answer, and the caller
// decides what a partial batch means.
type BulkWriteResult struct {
	Action  string            `json:"action"`
	Kind    string            `json:"kind"`
	Total   int               `json:"total"`
	Success []string          `json:"success"`
	Failed  map[string]string `json:"failed,omitempty"`
	Data    any               `json:"data,omitempty"`
}

// JSON implements cmdutil.Result.
func (r BulkWriteResult) JSON() any { return r }

// Human implements cmdutil.Result. A batch whose Data carries an
// `unchanged` list (targets that were already in the requested state) says
// how many, so "added 10/10" on ten already-filed items does not read as ten
// writes.
func (r BulkWriteResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s %d/%d %s(s)\n",
		uikit.SymOK, r.Action, len(r.Success), r.Total, r.Kind)
	if n := len(r.unchanged()); n > 0 {
		fmt.Fprintf(&b, "    %d already as requested (unchanged)\n", n)
	}
	for _, k := range slices.Sorted(maps.Keys(r.Failed)) {
		fmt.Fprintf(&b, "  %s %s: %s\n", uikit.SymFail, k, r.Failed[k])
	}
	return b.String()
}

// unchanged reads the `unchanged` list out of Data when a verb put one there.
func (r BulkWriteResult) unchanged() []string {
	data, ok := r.Data.(map[string]any)
	if !ok {
		return nil
	}
	keys, _ := data["unchanged"].([]string)
	return keys
}
