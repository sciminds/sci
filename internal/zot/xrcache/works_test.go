package xrcache

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/pkg/crossref"
)

// stubWorks answers Work() from a canned map; a nil entry is a 404.
type stubWorks struct {
	byDOI map[string]*crossref.WorkRecord
	errOn map[string]error
	calls []string
}

func (s *stubWorks) Work(_ context.Context, doi string) (*crossref.WorkRecord, error) {
	s.calls = append(s.calls, doi)
	if err, ok := s.errOn[doi]; ok {
		return nil, err
	}
	return s.byDOI[doi], nil
}

func TestFetchWorks_DedupesAndClassifies(t *testing.T) {
	t.Parallel()
	stub := &stubWorks{
		byDOI: map[string]*crossref.WorkRecord{
			"10.1098/rstb.2022.0048": {DOI: "10.1098/rstb.2022.0048", Type: "journal-article"},
			// "10.9999/nope" is absent (nil → 404)
		},
		errOn: map[string]error{"10.5555/boom": errors.New("transport")},
	}
	// The same DOI in three spellings must cost ONE request; the URL
	// wrapper and case are Zotero-side noise, not identity.
	dois := []string{
		"10.1098/RSTB.2022.0048",
		"https://doi.org/10.1098/rstb.2022.0048",
		" 10.1098/rstb.2022.0048 ",
		"10.9999/nope",
		"10.5555/boom",
	}
	res, err := FetchWorks(context.Background(), stub, dois, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.calls) != 3 {
		t.Errorf("requests = %v, want 3 distinct DOIs", stub.calls)
	}
	if len(res.Records) != 1 || res.Records[0].DOI != "10.1098/rstb.2022.0048" {
		t.Errorf("records = %+v", res.Records)
	}
	if len(res.Absent) != 1 || res.Absent[0] != "10.9999/nope" {
		t.Errorf("absent = %v — a 404 is a finding, recorded by name", res.Absent)
	}
	if len(res.Errored) != 1 || res.Errored[0] != "10.5555/boom" {
		t.Errorf("errored = %v — transport failures must never read as absence", res.Errored)
	}
	s := res.Stats
	if s.DOIsAsked != 3 || s.DOIsFetched != 1 || s.DOIsAbsent != 1 || s.DOIsErrored != 1 {
		t.Errorf("stats = %+v", s)
	}
}

func TestWriteWorksAndReadBase_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A base line carrying a field our model doesn't know — it must
	// survive a delta byte-for-byte (merge-never-narrow).
	baseLine := []byte(`{"doi":"10.1000/base","type":"journal-article","unmodelled_field":"survives"}`)

	res := WorksResult{
		Records: []crossref.WorkRecord{{DOI: "10.1000/new", Type: "posted-content"}},
	}
	res.Stats.DOIsFetched = 1
	body, err := WriteWorks(dir, "all", [][]byte{baseLine}, res, []string{"10.9999/nope"})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(body) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"unmodelled_field":"survives"`) {
		t.Error("base line was re-encoded — unmodelled fields must survive as raw bytes")
	}

	lines, dois, absent, err := ReadWorksBase(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("base lines = %d, want 2", len(lines))
	}
	if !dois["10.1000/base"] || !dois["10.1000/new"] {
		t.Errorf("dois = %v", dois)
	}
	if !absent["10.9999/nope"] {
		t.Errorf("absent = %v — the known-absent set is what stops a delta re-asking", absent)
	}

	var meta WorksMeta
	metaRaw, err := os.ReadFile(body + ".meta.json") //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.RecordsTotal != 2 || meta.SHA256 == "" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestReadWorksBase_BodyWithoutSidecarIsNoBase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, WorksFile), []byte(`{"doi":"10.1/x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, dois, absent, err := ReadWorksBase(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lines != nil || len(dois) != 0 || len(absent) != 0 {
		t.Error("a body with no sidecar is a partial write — it must not seed a delta")
	}
}

func TestNormalizeDOI(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"10.1098/RSTB.2022.0048":               "10.1098/rstb.2022.0048",
		"https://doi.org/10.1098/rstb.2022.48": "10.1098/rstb.2022.48",
		"http://dx.doi.org/10.1/X":             "10.1/x",
		"doi:10.1/y":                           "10.1/y",
		"  10.1/z  ":                           "10.1/z",
		"":                                     "",
	}
	for in, want := range cases {
		if got := NormalizeDOI(in); got != want {
			t.Errorf("NormalizeDOI(%q) = %q, want %q", in, got, want)
		}
	}
}
