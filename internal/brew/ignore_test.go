package brew

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/sci/internal/sciconfig"
)

// withIgnoreConfig points sci's config directory at a temp dir. When content is
// non-empty it becomes tools.json. Returns the tools.json path.
func withIgnoreConfig(t *testing.T, content string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	path := sciconfig.Path("tools.json")
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mustLoadIgnored(t *testing.T) IgnoreList {
	t.Helper()
	list, err := LoadIgnored()
	if err != nil {
		t.Fatalf("LoadIgnored: %v", err)
	}
	return list
}

const ignoredDockerTwice = `{"ignored":[{"type":"formula","name":"docker"},{"type":"cask","name":"docker"}]}`

func TestLoadIgnored_Missing(t *testing.T) {
	withIgnoreConfig(t, "")
	if list := mustLoadIgnored(t); len(list) != 0 {
		t.Errorf("list = %v, want empty", list)
	}
}

func TestLoadIgnored_Malformed(t *testing.T) {
	path := withIgnoreConfig(t, `{"ignored":`)
	_, err := LoadIgnored()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want it to name %s", err, path)
	}
}

func TestLoadIgnored_UnknownType(t *testing.T) {
	path := withIgnoreConfig(t, `{"ignored":[{"type":"brew","name":"jq"}]}`)
	_, err := LoadIgnored()
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), `"brew"`) {
		t.Errorf("err = %v, want it to name %s and \"brew\"", err, path)
	}
}

func TestIgnoreList_Contains(t *testing.T) {
	t.Parallel()
	list := IgnoreList{
		{Type: TypeFormula, Name: "oven-sh/bun/bun"},
		{Type: TypeCask, Name: "docker"},
		{Type: TypeUV, Name: "my-tool"},
	}
	tests := []struct {
		pkgType, name string
		want          bool
	}{
		{TypeFormula, "bun", true},
		{TypeFormula, "oven-sh/bun/bun", true},
		{TypeCask, "docker", true},
		{TypeUV, "My_Tool", true},
		{TypeUV, "my-tool[x]", true},
		{TypeFormula, "docker", false},
		{TypeCask, "bun", false},
	}
	for _, tt := range tests {
		if got := list.Contains(tt.pkgType, tt.name); got != tt.want {
			t.Errorf("Contains(%s, %s) = %v, want %v", tt.pkgType, tt.name, got, tt.want)
		}
	}
}

func TestIgnore_SavesInstalled(t *testing.T) {
	withIgnoreConfig(t, "")
	m := &mockRunner{listCasksResult: []string{"libreoffice"}}
	result, err := Ignore(m, "libreoffice", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true")
	}
	want := IgnoreList{{Type: TypeCask, Name: "libreoffice"}}
	if got := mustLoadIgnored(t); !slices.Equal(got, want) {
		t.Errorf("saved %v, want %v", got, want)
	}
}

func TestIgnore_NotInstalled(t *testing.T) {
	path := withIgnoreConfig(t, "")
	_, err := Ignore(&mockRunner{}, "libreoffice", "", false)
	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("err = %v, want ErrNotInstalled", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("tools.json should not exist")
	}
}

func TestIgnore_Ambiguous(t *testing.T) {
	withIgnoreConfig(t, "")
	m := &mockRunner{listFormulaeResult: []string{"docker"}, listCasksResult: []string{"docker"}}

	_, err := Ignore(m, "docker", "", false)
	if !errors.Is(err, ErrAmbiguousType) {
		t.Fatalf("err = %v, want ErrAmbiguousType", err)
	}
	if want := `"docker" matches formula and cask: pass --formula, --cask, or --uv`; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}

	if _, err := Ignore(m, "docker", TypeCask, false); err != nil {
		t.Fatal(err)
	}
	want := IgnoreList{{Type: TypeCask, Name: "docker"}}
	if got := mustLoadIgnored(t); !slices.Equal(got, want) {
		t.Errorf("saved %v, want %v", got, want)
	}
}

func TestIgnore_Twice(t *testing.T) {
	withIgnoreConfig(t, "")
	m := &mockRunner{uvToolListResult: []string{"ruff"}}
	if _, err := Ignore(m, "ruff", "", false); err != nil {
		t.Fatal(err)
	}
	result, err := Ignore(m, "ruff", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Error("Changed = true on the second ignore")
	}
	if got := result.Human(); !strings.Contains(got, "ruff is already ignored") {
		t.Errorf("Human() = %q", got)
	}
	if got := mustLoadIgnored(t); len(got) != 1 {
		t.Errorf("saved %v, want one entry", got)
	}
}

func TestIgnore_DryRun(t *testing.T) {
	path := withIgnoreConfig(t, "")
	result, err := Ignore(&mockRunner{listFormulaeResult: []string{"jq"}}, "jq", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.DryRun {
		t.Errorf("result = %+v, want Changed and DryRun", result)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("dry run wrote tools.json")
	}
}

func TestUnignore_NotInstalled(t *testing.T) {
	withIgnoreConfig(t, `{"ignored":[{"type":"cask","name":"libreoffice"}]}`)
	result, err := Unignore("libreoffice", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true")
	}
	if got := mustLoadIgnored(t); len(got) != 0 {
		t.Errorf("saved %v, want empty", got)
	}
}

func TestUnignore_NotIgnored(t *testing.T) {
	withIgnoreConfig(t, "")
	result, err := Unignore("jq", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Error("Changed = true, want false")
	}
	if got := result.Human(); !strings.Contains(got, "jq is not ignored") {
		t.Errorf("Human() = %q", got)
	}
}

func TestUnignore_Ambiguous(t *testing.T) {
	withIgnoreConfig(t, ignoredDockerTwice)
	if _, err := Unignore("docker", "", false); !errors.Is(err, ErrAmbiguousType) {
		t.Fatalf("err = %v, want ErrAmbiguousType", err)
	}
	if got := mustLoadIgnored(t); len(got) != 2 {
		t.Errorf("saved %v, want both entries", got)
	}
}
