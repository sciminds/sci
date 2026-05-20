package app

// model_duck_test.go — verifies that the dbtui Model wires
// store.RowEditabilityChecker into tab.ReadOnly. The full mutation
// teatests live alongside their respective Phase 3 commits.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/store/duck"
)

// requireDuck skips when the duckdb CLI is not on PATH.
func requireDuck(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb binary not on PATH; install via `sci doctor` to run this test")
	}
}

// makeDuckFixture writes a small `.duckdb` with one PK table (`people`)
// and one PK-less table (`extras`) into a fresh temp dir.
func makeDuckFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.duckdb")
	script := `CREATE TABLE people (id BIGINT PRIMARY KEY, name VARCHAR);
INSERT INTO people VALUES (1, 'alice'), (2, 'bob');
CREATE TABLE extras (k VARCHAR, v INTEGER);
INSERT INTO extras VALUES ('a', 1);
`
	cmd := exec.Command("duckdb", path)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v\n%s", err, out)
	}
	return path
}

// makeHeavyDuckFixture writes a .duckdb with a FLOAT[] embedding column
// so the heavy-type projection path can be exercised end-to-end through
// buildTab.
func makeHeavyDuckFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "heavy.duckdb")
	script := `CREATE TABLE vecs (
  id BIGINT PRIMARY KEY,
  label VARCHAR,
  embedding FLOAT[]
);
INSERT INTO vecs VALUES
  (1, 'a', [0.1, 0.2, 0.3, 0.4]::FLOAT[]),
  (2, 'b', [0.5, 0.6, 0.7, 0.8]::FLOAT[]);
`
	cmd := exec.Command("duckdb", path)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create heavy fixture: %v\n%s", err, out)
	}
	return path
}

// TestDuckHeavyColumnPlaceholderAndPreview verifies that a FLOAT[]
// column surfaces in dbtui as a read-only placeholder cell and that the
// Enter preview lazily fetches the full payload via CellFetcher.
func TestDuckHeavyColumnPlaceholderAndPreview(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeHeavyDuckFixture(t))
	if err != nil {
		t.Fatalf("duck.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	m, err := NewModel(s, "heavy.duckdb", false)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	tab, err := buildTab(s, "vecs")
	if err != nil {
		t.Fatalf("buildTab: %v", err)
	}
	m.tabs[indexOf(m.tabs, "vecs")] = tab

	// Spec for embedding column: Heavy + Kind=cellReadonly.
	var embSpec columnSpec
	for _, sp := range tab.Specs {
		if sp.DBName == "embedding" {
			embSpec = sp
			break
		}
	}
	if !embSpec.Heavy {
		t.Errorf("embedding spec Heavy = false; want true")
	}
	if embSpec.Kind != cellReadonly {
		t.Errorf("embedding spec Kind = %v; want cellReadonly", embSpec.Kind)
	}

	// In-memory cell value is the placeholder, not the full payload.
	if got := tab.CellRows[0][2].Value; got != "<FLOAT[4]>" {
		t.Errorf("embedding cell value = %q; want <FLOAT[4]>", got)
	}

	// fetchHeavyCellValue resolves the placeholder back to the real data.
	// (Driving the full key event would require a teatest harness; the
	// unit-level call exercises the same lazy path the Enter handler does.)
	m.tabs[indexOf(m.tabs, "vecs")] = tab
	got, ok := m.fetchHeavyCellValue(&tab, embSpec, 0)
	if !ok {
		t.Fatalf("fetchHeavyCellValue returned not-ok")
	}
	for _, want := range []string{"0.1", "0.2", "0.3", "0.4"} {
		if !strings.Contains(got, want) {
			t.Errorf("fetched embedding = %q; missing %q", got, want)
		}
	}
}

// indexOf returns the position of name in tabs, or -1 if not found. Used
// by the heavy-column test above; the production code routes tab lookup
// through the active index.
func indexOf(tabs []Tab, name string) int {
	for i, t := range tabs {
		if t.Name == name {
			return i
		}
	}
	return -1
}

// TestDuckNoPKTabOpensReadOnly verifies that a duckdb table without a
// PRIMARY KEY surfaces as a read-only tab, while a PK-having table
// stays editable.
func TestDuckNoPKTabOpensReadOnly(t *testing.T) {
	requireDuck(t)
	s, err := duck.Open(makeDuckFixture(t))
	if err != nil {
		t.Fatalf("duck.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	m, err := NewModel(s, "fixture.duckdb", false)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	// Lazy-load every tab so ReadOnly settles for all of them.
	for i := range m.tabs {
		if m.tabs[i].Loaded {
			continue
		}
		built, err := buildTab(s, m.tabs[i].Name)
		if err != nil {
			t.Fatalf("buildTab %q: %v", m.tabs[i].Name, err)
		}
		if m.shouldForceTabReadOnly(built.Name) {
			built.ReadOnly = true
		}
		m.tabs[i] = built
	}

	got := make(map[string]bool, len(m.tabs))
	for _, tab := range m.tabs {
		got[tab.Name] = tab.ReadOnly
	}
	if !got["extras"] {
		t.Error("extras (no PK) should open read-only")
	}
	if got["people"] {
		t.Error("people (PK on id) should be editable")
	}
}
