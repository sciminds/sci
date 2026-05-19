package store

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestIsSafeIdentifier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"penguins", true},
		{"BOLD signal", true},
		{"my_table", true},
		{"Table123", true},
		{"", false},
		{"drop;--", false},
		{`table"name`, false},
		{"hello\tworld", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsSafeIdentifier(tt.input); got != tt.want {
				t.Errorf("IsSafeIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func ExampleIsSafeIdentifier() {
	fmt.Println(IsSafeIdentifier("users"))
	fmt.Println(IsSafeIdentifier("my_table"))
	fmt.Println(IsSafeIdentifier("Robert'); DROP TABLE--"))
	fmt.Println(IsSafeIdentifier(""))
	// Output:
	// true
	// true
	// false
	// false
}

func TestIsSafeColumnName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"name", true},
		{"Date (UTC)", true},
		{"% complete", true},
		{"temp_°C", true},
		{"Q1-2024", true},
		{"日本語", true},
		{"x.y", true},
		{"col with spaces", true},
		{"", false},
		{"has\"quote", false},
		{"back\\slash", false},
		{"tab\there", false},
		{"new\nline", false},
		{"null\x00byte", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := IsSafeColumnName(tt.input); got != tt.want {
				t.Errorf("IsSafeColumnName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeImportHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "leading BOM in header is replaced",
			in:   []string{"\ufeffBad", "Good"},
			want: []string{"_Bad", "Good"},
		},
		{
			name: "trim surrounding whitespace",
			in:   []string{"  Name  ", "\tAge\t"},
			want: []string{"Name", "Age"},
		},
		{
			name: "fill empty headers",
			in:   []string{"a", "", "b", ""},
			want: []string{"a", "column_2", "b", "column_4"},
		},
		{
			name: "disambiguate duplicates",
			in:   []string{"name", "name", "name"},
			want: []string{"name", "name_1", "name_2"},
		},
		{
			name: "duplicate disambiguation skips collisions",
			in:   []string{"x", "x_1", "x"},
			want: []string{"x", "x_1", "x_2"},
		},
		{
			name: "unsafe chars replaced",
			in:   []string{`bad"col`, `back\slash`},
			want: []string{"bad_col", "back_slash"},
		},
		{
			name: "preserves Unicode and punctuation",
			in:   []string{"Date (UTC)", "temp_°C", "% complete"},
			want: []string{"Date (UTC)", "temp_°C", "% complete"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeImportHeaders(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SanitizeImportHeaders(%q) = %q, want %q", tt.in, got, tt.want)
			}
			for i, h := range got {
				if !IsSafeColumnName(h) {
					t.Errorf("SanitizeImportHeaders(%q)[%d] = %q, fails IsSafeColumnName", tt.in, i, h)
				}
			}
		})
	}
}

func TestDecodeReader_StripsUTF8BOM(t *testing.T) {
	t.Parallel()
	src := "\ufeffBad,Good\n1,2\n"
	got, err := io.ReadAll(DecodeReader(strings.NewReader(src)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "Bad,Good\n1,2\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecodeReader_PassesThroughWithoutBOM(t *testing.T) {
	t.Parallel()
	src := "Bad,Good\n1,2\n"
	got, err := io.ReadAll(DecodeReader(strings.NewReader(src)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != src {
		t.Errorf("got %q, want %q", got, src)
	}
}

func TestDecodeReader_DecodesUTF16LEBOM(t *testing.T) {
	t.Parallel()
	utf16le := []byte{0xff, 0xfe}
	for _, r := range "Bad,Good\n" {
		utf16le = append(utf16le, byte(r), 0x00)
	}
	got, err := io.ReadAll(DecodeReader(strings.NewReader(string(utf16le))))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "Bad,Good\n" {
		t.Errorf("got %q, want %q", got, "Bad,Good\n")
	}
}

func TestIsSafeIdentifierEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
		desc  string
	}{
		{"SELECT", true, "SQL keyword is still alphanumeric"},
		{"a", true, "single char"},
		{strings.Repeat("a", 100), true, "100-char name"},
		{"has-hyphen", false, "hyphen not allowed"},
		{"has.dot", false, "dot not allowed"},
		{"tab\there", false, "tab char not allowed"},
		{"new\nline", false, "newline not allowed"},
		{"DROP TABLE", true, "space allowed (DuckDB compat)"},
		{"résumé", false, "unicode not allowed"},
		{"_leading", true, "leading underscore ok"},
		{"123num", true, "leading digit ok"},
	}
	for _, tt := range tests {
		if got := IsSafeIdentifier(tt.input); got != tt.want {
			t.Errorf("IsSafeIdentifier(%q) [%s] = %v, want %v", tt.input, tt.desc, got, tt.want)
		}
	}
}

func TestContainsWriteKeyword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
		desc  string
	}{
		{"WITH x AS (SELECT 1) INSERT INTO t VALUES (1)", true, "space-delimited INSERT"},
		{"WITH x AS (SELECT 1)INSERT INTO t VALUES (1)", true, "no space before INSERT"},
		{"WITH x AS (SELECT 1) UPDATE t SET a=1", true, "UPDATE"},
		{"WITH x AS (SELECT 1) DELETE FROM t", true, "DELETE"},
		{"WITH x AS (SELECT 1) DROP TABLE t", true, "DROP"},
		{"WITH x AS (SELECT 1) ALTER TABLE t ADD COLUMN x INT", true, "ALTER"},
		{"WITH x AS (SELECT 1) CREATE TABLE t(a INT)", true, "CREATE"},
		{"WITH x AS (SELECT 1) SELECT * FROM x", false, "pure SELECT CTE"},
		{"WITH INSERTED AS (SELECT 1) SELECT * FROM INSERTED", false, "INSERTED is not INSERT keyword"},
		{"WITH x AS (SELECT 1) SELECT UPDATED FROM x", false, "UPDATED is not UPDATE keyword"},
		{"WITH x AS (SELECT 1) SELECT DELETER FROM x", false, "DELETER is not DELETE keyword"},
	}
	for _, tt := range tests {
		upper := strings.ToUpper(tt.input)
		if got := ContainsWriteKeyword(upper); got != tt.want {
			t.Errorf("ContainsWriteKeyword(%q) [%s] = %v, want %v", tt.input, tt.desc, got, tt.want)
		}
	}
}

func TestImportableExtensions(t *testing.T) {
	t.Parallel()
	exts := ImportableExtensions()
	want := map[string]bool{".csv": true, ".tsv": true, ".json": true, ".jsonl": true, ".ndjson": true}
	if len(exts) != len(want) {
		t.Fatalf("expected %d extensions, got %d", len(want), len(exts))
	}
	for _, ext := range exts {
		if !want[ext] {
			t.Errorf("unexpected extension %q", ext)
		}
	}
}

func TestTableNameFromFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"data.csv", "data"},
		{"123.json", "_123"},
		{"hello world.csv", "hello_world"},
		{"/some/path/test.jsonl", "test"},
		{"----.csv", "____"},
	}
	for _, tt := range tests {
		got := TableNameFromFile(tt.path)
		if got != tt.want {
			t.Errorf("TableNameFromFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestValidateReadOnlySQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"plain SELECT", "SELECT * FROM t", false},
		{"WITH CTE pure read", "WITH x AS (SELECT 1) SELECT * FROM x", false},
		{"empty", "", true},
		{"semicolon", "SELECT 1; DROP TABLE t", true},
		{"non-select", "DELETE FROM t", true},
		{"writable CTE", "WITH x AS (SELECT 1) INSERT INTO t VALUES (1)", true},
	}
	for _, tt := range tests {
		_, err := ValidateReadOnlySQL(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
		}
	}
}
