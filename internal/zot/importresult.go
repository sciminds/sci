package zot

import (
	"fmt"

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
