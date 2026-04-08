package convert

import (
	"strings"
	"testing"
)

func TestInferFormat(t *testing.T) {
	tests := []struct {
		path    string
		want    Format
		wantErr bool
	}{
		{path: "notebook.py", want: Marimo},
		{path: "notebook.md", want: MyST},
		{path: "notebook.qmd", want: Quarto},
		// Full paths still work
		{path: "/some/dir/analysis.py", want: Marimo},
		{path: "/some/dir/report.md", want: MyST},
		{path: "/some/dir/report.qmd", want: Quarto},
		// Unknown extensions return an error
		{path: "notebook.ipynb", wantErr: true},
		{path: "notebook.txt", wantErr: true},
		{path: "notebook", wantErr: true},
		{path: "notebook.R", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := InferFormat(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("InferFormat(%q) expected error, got nil (format=%q)", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("InferFormat(%q) unexpected error: %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("InferFormat(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestInferFormatErrorMessage(t *testing.T) {
	_, err := InferFormat("notebook.ipynb")
	if err == nil {
		t.Fatal("expected error for .ipynb")
	}
	if !strings.Contains(err.Error(), ".ipynb") {
		t.Errorf("error message should mention the bad extension, got: %q", err.Error())
	}
}

func TestConvertResultJSON(t *testing.T) {
	r := ConvertResult{
		Input:      "in.py",
		Output:     "out.md",
		FromFormat: "marimo",
		ToFormat:   "myst",
	}
	got := r.JSON()
	// JSON() must return the struct itself (used for JSON marshalling by cmdutil).
	if got != r {
		t.Errorf("JSON() returned unexpected value: %v", got)
	}
}

func TestConvertResultHuman(t *testing.T) {
	r := ConvertResult{
		Input:  "notebook.py",
		Output: "notebook.md",
	}
	got := r.Human()
	if !strings.Contains(got, "notebook.py") {
		t.Errorf("Human() should contain input filename, got: %q", got)
	}
	if !strings.Contains(got, "notebook.md") {
		t.Errorf("Human() should contain output filename, got: %q", got)
	}
	// Arrow separating input → output
	if !strings.Contains(got, "→") {
		t.Errorf("Human() should contain arrow separator, got: %q", got)
	}
}
