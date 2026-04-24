package py

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// ---------------------------------------------------------------------------
// DetectEnv
// ---------------------------------------------------------------------------

func TestDetectEnv(t *testing.T) {
	t.Run("pixi takes priority over uv", func(t *testing.T) {
		dir := t.TempDir()
		// Create both .pixi and pyproject.toml+.venv — pixi should win.
		mustMkdir(t, filepath.Join(dir, ".pixi"))
		mustTouch(t, filepath.Join(dir, "pyproject.toml"))
		mustMkdir(t, filepath.Join(dir, ".venv"))

		env := DetectEnv(dir)
		if env.Kind != EnvPixi {
			t.Errorf("expected pixi, got %v", env.Kind)
		}
		if env.Dir != dir {
			t.Errorf("expected dir %q, got %q", dir, env.Dir)
		}
	})

	t.Run("uv detected with pyproject.toml and .venv", func(t *testing.T) {
		dir := t.TempDir()
		mustTouch(t, filepath.Join(dir, "pyproject.toml"))
		mustMkdir(t, filepath.Join(dir, ".venv"))

		env := DetectEnv(dir)
		if env.Kind != EnvUV {
			t.Errorf("expected uv, got %v", env.Kind)
		}
		if env.Dir != dir {
			t.Errorf("expected dir %q, got %q", dir, env.Dir)
		}
	})

	t.Run("pyproject.toml alone is not enough", func(t *testing.T) {
		dir := t.TempDir()
		mustTouch(t, filepath.Join(dir, "pyproject.toml"))

		env := DetectEnv(dir)
		if env.Kind != EnvNone {
			t.Errorf("expected none, got %v", env.Kind)
		}
	})

	t.Run(".venv alone is not enough", func(t *testing.T) {
		dir := t.TempDir()
		mustMkdir(t, filepath.Join(dir, ".venv"))

		env := DetectEnv(dir)
		if env.Kind != EnvNone {
			t.Errorf("expected none, got %v", env.Kind)
		}
	})

	t.Run("empty directory returns none", func(t *testing.T) {
		dir := t.TempDir()
		env := DetectEnv(dir)
		if env.Kind != EnvNone {
			t.Errorf("expected none, got %v", env.Kind)
		}
	})
}

// ---------------------------------------------------------------------------
// FindEnv (walks up from cwd)
// ---------------------------------------------------------------------------

func TestFindEnv(t *testing.T) {
	t.Run("match at cwd", func(t *testing.T) {
		dir := t.TempDir()
		mustTouch(t, filepath.Join(dir, "pyproject.toml"))
		mustMkdir(t, filepath.Join(dir, ".venv"))
		t.Setenv("HOME", t.TempDir())

		env := FindEnv(dir)
		if env.Kind != EnvUV {
			t.Errorf("expected uv, got %v", env.Kind)
		}
		if env.Dir != dir {
			t.Errorf("expected dir %q, got %q", dir, env.Dir)
		}
	})

	t.Run("match at ancestor (uv)", func(t *testing.T) {
		root := t.TempDir()
		mustTouch(t, filepath.Join(root, "pyproject.toml"))
		mustMkdir(t, filepath.Join(root, ".venv"))
		sub := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())

		env := FindEnv(sub)
		if env.Kind != EnvUV {
			t.Errorf("expected uv, got %v", env.Kind)
		}
		if env.Dir != root {
			t.Errorf("expected dir %q, got %q", root, env.Dir)
		}
	})

	t.Run("match at ancestor (pixi)", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, ".pixi"))
		sub := filepath.Join(root, "a")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())

		env := FindEnv(sub)
		if env.Kind != EnvPixi {
			t.Errorf("expected pixi, got %v", env.Kind)
		}
		if env.Dir != root {
			t.Errorf("expected dir %q, got %q", root, env.Dir)
		}
	})

	t.Run(".git boundary stops walk", func(t *testing.T) {
		// pyproject lives above a .git boundary — must not be picked up.
		outer := t.TempDir()
		mustTouch(t, filepath.Join(outer, "pyproject.toml"))
		mustMkdir(t, filepath.Join(outer, ".venv"))

		repo := filepath.Join(outer, "repo")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(repo, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())

		env := FindEnv(sub)
		if env.Kind != EnvNone {
			t.Errorf("expected none (stopped at .git), got %v (dir=%q)", env.Kind, env.Dir)
		}
	})

	t.Run(".git as file (worktree) also stops walk", func(t *testing.T) {
		// Git worktrees use a `.git` file, not a directory.
		outer := t.TempDir()
		mustTouch(t, filepath.Join(outer, "pyproject.toml"))
		mustMkdir(t, filepath.Join(outer, ".venv"))

		repo := filepath.Join(outer, "repo")
		if err := os.Mkdir(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		mustTouch(t, filepath.Join(repo, ".git"))
		sub := filepath.Join(repo, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())

		env := FindEnv(sub)
		if env.Kind != EnvNone {
			t.Errorf("expected none (stopped at .git file), got %v", env.Kind)
		}
	})

	t.Run("HOME boundary stops walk", func(t *testing.T) {
		// pyproject above $HOME must not be picked up.
		parent := t.TempDir()
		mustTouch(t, filepath.Join(parent, "pyproject.toml"))
		mustMkdir(t, filepath.Join(parent, ".venv"))

		home := filepath.Join(parent, "home")
		if err := os.Mkdir(home, 0o755); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(home, "proj")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)

		env := FindEnv(sub)
		if env.Kind != EnvNone {
			t.Errorf("expected none (stopped at HOME), got %v", env.Kind)
		}
	})

	t.Run("env at boundary itself is still returned", func(t *testing.T) {
		// .git and pyproject+venv both at the same level — env wins,
		// boundary only prevents ascending further.
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, ".git"))
		mustTouch(t, filepath.Join(root, "pyproject.toml"))
		mustMkdir(t, filepath.Join(root, ".venv"))
		sub := filepath.Join(root, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())

		env := FindEnv(sub)
		if env.Kind != EnvUV {
			t.Errorf("expected uv, got %v", env.Kind)
		}
		if env.Dir != root {
			t.Errorf("expected dir %q, got %q", root, env.Dir)
		}
	})

	t.Run("no env anywhere returns none", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "a")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", root)

		env := FindEnv(sub)
		if env.Kind != EnvNone {
			t.Errorf("expected none, got %v", env.Kind)
		}
	})
}

