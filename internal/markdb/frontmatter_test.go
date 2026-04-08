package markdb

import (
	"testing"
)

func TestExtractFrontmatter(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantRaw    string
		wantBody   string
		wantKeys   []string // expected keys in Frontmatter map
		wantNilMap bool     // expect Frontmatter == nil
		wantError  bool     // expect ParseError non-empty
	}{
		{
			name:     "valid frontmatter",
			input:    "---\ntitle: Hello\ntags: [a, b]\n---\nBody text",
			wantRaw:  "title: Hello\ntags: [a, b]\n",
			wantBody: "Body text",
			wantKeys: []string{"title", "tags"},
		},
		{
			name:       "no frontmatter",
			input:      "Just body text\nwith multiple lines",
			wantRaw:    "",
			wantBody:   "Just body text\nwith multiple lines",
			wantNilMap: true,
		},
		{
			name:     "empty frontmatter",
			input:    "---\n---\nBody after empty",
			wantRaw:  "",
			wantBody: "Body after empty",
		},
		{
			name:      "malformed YAML",
			input:     "---\n: [bad\n---\nBody after bad yaml",
			wantRaw:   ": [bad\n",
			wantBody:  "Body after bad yaml",
			wantError: true,
		},
		{
			name:       "no closing delimiter",
			input:      "---\ntitle: x\nBody with no close",
			wantRaw:    "",
			wantBody:   "---\ntitle: x\nBody with no close",
			wantNilMap: true,
		},
		{
			name:       "first line not delimiter",
			input:      "title: x\n---\nBody",
			wantRaw:    "",
			wantBody:   "title: x\n---\nBody",
			wantNilMap: true,
		},
		{
			name:     "complex YAML types",
			input:    "---\ncount: 42\npi: 3.14\ndraft: true\ntags:\n  - go\n  - sqlite\nnested:\n  key: value\n---\nBody",
			wantRaw:  "count: 42\npi: 3.14\ndraft: true\ntags:\n  - go\n  - sqlite\nnested:\n  key: value\n",
			wantBody: "Body",
			wantKeys: []string{"count", "pi", "draft", "tags", "nested"},
		},
		{
			name:     "triple dash in body after frontmatter",
			input:    "---\ntitle: Post\n---\nSome body\n---\nThis is not a delimiter",
			wantRaw:  "title: Post\n",
			wantBody: "Some body\n---\nThis is not a delimiter",
			wantKeys: []string{"title"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFrontmatter([]byte(tt.input))

			if got.FrontmatterRaw != tt.wantRaw {
				t.Errorf("FrontmatterRaw = %q, want %q", got.FrontmatterRaw, tt.wantRaw)
			}
			if got.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", got.Body, tt.wantBody)
			}
			if tt.wantNilMap && got.Frontmatter != nil {
				t.Errorf("Frontmatter = %v, want nil", got.Frontmatter)
			}
			if !tt.wantNilMap && got.Frontmatter == nil && !tt.wantError {
				t.Error("Frontmatter is nil, want non-nil")
			}
			if tt.wantError && got.ParseError == "" {
				t.Error("ParseError is empty, want non-empty")
			}
			if !tt.wantError && got.ParseError != "" {
				t.Errorf("ParseError = %q, want empty", got.ParseError)
			}
			for _, key := range tt.wantKeys {
				if _, ok := got.Frontmatter[key]; !ok {
					t.Errorf("Frontmatter missing key %q", key)
				}
			}
		})
	}
}

func TestExtractFrontmatterValues(t *testing.T) {
	input := "---\ncount: 42\npi: 3.14\ndraft: true\nname: hello\n---\n"
	got := ExtractFrontmatter([]byte(input))

	if v, ok := got.Frontmatter["count"].(int); !ok || v != 42 {
		t.Errorf("count = %v (%T), want 42 (int)", got.Frontmatter["count"], got.Frontmatter["count"])
	}
	if v, ok := got.Frontmatter["pi"].(float64); !ok || v != 3.14 {
		t.Errorf("pi = %v (%T), want 3.14 (float64)", got.Frontmatter["pi"], got.Frontmatter["pi"])
	}
	if v, ok := got.Frontmatter["draft"].(bool); !ok || v != true {
		t.Errorf("draft = %v (%T), want true (bool)", got.Frontmatter["draft"], got.Frontmatter["draft"])
	}
	if v, ok := got.Frontmatter["name"].(string); !ok || v != "hello" {
		t.Errorf("name = %v (%T), want hello (string)", got.Frontmatter["name"], got.Frontmatter["name"])
	}
}
