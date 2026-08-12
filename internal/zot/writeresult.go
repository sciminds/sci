package zot

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sciminds/sci/internal/uikit"
)

// WriteResult is the standard return type for write commands. Action is
// a short verb ("created", "updated", "trashed", …) and Target is the key
// or name of the affected entity.
//
// Data optionally carries the full hydrated entity (local.Item for items,
// local.Collection for collections) so callers — especially LLM agents
// consuming --json — can see what was just written without a second
// round-trip against the (possibly unsynced) local SQLite.
type WriteResult struct {
	Action  string `json:"action"`
	Kind    string `json:"kind"`   // "item" | "collection" | "tag"
	Target  string `json:"target"` // key or name
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

// ItemTypeChange reports an `item update --type` in the write's own result.
//
// Zotero removes every field the new type does not declare, and it does so
// silently — the PATCH answers 204 whether it kept the venue or discarded
// it. A caller repairing metadata needs to know which fields went, so
// DroppedFields is a diff of the item before and after the write rather
// than a restatement of what was asked for.
//
// An empty DroppedFields on a real type change is a meaningful answer: the
// new type declared everything the item carried.
type ItemTypeChange struct {
	From          string   `json:"from"`
	To            string   `json:"to"`
	DroppedFields []string `json:"dropped_fields"`
}

// ItemUpdateData is the `data` an `item update` returns when it did
// something the caller cannot see in the fields they named.
type ItemUpdateData struct {
	TypeChange       *ItemTypeChange `json:"type_change,omitempty"`
	CreatorsReplaced int             `json:"creators_replaced,omitempty"`
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
	Data    any               `json:"data,omitempty"`
}

// JSON implements cmdutil.Result.
func (r BulkWriteResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r BulkWriteResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s %d/%d %s(s)\n",
		uikit.SymOK, r.Action, len(r.Success), r.Total, r.Kind)
	if len(r.Failed) > 0 {
		keys := slices.Sorted(maps.Keys(r.Failed))
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s %s: %s\n", uikit.SymFail, k, r.Failed[k])
		}
	}
	return b.String()
}