// ---------------------------------------------------------------------------
// BuildUVArgs — IPython
// ---------------------------------------------------------------------------

func TestBuildUVArgs_IPython(t *testing.T) {
	tool := IPythonTool

	t.Run("ephemeral with no extras", func(t *testing.T) {
		env := EnvInfo{Kind: EnvNone}
		got := BuildUVArgs(tool, env, nil)

		allPkgs := slices.Concat([]string{tool.Pkg}, tool.DefaultPkgs)
		want := []string{"run"}
		for _, p := range allPkgs {
			want = append(want, "--with", p)
		}
		want = append(want, "--", "ipython")

		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("ephemeral with extras", func(t *testing.T) {
		env := EnvInfo{Kind: EnvNone}
		got := BuildUVArgs(tool, env, []string{"pandas", "matplotlib"})

		allPkgs := slices.Concat([]string{tool.Pkg}, tool.DefaultPkgs)
		allPkgs = append(allPkgs, "pandas", "matplotlib")
		want := []string{"run"}
		for _, p := range allPkgs {
			want = append(want, "--with", p)
		}
		want = append(want, "--", "ipython")

		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("uv project", func(t *testing.T) {
		env := EnvInfo{Kind: EnvUV, Dir: "/some/project"}
		got := BuildUVArgs(tool, env, nil)

		want := []string{"run", "--project", "/some/project", "--with", "ipython", "--", "ipython"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("uv project with extras", func(t *testing.T) {
		env := EnvInfo{Kind: EnvUV, Dir: "/some/project"}
		got := BuildUVArgs(tool, env, []string{"pandas"})

		want := []string{"run", "--project", "/some/project", "--with", "ipython", "--with", "pandas", "--", "ipython"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("pixi returns nil", func(t *testing.T) {
		env := EnvInfo{Kind: EnvPixi, Dir: "/some/pixi"}
		got := BuildUVArgs(tool, env, nil)

		if got != nil {
			t.Errorf("expected nil for pixi, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// BuildUVArgs — Marimo
// ---------------------------------------------------------------------------

func TestBuildUVArgs_Marimo(t *testing.T) {
	tool := MarimoTool

	t.Run("ephemeral with no extras", func(t *testing.T) {
		env := EnvInfo{Kind: EnvNone}
		got := BuildUVArgs(tool, env, nil)

		want := []string{"run", "--with", "marimo", "--", "marimo", "edit"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("ephemeral with extras", func(t *testing.T) {
		env := EnvInfo{Kind: EnvNone}
		got := BuildUVArgs(tool, env, []string{"pandas", "numpy"})

		want := []string{"run", "--with", "marimo", "--with", "pandas", "--with", "numpy", "--", "marimo", "edit"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("uv project", func(t *testing.T) {
		env := EnvInfo{Kind: EnvUV, Dir: "/some/project"}
		got := BuildUVArgs(tool, env, nil)

		want := []string{"run", "--project", "/some/project", "--with", "marimo", "--", "marimo", "edit"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("uv project with extras", func(t *testing.T) {
		env := EnvInfo{Kind: EnvUV, Dir: "/some/project"}
		got := BuildUVArgs(tool, env, []string{"polars"})

		want := []string{"run", "--project", "/some/project", "--with", "marimo", "--with", "polars", "--", "marimo", "edit"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("pixi returns nil", func(t *testing.T) {
		env := EnvInfo{Kind: EnvPixi, Dir: "/some/pixi"}
		got := BuildUVArgs(tool, env, nil)

		if got != nil {
			t.Errorf("expected nil for pixi, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// BuildPixiCmd
// ---------------------------------------------------------------------------

func TestBuildPixiCmd(t *testing.T) {
	t.Run("ipython returns python -m IPython", func(t *testing.T) {
		got := BuildPixiCmd("/my/project", IPythonTool)
		want := []string{
			filepath.Join("/my/project", ".pixi", "envs", "default", "bin", "python"),
			"-m", "IPython",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("marimo returns python -m marimo edit", func(t *testing.T) {
		got := BuildPixiCmd("/my/project", MarimoTool)
		want := []string{
			filepath.Join("/my/project", ".pixi", "envs", "default", "bin", "python"),
			"-m", "marimo", "edit",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustTouch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
