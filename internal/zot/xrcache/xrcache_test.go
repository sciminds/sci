package xrcache_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/xrcache"
	"github.com/sciminds/cli/pkg/crossref"
)

// fakeXR records what it was asked and answers from a script.
type fakeXR struct {
	calls []string
	byQ   map[string][]crossref.Record
	fail  map[string]error
	// failOnce records queries that fail exactly once, so a retry succeeds.
	failOnce map[string]int
}

func (f *fakeXR) Search(_ context.Context, title string, _ int) ([]crossref.Record, error) {
	f.calls = append(f.calls, title)
	if n, ok := f.failOnce[title]; ok && n > 0 {
		f.failOnce[title] = n - 1
		return nil, errors.New("503")
	}
	if err, ok := f.fail[title]; ok {
		return nil, err
	}
	return f.byQ[title], nil
}

func TestEveryCandidateIsTaggedWithTheTitleThatFoundIt(t *testing.T) {
	t.Parallel()
	f := &fakeXR{byQ: map[string][]crossref.Record{
		"Serial position": {{DOI: "10.1/a"}, {DOI: "10.1/b"}},
		"Gossip":          {{DOI: "10.2/c"}},
	}}
	res, err := xrcache.Fetch(context.Background(), f,
		[]string{"Serial position", "Gossip"}, xrcache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("kept %d candidates, want 3", len(res.Candidates))
	}
	// The join back to an item is by query title, so a candidate that has
	// lost which query produced it is unusable — and silently so, because
	// it still counts.
	for _, c := range res.Candidates {
		if c.QueryTitle == "" {
			t.Errorf("candidate %s carries no query title", c.DOI)
		}
	}
	if res.Stats.TitlesQueried != 2 || res.Stats.TitlesWithHits != 2 {
		t.Errorf("stats = %+v", res.Stats)
	}
}

func TestSilenceAndFailureAreDifferentAnswers(t *testing.T) {
	t.Parallel()
	f := &fakeXR{
		byQ:  map[string][]crossref.Record{"found": {{DOI: "10.1/a"}}},
		fail: map[string]error{"broken": errors.New("dial tcp: no route to host")},
	}
	res, err := xrcache.Fetch(context.Background(), f,
		[]string{"found", "absent", "broken"}, xrcache.Options{Retries: 1})
	if err != nil {
		t.Fatal(err)
	}
	// This is the whole discipline. "Crossref has no record" is EVIDENCE
	// -- it is what says a DOI should not be written for a 1947 paper.
	// "We could not ask" is the absence of evidence. Collapsing them lets
	// a flaky network quietly vote against writing a DOI, and the sweep
	// reports a precision number computed over questions never asked.
	if got := res.Stats.TitlesNoMatch; got != 1 {
		t.Errorf("no-match = %d, want just 'absent'", got)
	}
	if len(res.Errored) != 1 || res.Errored[0] != "broken" {
		t.Errorf("errored = %v, want just 'broken'", res.Errored)
	}
	if res.Stats.TitlesQueried != 3 {
		t.Errorf("queried = %d", res.Stats.TitlesQueried)
	}
}

func TestATransientFailureIsRetriedBeforeItCounts(t *testing.T) {
	t.Parallel()
	f := &fakeXR{
		byQ:      map[string][]crossref.Record{"flaky": {{DOI: "10.1/a"}}},
		failOnce: map[string]int{"flaky": 1},
	}
	res, err := xrcache.Fetch(context.Background(), f,
		[]string{"flaky"}, xrcache.Options{Retries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errored) != 0 {
		t.Errorf("a retried-and-recovered title was still reported errored: %v", res.Errored)
	}
	if len(res.Candidates) != 1 {
		t.Errorf("recovered title kept %d candidates", len(res.Candidates))
	}
}

func TestTitlesAreAskedOnceEach(t *testing.T) {
	t.Parallel()
	f := &fakeXR{byQ: map[string][]crossref.Record{"Dup": {{DOI: "10.1/a"}}}}
	// Cross-library duplicates share a title. Crossref is free, but a
	// library-wide sweep is still thousands of round trips, and asking the
	// same question twice also duplicates every candidate downstream.
	if _, err := xrcache.Fetch(context.Background(), f,
		[]string{"Dup", "Dup", "dup"}, xrcache.Options{}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Errorf("asked %d times: %v", len(f.calls), f.calls)
	}
}

func TestTheSidecarGoesLastAndCarriesTheDigest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res := xrcache.Result{
		Candidates: []xrcache.Candidate{{QueryTitle: "t", DOI: "10.1/a", Year: 1962}},
		Errored:    []string{"broken"},
	}
	res.Stats.TitlesQueried = 2
	body, err := xrcache.Write(dir, "all", res)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(body + ".meta.json")
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		RecordsTotal int      `json:"records_total"`
		SHA256       string   `json:"sha256"`
		Errored      []string `json:"errored"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.SHA256 == "" || meta.RecordsTotal != 1 {
		t.Errorf("meta = %+v", meta)
	}
	// Errored titles ride in the sidecar because a consumer computing an
	// agreement rate needs to know the denominator excluded them.
	if len(meta.Errored) != 1 {
		t.Errorf("sidecar dropped the errored titles: %+v", meta)
	}
	if filepath.Base(body) != xrcache.CacheFile {
		t.Errorf("body = %s", body)
	}
	lines, err := os.ReadFile(body)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(string(lines)), "\n") + 1; n != 1 {
		t.Errorf("body has %d lines, want one per candidate", n)
	}
}
