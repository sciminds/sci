package xrcache

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/pkg/crossref"
)

// WorkFetcher is what the by-DOI sweep needs from a Crossref client.
type WorkFetcher interface {
	Work(ctx context.Context, doi string) (*crossref.WorkRecord, error)
}

// WorksStats is what a by-DOI sweep did.
type WorksStats struct {
	DOIsAsked   int `json:"dois_asked"`
	DOIsFetched int `json:"dois_fetched"`
	// DOIsAbsent counts DOIs Crossref answered with a 404. A FINDING, not
	// a failure: DataCite-registered DOIs (arXiv, OSF) are structurally
	// absent from Crossref, and recording them is what stops a delta from
	// re-asking forever.
	DOIsAbsent  int `json:"dois_absent"`
	DOIsErrored int `json:"dois_errored"`
	// DOIsSkipped counts DOIs the caller excluded before the sweep —
	// already in the base, or on the known-absent list.
	DOIsSkipped int `json:"dois_skipped"`
	Requests    int `json:"requests"`
}

// WorksResult is a completed by-DOI sweep.
type WorksResult struct {
	Records []crossref.WorkRecord
	// Absent lists DOIs Crossref has no record for, by name, so the
	// sidecar can carry them into the next delta's skip set.
	Absent []string
	// Errored lists DOIs whose lookup failed after every retry — kept
	// apart from Absent so a flaky network is never recorded as evidence
	// that Crossref lacks a record.
	Errored []string
	Stats   WorksStats
}

// NormalizeDOI folds Zotero-side spelling noise — doi.org URL wrappers,
// a doi: prefix, whitespace, case — into the bare lowercase form that is
// both Crossref's own spelling and this cache's row identity.
func NormalizeDOI(raw string) string {
	s := strings.TrimSpace(raw)
	lower := strings.ToLower(s)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		if strings.HasPrefix(lower, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// FetchWorks asks Crossref for every distinct DOI, by DOI.
//
// Like Fetch, a DOI Crossref cannot answer is a result, not an error —
// see [WorksResult.Absent] and [WorksResult.Errored]. An error here means
// the sweep itself could not run.
func FetchWorks(ctx context.Context, c WorkFetcher, dois []string, opts Options) (WorksResult, error) {
	var res WorksResult

	seen := map[string]bool{}
	uniq := make([]string, 0, len(dois))
	for _, d := range dois {
		n := NormalizeDOI(d)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		uniq = append(uniq, n)
	}

	for i, doi := range uniq {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Stats.DOIsAsked++

		var (
			rec *crossref.WorkRecord
			err error
		)
		for attempt := 0; attempt <= opts.Retries; attempt++ {
			if attempt > 0 && opts.Pause > 0 {
				time.Sleep(opts.Pause)
			}
			rec, err = c.Work(ctx, doi)
			res.Stats.Requests++
			if err == nil {
				break
			}
		}
		switch {
		case err != nil:
			res.Stats.DOIsErrored++
			res.Errored = append(res.Errored, doi)
		case rec == nil:
			res.Stats.DOIsAbsent++
			res.Absent = append(res.Absent, doi)
		default:
			res.Stats.DOIsFetched++
			res.Records = append(res.Records, *rec)
		}
		if opts.Progress != nil {
			opts.Progress(i+1, len(uniq))
		}
		if opts.Pause > 0 {
			time.Sleep(opts.Pause)
		}
	}
	return res, nil
}

// WorksFile is the by-DOI cache's name inside a staging directory.
const WorksFile = "crossref-works.ndjson"

// WorksMeta is the sidecar written beside the works body. Absent is the
// known-absent skip set: DOIs Crossref 404'd, carried forward so a delta
// never re-asks them.
type WorksMeta struct {
	ProducedAt   string     `json:"produced_at"`
	ProducedBy   string     `json:"produced_by"`
	Scope        string     `json:"scope"`
	RecordsTotal int        `json:"records_total"`
	Stats        WorksStats `json:"stats"`
	Absent       []string   `json:"absent,omitempty"`
	Errored      []string   `json:"errored,omitempty"`
	SHA256       string     `json:"sha256"`
}

// ReadWorksBase reads an existing works cache for a delta: the body's raw
// lines, the DOIs they hold, and the sidecar's known-absent set. A body
// with no sidecar is a partial write and returns an empty base — seeding
// a delta from bytes nothing vouches for would make the tear permanent.
func ReadWorksBase(dir string) (lines [][]byte, dois map[string]bool, absent map[string]bool, err error) {
	dois, absent = map[string]bool{}, map[string]bool{}
	body := filepath.Join(dir, WorksFile)

	metaRaw, err := os.ReadFile(body + ".meta.json") //nolint:gosec // staging path the caller owns
	if os.IsNotExist(err) {
		return nil, dois, absent, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	var meta WorksMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, nil, nil, fmt.Errorf("works sidecar unreadable: %w", err)
	}

	raw, err := os.ReadFile(body) //nolint:gosec // staging path the caller owns
	if os.IsNotExist(err) {
		return nil, map[string]bool{}, map[string]bool{}, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}

	// Base lines are kept as RAW BYTES, never decoded and re-encoded:
	// Crossref fields our model doesn't carry must survive a delta.
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var probe struct {
			DOI string `json:"doi"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, nil, nil, fmt.Errorf("works cache line unreadable: %w", err)
		}
		lines = append(lines, bytes.Clone(line))
		dois[NormalizeDOI(probe.DOI)] = true
	}
	if err := sc.Err(); err != nil {
		return nil, nil, nil, err
	}
	for _, d := range meta.Absent {
		absent[NormalizeDOI(d)] = true
	}
	return lines, dois, absent, nil
}

// WriteWorks writes base lines then fresh records, then the sidecar —
// same body-then-sidecar order as Write, so a body with no matching
// sidecar is always readable as a partial write. absent is the FULL
// known-absent list to record (the caller merges the base's carry-over
// with this sweep's findings).
func WriteWorks(dir, scope string, base [][]byte, res WorksResult, absent []string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body := filepath.Join(dir, WorksFile)

	f, err := os.Create(body) //nolint:gosec // path is the caller's own --out
	if err != nil {
		return "", err
	}
	for _, line := range base {
		if _, err := f.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			return "", err
		}
	}
	enc := json.NewEncoder(f)
	for i := range res.Records {
		if err := enc.Encode(res.Records[i]); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("encode work %s: %w", res.Records[i].DOI, err)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	sum, err := digest(body)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(WorksMeta{
		ProducedAt:   time.Now().UTC().Format(time.RFC3339),
		ProducedBy:   "sci zot crossref works",
		Scope:        scope,
		RecordsTotal: len(base) + len(res.Records),
		Stats:        res.Stats,
		Absent:       lo.Uniq(absent),
		Errored:      res.Errored,
		SHA256:       sum,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	//nolint:gosec // sidecar mirrors the body's perms
	if err := os.WriteFile(body+".meta.json", append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	return body, nil
}
