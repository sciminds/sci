package brew

import (
	"testing"
)

func TestList_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\nbrew \"curl\"\n")

	result, err := List(bf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(result.Packages))
	}
	if result.Packages[0] != "htop" {
		t.Errorf("result.Packages[0] = %q, want %q", result.Packages[0], "htop")
	}
}

func TestList_WithType(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\ncask \"firefox\"\nuv \"marimo\"\n")

	result, err := List(bf, "cask")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(result.Packages))
	}
	if result.Packages[0] != "firefox" {
		t.Errorf("result.Packages[0] = %q, want %q", result.Packages[0], "firefox")
	}
}

func TestList_FormulaType(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\ncask \"firefox\"\n")

	result, err := List(bf, "formula")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(result.Packages))
	}
	if result.Packages[0] != "htop" {
		t.Errorf("result.Packages[0] = %q, want %q", result.Packages[0], "htop")
	}
}

func TestParseBrewInfo_Formulae(t *testing.T) {
	t.Parallel()
	jsonData := `{"formulae":[{"name":"htop","desc":"Improved top","versions":{"stable":"3.4.1"}},{"name":"curl","desc":"Get a file from an HTTP server","versions":{"stable":"8.9.1"}}],"casks":[]}`
	pkgs, err := parseBrewInfo(jsonData, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "htop" || pkgs[0].Desc != "Improved top" || pkgs[0].Type != "formula" {
		t.Errorf("pkgs[0] = %+v", pkgs[0])
	}
	if pkgs[0].Version != "3.4.1" {
		t.Errorf("pkgs[0].Version = %q, want %q", pkgs[0].Version, "3.4.1")
	}
}

func TestParseBrewInfo_Casks(t *testing.T) {
	t.Parallel()
	jsonData := `{"formulae":[],"casks":[{"token":"firefox","desc":"Web browser","version":"149.0"}]}`
	pkgs, err := parseBrewInfo(jsonData, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Name != "firefox" || pkgs[0].Type != "cask" {
		t.Errorf("pkgs[0] = %+v", pkgs[0])
	}
	if pkgs[0].Version != "149.0" {
		t.Errorf("pkgs[0].Version = %q, want %q", pkgs[0].Version, "149.0")
	}
}

func TestListDetailed_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\ncask \"firefox\"\n")
	m := &mockRunner{
		infoResult: []PackageInfo{
			{Name: "htop", Desc: "Improved top", Type: "formula"},
		},
		infoCaskResult: []PackageInfo{
			{Name: "firefox", Desc: "Web browser", Type: "cask"},
		},
	}

	result, err := ListDetailed(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(result))
	}
	// Sorted alphabetically.
	if result[0].Name != "firefox" {
		t.Errorf("result[0].Name = %q, want %q", result[0].Name, "firefox")
	}
	if result[1].Name != "htop" {
		t.Errorf("result[1].Name = %q, want %q", result[1].Name, "htop")
	}
}
