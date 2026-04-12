package hygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sciminds/cli/internal/zot/local"
)

// OrphanKind names a sub-check within the Orphans umbrella. Findings
// carry the kind as Finding.Kind so the renderer can group them.
type OrphanKind string

const (
	OrphanEmptyCollection      OrphanKind = "empty-collection"
	OrphanStandaloneAttachment OrphanKind = "standalone-attachment"
	OrphanStandaloneNote       OrphanKind = "standalone-note"
	OrphanUncollectedItem      OrphanKind = "uncollected-item"
	OrphanUnusedTag            OrphanKind = "unused-tag"
	OrphanMissingFile          OrphanKind = "missing-file"
)

// AllOrphanKinds lists every kind the parser recognizes. The *default*
// set (what `zot doctor orphans` with no --kind flag runs) is defaultOrphanKinds
// below, which excludes the noisy and expensive kinds.
var AllOrphanKinds = []OrphanKind{
	OrphanEmptyCollection,
	OrphanStandaloneAttachment,
	OrphanStandaloneNote,
	OrphanUncollectedItem,
	OrphanUnusedTag,
	OrphanMissingFile,
}

// defaultOrphanKinds is the set used when OrphansOptions.Kinds is nil.
// It excludes:
//   - OrphanUncollectedItem: noisy — users who don't organize by
//     collections end up with thousands of findings. Opt in with
//     --kind uncollected-item.
//   - OrphanMissingFile: expensive — requires filesystem stats. Even
//     when selected, runs only if OrphansOptions.CheckFiles is true.
var defaultOrphanKinds = []OrphanKind{
	OrphanEmptyCollection,
	OrphanStandaloneAttachment,
	OrphanStandaloneNote,
	OrphanUnusedTag,
}

// ParseOrphanKind maps a user-facing string to an OrphanKind.
func ParseOrphanKind(s string) (OrphanKind, error) {
	s = strings.TrimSpace(s)
	for _, k := range AllOrphanKinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf(
		"unknown orphan kind %q (want: %s)",
		s,
		"empty-collection, standalone-attachment, standalone-note, uncollected-item, unused-tag, missing-file",
	)
}

// OrphansOptions configures which sub-checks run and whether the
// filesystem-touching missing-file check is enabled.
type OrphansOptions struct {
	// Kinds is the subset of sub-checks to run. Empty = run all.
	Kinds []OrphanKind
	// CheckFiles controls the missing-file sub-check. Default off because
	// stat'ing every attachment on a network-mounted storage dir can be
	// slow. Has no effect when missing-file is not in the selected kinds.
	CheckFiles bool
	// DataDir is the Zotero data directory used to resolve attachment
	// paths for missing-file checking. Required when CheckFiles is true
	// AND missing-file is selected.
	DataDir string
}

// OrphansStats is the summary attached to Report.Stats for orphan runs.
// One entry per kind that was actually run, with the count of findings
// produced. Kinds not in the selected set are absent from the map.
type OrphansStats struct {
	CountsByKind map[string]int `json:"counts_by_kind"`
	Total        int            `json:"total"`
}

// severityForOrphan grades a sub-kind. Structural problems that indicate
// data loss are errors; standalone attachments are warnings (they're
// data the user can reattach); everything else is info.
func severityForOrphan(k OrphanKind) Severity {
	switch k {
	case OrphanMissingFile:
		return SevError
	case OrphanStandaloneAttachment:
		return SevWarn
	default:
		return SevInfo
	}
}

// orphanKindsSelected resolves the caller's selection into a set. Nil
// or empty input falls back to defaultOrphanKinds (not AllOrphanKinds —
// the default excludes noisy/expensive sub-checks).
func orphanKindsSelected(kinds []OrphanKind) map[OrphanKind]struct{} {
	out := map[OrphanKind]struct{}{}
	if len(kinds) == 0 {
		for _, k := range defaultOrphanKinds {
			out[k] = struct{}{}
		}
		return out
	}
	for _, k := range kinds {
		out[k] = struct{}{}
	}
	return out
}

