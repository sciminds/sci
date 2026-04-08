package markdb

import (
	"fmt"
	"strings"
)

// IngestCmdResult implements cmdutil.Result for the ingest command.
type IngestCmdResult struct {
	Stats IngestStats `json:"stats"`
	DB    string      `json:"db"`
	Links struct {
		Resolved int `json:"resolved"`
		Broken   int `json:"broken"`
	} `json:"links"`
}

func (r IngestCmdResult) JSON() any { return r }

func (r IngestCmdResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", r.DB)
	fmt.Fprintf(&b, "  added: %d  updated: %d  removed: %d  skipped: %d",
		r.Stats.Added, r.Stats.Updated, r.Stats.Removed, r.Stats.Skipped)
	if r.Stats.Errors > 0 {
		fmt.Fprintf(&b, "  errors: %d", r.Stats.Errors)
	}
	fmt.Fprintf(&b, "\n  links: %d resolved, %d broken\n", r.Links.Resolved, r.Links.Broken)
	return b.String()
}

// SearchCmdResult implements cmdutil.Result for the search command.
type SearchCmdResult struct {
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`
}

func (r SearchCmdResult) JSON() any { return r }

func (r SearchCmdResult) Human() string {
	if len(r.Hits) == 0 {
		return "  no results\n"
	}
	var b strings.Builder
	for _, h := range r.Hits {
		fmt.Fprintf(&b, "  %s\n", h.Path)
		if h.Snippet != "" {
			fmt.Fprintf(&b, "    %s\n", h.Snippet)
		}
	}
	fmt.Fprintf(&b, "  %d result(s)\n", len(r.Hits))
	return b.String()
}

// InfoCmdResult implements cmdutil.Result for the info command.
type InfoCmdResult struct {
	Sources     int            `json:"sources"`
	Files       int            `json:"files"`
	Links       int            `json:"links"`
	BrokenLinks int            `json:"broken_links"`
	SchemaKeys  int            `json:"schema_keys"`
	ParseErrors int            `json:"parse_errors"`
	TypeCounts  map[string]int `json:"type_counts,omitempty"`
}

func (r InfoCmdResult) JSON() any { return r }

func (r InfoCmdResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  files: %d  sources: %d  schema keys: %d\n", r.Files, r.Sources, r.SchemaKeys)
	fmt.Fprintf(&b, "  links: %d  broken: %d\n", r.Links, r.BrokenLinks)
	if r.ParseErrors > 0 {
		fmt.Fprintf(&b, "  parse errors: %d\n", r.ParseErrors)
	}
	return b.String()
}

// Info queries summary statistics from the database.
// Errors are intentionally ignored: each count defaults to 0 when its table
// does not yet exist (e.g. freshly created database before first ingest).
func (s *Store) Info() (*InfoCmdResult, error) {
	r := &InfoCmdResult{}

	_ = s.db.QueryRow("SELECT COUNT(*) FROM _sources").Scan(&r.Sources)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&r.Files)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM links").Scan(&r.Links)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM links WHERE target_id IS NULL").Scan(&r.BrokenLinks)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM _schema").Scan(&r.SchemaKeys)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM files WHERE parse_error IS NOT NULL").Scan(&r.ParseErrors)

	return r, nil
}

// DiffCmdResult implements cmdutil.Result for the diff command.
type DiffCmdResult struct {
	Result DiffResult `json:"result"`
}

func (r DiffCmdResult) JSON() any { return r }

func (r DiffCmdResult) Human() string {
	total := len(r.Result.Added) + len(r.Result.Modified) + len(r.Result.Deleted)
	if total == 0 {
		return "  no changes\n"
	}
	var b strings.Builder
	for _, p := range r.Result.Added {
		fmt.Fprintf(&b, "  + %s\n", p)
	}
	for _, p := range r.Result.Modified {
		fmt.Fprintf(&b, "  ~ %s\n", p)
	}
	for _, p := range r.Result.Deleted {
		fmt.Fprintf(&b, "  - %s\n", p)
	}
	fmt.Fprintf(&b, "  %d change(s)\n", total)
	return b.String()
}

// ExportCmdResult implements cmdutil.Result for the export command.
type ExportCmdResult struct {
	Stats ExportStats `json:"stats"`
}

func (r ExportCmdResult) JSON() any { return r }

func (r ExportCmdResult) Human() string {
	return fmt.Sprintf("  exported %d file(s) to %s\n", r.Stats.Written, r.Stats.Dir)
}
