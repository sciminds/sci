package zot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/citekey"
	"github.com/sciminds/cli/internal/zot/local"
)

// Keymap records the synthesized cite-key *prefix* (the part before
// `-ZOTKEY`) for each item, indexed by Zotero key. Only synthesized entries
// appear here — pinned keys are user-owned and not subject to drift.
//
// Persisted alongside a .bib export as `.zotero-citekeymap.json`. On the next
// export, ExportLibrary compares the freshly-synthesized prefix against the
// stored one and emits a biblatex `ids = {oldkey}` alias whenever they
// differ, so manuscripts that already cite the old form still resolve.
type Keymap map[string]string

// ExportStats summarizes a library-wide export.
type ExportStats struct {
	Total       int    `json:"total"`
	Pinned      int    `json:"pinned"`
	Synthesized int    `json:"synthesized"`
	Drifted     int    `json:"drifted"`
	Keymap      Keymap `json:"-"` // pass to SaveKeymap; not JSON-serialized inline
}

// ExportLibrary serializes a slice of items into a single document in the
// requested format. Pass the previous run's Keymap (or nil) for drift
// detection on synthesized entries.
//
// For BibTeX output:
//   - user-pinned cite-keys are emitted verbatim with a zotero:// URI
//     appended to the `note` field (preserving any existing user prose from
//     the Zotero `extra` field)
//   - synthesized cite-keys carry their 8-char Zotero key as a suffix for
//     uniqueness and round-trip recovery
//   - drifted synthesized prefixes get a biblatex `ids = {oldkey}` alias
func ExportLibrary(items []local.Item, format ExportFormat, prev Keymap) (string, ExportStats, error) {
	stats := ExportStats{Total: len(items), Keymap: Keymap{}}

	switch format {
	case ExportCSLJSON, "":
		body, err := exportCSLJSONLibrary(items, &stats)
		return body, stats, err
	case ExportBibTeX:
		body := exportBibTeXLibrary(items, prev, &stats)
		return body, stats, nil
	default:
		return "", stats, fmt.Errorf("unknown export format %q", format)
	}
}

func exportBibTeXLibrary(items []local.Item, prev Keymap, stats *ExportStats) string {
	var b strings.Builder
	for i := range items {
		it := &items[i]
		key, synth := citekey.Resolve(it)
		opts := bibEntryOpts{CiteKey: key}
		if synth {
			stats.Synthesized++
			prefix := strings.TrimSuffix(key, "-"+it.Key)
			stats.Keymap[it.Key] = prefix
			if old, ok := prev[it.Key]; ok && old != prefix {
				opts.IDsAlias = old + "-" + it.Key
				stats.Drifted++
			}
		} else {
			stats.Pinned++
			opts.ZoteroURI = zoteroSelectURI(it.Key)
		}
		b.WriteString(writeBibEntry(it, opts))
		b.WriteByte('\n')
	}
	return b.String()
}

func exportCSLJSONLibrary(items []local.Item, stats *ExportStats) (string, error) {
	out := lo.Map(items, func(it local.Item, _ int) cslItem {
		return buildCSLItem(&it)
	})
	stats.Synthesized = lo.CountBy(items, func(it local.Item) bool {
		_, synth := citekey.Resolve(&it)
		return synth
	})
	stats.Pinned = len(items) - stats.Synthesized
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// zoteroSelectURI returns the zotero:// deep-link for an item. Clickable in
// editors that recognize the scheme (JabRef, some VS Code extensions);
// inert string elsewhere. Used as a round-trip anchor for pinned entries.
func zoteroSelectURI(key string) string {
	return "zotero://select/library/items/" + key
}

// LoadKeymap reads a .zotero-citekeymap.json sidecar. A missing file is not
// an error — callers get an empty map and drift detection is simply skipped
// on the first export.
func LoadKeymap(path string) (Keymap, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Keymap{}, nil
		}
		return nil, err
	}
	var m Keymap
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = Keymap{}
	}
	return m, nil
}

// SaveKeymap writes the keymap to disk as indented JSON.
func SaveKeymap(path string, m Keymap) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
