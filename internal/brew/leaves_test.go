package brew

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestComputeRequestedLeaves(t *testing.T) {
	t.Parallel()
	receipts := []installReceipt{
		// On-request core formula, nothing depends on it -> leaf.
		{Name: "git", InstalledOnRequest: true},
		// On-request tap formula -> leaf, emitted tap-qualified so it stays
		// installable (bun is not in homebrew-core).
		{Name: "bun", InstalledOnRequest: true, Source: receiptSource{Tap: "oven-sh/bun"}},
		// On-request, but another installed formula depends on it -> NOT a leaf.
		{Name: "openssl@3", InstalledOnRequest: true},
		// Pulled in as a dependency, not on request -> NOT a leaf.
		{Name: "ca-certificates", InstalledOnRequest: false},
		// Consumer that depends on openssl@3 and ca-certificates.
		{
			Name:                "curl",
			InstalledOnRequest:  true,
			RuntimeDependencies: []receiptDep{{FullName: "openssl@3"}, {FullName: "ca-certificates"}},
		},
	}

	got := computeRequestedLeaves(receipts)
	// Sorted; openssl@3 is excluded because curl depends on it, ca-certificates
	// because it wasn't installed on request.
	want := []string{"curl", "git", "oven-sh/bun/bun"}
	if !slices.Equal(got, want) {
		t.Errorf("computeRequestedLeaves() = %v, want %v", got, want)
	}
}

func TestLeavesFromCellar(t *testing.T) {
	t.Parallel()
	cellar := t.TempDir()
	writeReceipt(t, cellar, "git", "2.45.0", `{"installed_on_request": true, "source": {"tap": "homebrew/core"}}`)
	writeReceipt(t, cellar, "bun", "1.3.14", `{"installed_on_request": true, "source": {"tap": "oven-sh/bun"}}`)
	writeReceipt(t, cellar, "openssl@3", "3.3.0", `{"installed_on_request": false, "runtime_dependencies": [{"full_name": "ca-certificates"}]}`)
	writeReceipt(t, cellar, "ca-certificates", "2024", `{"installed_on_request": false}`)

	got := leavesFromCellar(cellar)
	want := []string{"git", "oven-sh/bun/bun"}
	if !slices.Equal(got, want) {
		t.Errorf("leavesFromCellar() = %v, want %v", got, want)
	}
}

// writeReceipt creates <cellar>/<name>/<version>/INSTALL_RECEIPT.json.
func writeReceipt(t *testing.T, cellar, name, version, body string) {
	t.Helper()
	dir := filepath.Join(cellar, name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "INSTALL_RECEIPT.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
