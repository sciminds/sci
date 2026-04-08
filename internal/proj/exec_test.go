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
		ds     DocSystem
		target string
		want   []string
	}{
		{"quarto no target", Quarto, "", []string{"quarto", "render"}},
		{"quarto with target", Quarto, "code/report.qmd", []string{"quarto", "render", "code/report.qmd"}},
		{"myst", Myst, "", []string{"npx", "mystmd", "build", "--html"}},
		{"none returns nil", NoDoc, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildRenderArgs(tt.ds, tt.target)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPreviewArgs(t *testing.T) {
	tests := []struct {
		name string
		ds   DocSystem
		want []string
	}{
		{"quarto", Quarto, []string{"quarto", "preview"}},
		{"myst", Myst, []string{"npx", "mystmd", "start"}},
		{"none returns nil", NoDoc, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildPreviewArgs(tt.ds)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
