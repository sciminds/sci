package zot

import (
	"fmt"

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
