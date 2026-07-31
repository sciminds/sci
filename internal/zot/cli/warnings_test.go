package cli

// warnings_test.go — Phase C of the agent-surface hardening pass: the
// success-path warning rules. Warnings must fail open (no lastsync row → no
// claim) and carry a resubmittable fix when a --remote twin exists.

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/local"
)

// seedLastSync writes the version-table lastsync row Zotero maintains.
func seedLastSync(t *testing.T, dataDir string, at time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dataDir+"/zotero.sqlite")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(
		`INSERT OR REPLACE INTO version (schema, version) VALUES ('lastsync', ` +
			strconv.FormatInt(at.Unix(), 10) + `)`); err != nil {
		t.Fatalf("seed lastsync: %v", err)
	}
}

func searchEnvelope(t *testing.T, out []byte) struct {
	OK       bool              `json:"ok"`
	Warnings []cmdutil.Warning `json:"warnings"`
} {
	t.Helper()
	var env struct {
		OK       bool              `json:"ok"`
		Warnings []cmdutil.Warning `json:"warnings"`
	}
	start := strings.IndexByte(string(out), '{')
	if start < 0 {
		t.Fatalf("no JSON in output: %q", string(out))
	}
	if err := json.Unmarshal(out[start:], &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	return env
}

func TestSearch_StaleLocalDB_WarnsInEnvelope(t *testing.T) {
	dataDir := withOrientConfig(t)
	seedLastSync(t, dataDir, time.Now().Add(-40*24*time.Hour))

	out, err := runItemRead(t, "--json", "--library", "personal", "search", "Paper")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	env := searchEnvelope(t, out)
	if len(env.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want exactly one stale-local", env.Warnings)
	}
	w := env.Warnings[0]
	if w.Code != cmdutil.CodeStaleLocal {
		t.Errorf("code = %q, want stale-local", w.Code)
	}
	if !strings.Contains(w.Message, "40 days ago") {
		t.Errorf("message should quantify staleness, got %q", w.Message)
	}
}

func TestSearch_FreshLocalDB_NoWarning(t *testing.T) {
	dataDir := withOrientConfig(t)
	seedLastSync(t, dataDir, time.Now().Add(-2*24*time.Hour))

	out, err := runItemRead(t, "--json", "--library", "personal", "search", "Paper")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if env := searchEnvelope(t, out); len(env.Warnings) != 0 {
		t.Errorf("fresh DB must not warn, got %+v", env.Warnings)
	}
}

func TestSearch_NoLastSyncRow_FailsOpen(t *testing.T) {
	withOrientConfig(t) // orient fixture has no lastsync row

	out, err := runItemRead(t, "--json", "--library", "personal", "search", "Paper")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if env := searchEnvelope(t, out); len(env.Warnings) != 0 {
		t.Errorf("missing lastsync row must not warn, got %+v", env.Warnings)
	}
}

func TestSearch_StaleRule_EnvDisable(t *testing.T) {
	dataDir := withOrientConfig(t)
	seedLastSync(t, dataDir, time.Now().Add(-400*24*time.Hour))
	t.Setenv("SCI_ZOT_STALE_DAYS", "0")

	out, err := runItemRead(t, "--json", "--library", "personal", "search", "Paper")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if env := searchEnvelope(t, out); len(env.Warnings) != 0 {
		t.Errorf("SCI_ZOT_STALE_DAYS=0 must disable the rule, got %+v", env.Warnings)
	}
}

func TestRemoteRerunFix(t *testing.T) {
	t.Parallel()
	got := remoteRerunFix([]string{"/bin/sci", "zot", "search", "--json", "foo bar"})
	want := "sci zot search --json 'foo bar' --remote"
	if got != want {
		t.Errorf("remoteRerunFix = %q, want %q", got, want)
	}
	if fix := remoteRerunFix([]string{"go-test-binary", "-test.v"}); fix != "" {
		t.Errorf("no zot token should yield no fix, got %q", fix)
	}
}

func TestBibQualityWarning(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "AAAA1111", Date: "2024-01-01"},
		{Key: "BBBB2222", Date: ""},
		{Key: "CCCC3333", Date: "   "},
	}
	warns := bibQualityWarning(items, "shared")
	if len(warns) != 1 {
		t.Fatalf("want one warning, got %+v", warns)
	}
	w := warns[0]
	if !strings.Contains(w.Message, "2 of 3") {
		t.Errorf("message should quantify, got %q", w.Message)
	}
	if !strings.Contains(w.Message, "BBBB2222") {
		t.Errorf("message should name offending keys, got %q", w.Message)
	}
	if w.Fix != "sci zot --library shared doctor missing" {
		t.Errorf("fix = %q", w.Fix)
	}

	if clean := bibQualityWarning([]local.Item{{Key: "A", Date: "2020"}}, "personal"); len(clean) != 0 {
		t.Errorf("all-dated items must not warn, got %+v", clean)
	}
}

// fakeFreshness is a one-method stand-in for the freshness fact a
// local.Reader exposes. The narrow consumer-side interface is what makes this
// possible without a ~50-method Reader fake.
type fakeFreshness struct {
	walBytes int64
	walOK    bool
}

func (f fakeFreshness) PendingWAL() (int64, bool) { return f.walBytes, f.walOK }

// TestWALStaleWarning_FiresOnPendingWAL pins the rule and its honest
// negative: local reads always run in immutable mode, so a non-empty WAL is
// exactly what was missed — and a checkpointed one is no claim at all.
func TestWALStaleWarning_FiresOnPendingWAL(t *testing.T) {
	cases := []struct {
		name string
		db   fakeFreshness
		want bool
	}{
		{"pending WAL", fakeFreshness{walBytes: 2_542_072, walOK: true}, true},
		{"checkpointed", fakeFreshness{}, false},
		{"no signal despite bytes", fakeFreshness{walBytes: 2_542_072}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := walStaleWarning(tc.db)
			if (len(got) > 0) != tc.want {
				t.Fatalf("walStaleWarning fired=%v, want %v (%+v)", len(got) > 0, tc.want, tc.db)
			}
		})
	}
}

// TestWALStaleWarning_CarriesActionableFix — the remedy is the whole point.
// A user who is told their read may be stale needs to know what to do.
func TestWALStaleWarning_CarriesActionableFix(t *testing.T) {
	got := walStaleWarning(fakeFreshness{walBytes: 2_542_072, walOK: true})
	if len(got) != 1 {
		t.Fatalf("got %d warnings, want 1", len(got))
	}
	if got[0].Code != cmdutil.CodeStaleLocal {
		t.Errorf("code = %q, want %q", got[0].Code, cmdutil.CodeStaleLocal)
	}
	if !strings.Contains(strings.ToLower(got[0].Message), "zotero") {
		t.Errorf("message does not name Zotero as the cause: %q", got[0].Message)
	}
	if got[0].Fix == "" {
		t.Error("warning carries no fix — the user is told they may be stale with no remedy")
	}
}
