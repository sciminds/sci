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
