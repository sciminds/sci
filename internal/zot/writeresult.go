package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// WriteResult is the standard return type for write commands. Action is
// a short verb ("created", "updated", "trashed", …) and Target is the key
// or name of the affected entity.
type WriteResult struct {
	Action  string `json:"action"`
	Kind    string `json:"kind"`   // "item" | "collection" | "tag"
	Target  string `json:"target"` // key or name
	Message string `json:"message,omitempty"`
}

func (r WriteResult) JSON() any { return r }
func (r WriteResult) Human() string {
	msg := r.Message
	if msg == "" {
		msg = fmt.Sprintf("%s %s %s", r.Action, r.Kind, r.Target)
	}
	return fmt.Sprintf("  %s %s\n", ui.SymOK, msg)
}

// BulkWriteResult reports per-item outcomes for a batch write (e.g. bulk
// metadata update across many items). Success holds the keys that applied
// cleanly; Failed maps key → error message for the rest.
type BulkWriteResult struct {
	Action  string            `json:"action"`
	Kind    string            `json:"kind"`
	Total   int               `json:"total"`
	Success []string          `json:"success"`
	Failed  map[string]string `json:"failed,omitempty"`
}

func (r BulkWriteResult) JSON() any { return r }
func (r BulkWriteResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s %d/%d %s(s)\n",
		ui.SymOK, r.Action, len(r.Success), r.Total, r.Kind)
	if len(r.Failed) > 0 {
		keys := slices.Sorted(maps.Keys(r.Failed))
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s %s: %s\n", ui.SymFail, k, r.Failed[k])
		}
	}
	return b.String()
}
