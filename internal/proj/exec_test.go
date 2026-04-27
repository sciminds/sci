package proj

import (
	"reflect"
	"testing"
)

func TestBuildRunTaskArgs(t *testing.T) {
	tests := []struct {
		name string
		pm   PkgManager
		task string
		args []string
		want []string
	}{
		{"pixi no args", Pixi, "setup", nil, []string{"pixi", "run", "setup"}},
		{"pixi with args", Pixi, "marimo", []string{"notebook.py"}, []string{"pixi", "run", "marimo", "notebook.py"}},
		{"uv no args", UV, "setup", nil, []string{"uv", "run", "poe", "setup"}},
		{"uv with args", UV, "render", []string{"--pdf"}, []string{"uv", "run", "poe", "render", "--pdf"}},
		{"unknown returns nil", "poetry", "test", nil, nil},
		{"empty pkg manager returns nil", "", "setup", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildRunTaskArgs(tt.pm, tt.task, tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildRenderArgs(t *testing.T) {
	tests := []struct {
		name   string
		kind   Kind
		ds     DocSystem
		target string
		want   []string
	}{
		{"python+quarto no target", Python, Quarto, "", []string{"quarto", "render"}},
		{"python+quarto with target", Python, Quarto, "code/report.qmd", []string{"quarto", "render", "code/report.qmd"}},
		{"python+myst → html", Python, Myst, "", []string{"npx", "mystmd", "build", "--html"}},
		{"writing+myst → pdf", Writing, Myst, "", []string{"npx", "mystmd", "build", "--pdf"}},
		{"python+none returns nil", Python, NoDoc, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildRenderArgs(tt.kind, tt.ds, tt.target)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPreviewArgs(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		ds   DocSystem
		want []string
	}{
		{"python+quarto", Python, Quarto, []string{"quarto", "preview"}},
		{"python+myst", Python, Myst, []string{"npx", "mystmd", "start"}},
		{"writing+myst", Writing, Myst, []string{"npx", "mystmd", "start"}},
		{"python+none returns nil", Python, NoDoc, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildPreviewArgs(tt.kind, tt.ds)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
