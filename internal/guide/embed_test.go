package guide

import "testing"

func TestLoadCast(t *testing.T) {
	data, err := LoadCast("ls.cast")
	if err != nil {
		t.Fatalf("LoadCast(ls.cast): %v", err)
	}
	c, err := ParseCast(data)
	if err != nil {
		t.Fatalf("ParseCast: %v", err)
	}
	if c.Header.Version != 2 {
		t.Errorf("version = %d, want 2", c.Header.Version)
	}
	if len(c.Events) == 0 {
		t.Error("expected events, got none")
	}
}

func TestLoadCastNotFound(t *testing.T) {
	_, err := LoadCast("nonexistent.cast")
	if err == nil {
		t.Error("expected error for nonexistent cast file")
	}
}

func TestAllEntriesHaveCasts(t *testing.T) {
	for _, e := range append(BasicEntries, GitEntries...) {
		t.Run(e.Name, func(t *testing.T) {
			if e.Name == "" || e.Cmd == "" || e.Desc == "" || e.CastFile == "" {
				t.Error("entry has empty fields")
			}
			data, err := LoadCast(e.CastFile)
			if err != nil {
				t.Fatalf("LoadCast(%s): %v", e.CastFile, err)
			}
			if _, err := ParseCast(data); err != nil {
				t.Fatalf("ParseCast(%s): %v", e.CastFile, err)
			}
		})
	}
}

func TestLoadPage(t *testing.T) {
	data, err := LoadPage("python-basics.md")
	if err != nil {
		t.Fatalf("LoadPage(python-basics.md): %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty page content")
	}
}

func TestLoadPageNotFound(t *testing.T) {
	_, err := LoadPage("nonexistent.md")
	if err == nil {
		t.Error("expected error for nonexistent page file")
	}
}

func TestAllEntriesHavePages(t *testing.T) {
	for _, e := range PythonEntries {
		t.Run(e.Name, func(t *testing.T) {
			if e.Name == "" || e.Cmd == "" || e.Desc == "" || e.PageFile == "" {
				t.Error("entry has empty fields")
			}
			data, err := LoadPage(e.PageFile)
			if err != nil {
				t.Fatalf("LoadPage(%s): %v", e.PageFile, err)
			}
			if len(data) == 0 {
				t.Errorf("page %s is empty", e.PageFile)
			}
		})
	}
}
