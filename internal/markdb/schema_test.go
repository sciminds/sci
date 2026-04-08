package markdb

import (
	"sort"
	"testing"
)

func TestSanitizeColumnName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"title", "title"},
		{"my-key.name", "my_key_name"},
		{"path", "fm_path"},
		{"body", "fm_body"},
		{"mtime", "fm_mtime"},
		{"hash", "fm_hash"},
		{"id", "fm_id"},
		{"source_id", "fm_source_id"},
		{"frontmatter_raw", "fm_frontmatter_raw"},
		{"body_text", "fm_body_text"},
		{"frontmatter_text", "fm_frontmatter_text"},
		{"parse_error", "fm_parse_error"},
		{"1key", "_1key"},
		{"normal_key", "normal_key"},
		{"UPPER", "UPPER"},
		{"with spaces", "with_spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeColumnName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeColumnName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferType(t *testing.T) {
	tests := []struct {
		name   string
		values []any
		want   string
	}{
		{"all strings", []any{"a", "b", "c"}, "text"},
		{"all ints", []any{1, 2, 3}, "integer"},
		{"all int64", []any{int64(1), int64(2)}, "integer"},
		{"all floats", []any{1.5, 2.5}, "real"},
		{"mix int and float", []any{1, 2.5}, "real"},
		{"booleans", []any{true, false}, "integer"},
		{"list values", []any{[]any{"a", "b"}}, "json"},
		{"map values", []any{map[string]any{"k": "v"}}, "json"},
		{"mix int and string", []any{1, "two"}, "text"},
		{"mix bool and int", []any{true, 42}, "integer"},
		{"mix bool and string", []any{true, "yes"}, "text"},
		{"empty", []any{}, "text"},
		{"nil values skipped", []any{nil, "hello"}, "text"},
		{"all nil", []any{nil, nil}, "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferType(tt.values)
			if got != tt.want {
				t.Errorf("InferType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverSchema(t *testing.T) {
	parsed := []map[string]any{
		{"title": "Post 1", "count": 10, "draft": true},
		{"title": "Post 2", "count": 20, "tags": []any{"go", "sqlite"}},
		{"title": "Post 3", "category": "work"},
	}

	cols := DiscoverSchema(parsed)

	// Build lookup by key.
	byKey := make(map[string]ColumnDef)
	for _, c := range cols {
		byKey[c.Key] = c
	}

	// title: present in all 3, type text
	if c, ok := byKey["title"]; !ok {
		t.Error("missing key 'title'")
	} else {
		if c.InferredType != "text" {
			t.Errorf("title type = %q, want text", c.InferredType)
		}
		if c.FileCount != 3 {
			t.Errorf("title file_count = %d, want 3", c.FileCount)
		}
	}

	// count: present in 2, type integer
	if c, ok := byKey["count"]; !ok {
		t.Error("missing key 'count'")
	} else {
		if c.InferredType != "integer" {
			t.Errorf("count type = %q, want integer", c.InferredType)
		}
		if c.FileCount != 2 {
			t.Errorf("count file_count = %d, want 2", c.FileCount)
		}
	}

	// draft: present in 1, type integer (bool)
	if c, ok := byKey["draft"]; !ok {
		t.Error("missing key 'draft'")
	} else if c.InferredType != "integer" {
		t.Errorf("draft type = %q, want integer", c.InferredType)
	}

	// tags: present in 1, type json
	if c, ok := byKey["tags"]; !ok {
		t.Error("missing key 'tags'")
	} else if c.InferredType != "json" {
		t.Errorf("tags type = %q, want json", c.InferredType)
	}

	// category: present in 1, type text
	if c, ok := byKey["category"]; !ok {
		t.Error("missing key 'category'")
	} else if c.FileCount != 1 {
		t.Errorf("category file_count = %d, want 1", c.FileCount)
	}

	// Total: 5 unique keys
	if len(cols) != 5 {
		keys := make([]string, len(cols))
		for i, c := range cols {
			keys[i] = c.Key
		}
		sort.Strings(keys)
		t.Errorf("got %d columns %v, want 5", len(cols), keys)
	}

	// Verify reserved name handling.
	reserved := []map[string]any{
		{"path": "/some/path", "title": "test"},
	}
	cols2 := DiscoverSchema(reserved)
	byKey2 := make(map[string]ColumnDef)
	for _, c := range cols2 {
		byKey2[c.Key] = c
	}
	if c, ok := byKey2["path"]; !ok {
		t.Error("missing key 'path'")
	} else if c.ColumnName != "fm_path" {
		t.Errorf("path column = %q, want fm_path", c.ColumnName)
	}
}
