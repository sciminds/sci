package duck_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/store"
	"github.com/sciminds/cli/internal/store/contracttest"
	"github.com/sciminds/cli/internal/store/duck"
)

// setupContract builds the shared contract fixture in a fresh .duckdb file
// via the duckdb CLI and opens it. Skips cleanly when duckdb is absent.
func setupContract(t *testing.T) store.DataStore {
	t.Helper()
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb binary not on PATH; install via `sci doctor` to run this test")
	}
	path := filepath.Join(t.TempDir(), "contract.duckdb")
	script := `CREATE TABLE people (id BIGINT PRIMARY KEY, name VARCHAR, score DOUBLE);
INSERT INTO people VALUES (1, 'alice', 3.14), (2, 'bob', 2.72), (3, 'carol', NULL);
CREATE TABLE extras (k VARCHAR, v INTEGER);
INSERT INTO extras VALUES ('a', 1), ('b', 2);
`
	cmd := exec.Command("duckdb", path)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v\n%s", err, out)
	}
	s, err := duck.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStoreContract(t *testing.T) {
	contracttest.Run(t, setupContract)
}
