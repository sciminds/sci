// Package xrcache sweeps Crossref for the library's DOI-less titles and
// writes the candidates into zot's staging directory.
//
// It is the second opinion in a two-source DOI check. OpenAlex resolves a
// DOI-less item by title and zot accepts the match at `medium` confidence;
// writing that inferred DOI back into Zotero would make it resolve at
// `high` on the next build, by a DOI zot itself guessed. Asking a second,
// independent index closes that loop: a DOI both indexes reach from the
// same title is evidence, and one index answering alone is not.
//
// Like oacache, this fetches and never decides. Crossref ranks fuzzily —
// its top hit for a real 1962 title is a differently-named dataset — so
// every candidate is kept, tagged with the query that found it, and the
// exact-title-and-year rule is applied downstream where title_norm lives.
package xrcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sciminds/cli/pkg/crossref"
)

// Searcher is what this package needs from a Crossref client.
type Searcher interface {
	Search(ctx context.Context, title string, rows int) ([]crossref.Record, error)
}

// Candidate is one Crossref record plus the query that produced it. The
// query title is the join key back to a library item, so it is part of the
// record rather than implied by position.
type Candidate struct {
	QueryTitle string `json:"query_title"`
	DOI        string `json:"doi"`
	Title      string `json:"title"`
	Venue      string `json:"venue"`
	Year       int    `json:"year"`
	Type       string `json:"type"`
	// Rank is Crossref's own ordering. Recorded, never obeyed: it is
	// useful for diagnosing a rule, and following it would reproduce
	// exactly the misidentification the rule exists to avoid.
	Rank int `json:"rank"`
}

// Stats is what a sweep did.
type Stats struct {
	TitlesQueried  int `json:"titles_queried"`
	TitlesWithHits int `json:"titles_with_hits"`
	// TitlesNoMatch counts titles Crossref answered with nothing. This is
	// a FINDING, not a failure: for a preprint or a 1947 paper, "Crossref
	// has no DOI" is usually the true answer and the reason not to write
	// one.
	TitlesNoMatch int `json:"titles_no_match"`
	// TitlesErrored counts titles that could not be asked at all, kept
	// apart from TitlesNoMatch so a flaky network can never be read as
	// evidence about a paper.
	TitlesErrored int `json:"titles_errored"`
	Candidates    int `json:"candidates"`
	Requests      int `json:"requests"`
}

// Result is a completed sweep.
type Result struct {
	Candidates []Candidate
	// Errored lists the titles whose lookup failed after every retry. It
	// is a list rather than a count because a consumer computing an
	// agreement rate must be able to exclude them from the denominator by
	// name, not just shrink it.
	Errored []string
	Stats   Stats
}

// Options tunes a sweep.
type Options struct {
	// Rows is candidates kept per title; zero means crossref.DefaultRows.
	Rows int
	// Retries is additional attempts after a failure. Zero means one try.
	Retries int
	// Pause is slept between requests. Crossref's polite pool is generous
	// but not unlimited, and a 1,300-title sweep is long enough to notice.
	Pause time.Duration
	// Progress, when set, is called after each title.
	Progress func(done, total int)
}

// Fetch sweeps every distinct title.
//
// It does not return an error for a title Crossref cannot answer — see
// [Result.Errored]. An error here means the sweep itself could not run.
func Fetch(ctx context.Context, c Searcher, titles []string, opts Options) (Result, error) {
	var res Result

	// Cross-library duplicates share a title, and asking twice would both
	// burn a request and duplicate every candidate downstream.
	seen := map[string]bool{}
	uniq := make([]string, 0, len(titles))
	for _, t := range titles {
		t = strings.TrimSpace(t)
		k := strings.ToLower(t)
		if t == "" || seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, t)
	}

	for i, title := range uniq {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Stats.TitlesQueried++

		var (
			out []crossref.Record
			err error
		)
		for attempt := 0; attempt <= opts.Retries; attempt++ {
			if attempt > 0 && opts.Pause > 0 {
				time.Sleep(opts.Pause)
			}
			out, err = c.Search(ctx, title, opts.Rows)
			res.Stats.Requests++
			if err == nil {
				break
			}
		}
		switch {
		case err != nil:
			res.Stats.TitlesErrored++
			res.Errored = append(res.Errored, title)
		case len(out) == 0:
			res.Stats.TitlesNoMatch++
		default:
			res.Stats.TitlesWithHits++
			for rank, r := range out {
				res.Candidates = append(res.Candidates, Candidate{
					QueryTitle: title, DOI: r.DOI, Title: r.Title,
					Venue: r.Venue, Year: r.Year, Type: r.Type, Rank: rank,
				})
			}
		}
		if opts.Progress != nil {
			opts.Progress(i+1, len(uniq))
		}
		if opts.Pause > 0 {
			time.Sleep(opts.Pause)
		}
	}
	res.Stats.Candidates = len(res.Candidates)
	return res, nil
}

// Meta is the sidecar written beside the body.
type Meta struct {
	ProducedAt   string   `json:"produced_at"`
	ProducedBy   string   `json:"produced_by"`
	Scope        string   `json:"scope"`
	RecordsTotal int      `json:"records_total"`
	Stats        Stats    `json:"stats"`
	Errored      []string `json:"errored,omitempty"`
	SHA256       string   `json:"sha256"`
}

// CacheFile is the body's name inside a staging directory.
const CacheFile = "crossref-candidates.ndjson"

// Write writes the body and then its sidecar.
//
// The order is the contract, identical to oacache.Write and `zot export`:
// the sidecar lands LAST and carries a digest of the bytes that actually
// arrived, so a consumer finding a body without a matching sidecar knows
// it caught a partial write rather than guessing.
func Write(dir, scope string, res Result) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body := filepath.Join(dir, CacheFile)

	f, err := os.Create(body) //nolint:gosec // path is the caller's own --out
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	for i := range res.Candidates {
		if err := enc.Encode(res.Candidates[i]); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("encode candidate %s: %w", res.Candidates[i].DOI, err)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	sum, err := digest(body)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(Meta{
		ProducedAt:   time.Now().UTC().Format(time.RFC3339),
		ProducedBy:   "sci zot crossref sync",
		Scope:        scope,
		RecordsTotal: len(res.Candidates),
		Stats:        res.Stats,
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

func digest(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is one we just wrote
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