// Orphans runs the configured sub-checks and returns a Report with one
// Finding per orphan record. Findings from different sub-kinds coexist
// in a single slice; renderers group by Finding.Kind.
func Orphans(db local.Reader, opts OrphansOptions) (*Report, error) {
	selected := orphanKindsSelected(opts.Kinds)
	counts := map[string]int{}
	var findings []Finding

	if _, ok := selected[OrphanEmptyCollection]; ok {
		cs, err := db.ScanEmptyCollections()
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			findings = append(findings, Finding{
				Check:    "orphans",
				Kind:     string(OrphanEmptyCollection),
				ItemKey:  c.Key,
				Title:    c.Name,
				Severity: severityForOrphan(OrphanEmptyCollection),
				Message:  "empty collection",
				Fixable:  true,
			})
		}
		counts[string(OrphanEmptyCollection)] = len(cs)
	}

	if _, ok := selected[OrphanStandaloneAttachment]; ok {
		as, err := db.ScanStandaloneAttachments()
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			msg := "standalone attachment"
			if a.Filename != "" {
				msg += ": " + a.Filename
			}
			findings = append(findings, Finding{
				Check:    "orphans",
				Kind:     string(OrphanStandaloneAttachment),
				ItemKey:  a.Key,
				Title:    a.Filename,
				Severity: severityForOrphan(OrphanStandaloneAttachment),
				Message:  msg,
				Fixable:  false,
			})
		}
		counts[string(OrphanStandaloneAttachment)] = len(as)
	}

	if _, ok := selected[OrphanStandaloneNote]; ok {
		ns, err := db.ScanStandaloneNotes()
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			title := n.Title
			if title == "" {
				title = "(untitled note)"
			}
			findings = append(findings, Finding{
				Check:    "orphans",
				Kind:     string(OrphanStandaloneNote),
				ItemKey:  n.Key,
				Title:    title,
				Severity: severityForOrphan(OrphanStandaloneNote),
				Message:  "standalone note",
				Fixable:  false,
			})
		}
		counts[string(OrphanStandaloneNote)] = len(ns)
	}

	if _, ok := selected[OrphanUncollectedItem]; ok {
		is, err := db.ScanUncollectedItems()
		if err != nil {
			return nil, err
		}
		for _, it := range is {
			findings = append(findings, Finding{
				Check:    "orphans",
				Kind:     string(OrphanUncollectedItem),
				ItemKey:  it.Key,
				Title:    it.Title,
				Severity: severityForOrphan(OrphanUncollectedItem),
				Message:  "item in zero collections",
				Fixable:  false,
			})
		}
		counts[string(OrphanUncollectedItem)] = len(is)
	}

	if _, ok := selected[OrphanUnusedTag]; ok {
		ts, err := db.ScanUnusedTags()
		if err != nil {
			return nil, err
		}
		for _, tg := range ts {
			findings = append(findings, Finding{
				Check:    "orphans",
				Kind:     string(OrphanUnusedTag),
				ItemKey:  "", // tags have no key
				Title:    tg.Name,
				Severity: severityForOrphan(OrphanUnusedTag),
				Message:  "unused tag",
				Fixable:  true,
			})
		}
		counts[string(OrphanUnusedTag)] = len(ts)
	}

	// missing-file is opt-in because it stats every imported attachment.
	if _, ok := selected[OrphanMissingFile]; ok && opts.CheckFiles {
		missing, err := scanMissingFiles(db, opts.DataDir)
		if err != nil {
			return nil, err
		}
		findings = append(findings, missing...)
		counts[string(OrphanMissingFile)] = len(missing)
	}

	// Stable sort: by kind first (so the renderer can group), then item key.
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].ItemKey < findings[j].ItemKey
	})

	total := 0
	for _, n := range counts {
		total += n
	}

	return &Report{
		Check:    "orphans",
		Scanned:  total,
		Findings: findings,
		Stats: OrphansStats{
			CountsByKind: counts,
			Total:        total,
		},
	}, nil
}

// scanMissingFiles stats the expected on-disk path for every imported
// attachment (linkMode 0 or 1) in the library. Findings are emitted for
// attachments whose file does not exist.
//
// Linked attachments (linkMode 2) are NOT checked — the path is user-
// managed and often on external storage we don't own. Linked URLs
// (linkMode 3) have no file.
func scanMissingFiles(db local.Reader, dataDir string) ([]Finding, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("missing-file check requires a data directory")
	}
	rows, err := db.ScanAttachmentFiles()
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range rows {
		if a.LinkMode != 0 && a.LinkMode != 1 {
			continue
		}
		if a.Filename == "" {
			continue
		}
		path := filepath.Join(dataDir, "storage", a.Key, a.Filename)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		out = append(out, Finding{
			Check:    "orphans",
			Kind:     string(OrphanMissingFile),
			ItemKey:  a.Key,
			Title:    a.Filename,
			Severity: severityForOrphan(OrphanMissingFile),
			Message:  "attachment file missing on disk",
			Fixable:  false,
		})
	}
	return out, nil
}
