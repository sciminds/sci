package data

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

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
	// UTF-16 LE BOM (FF FE) followed by "Bad,Good\n" in UTF-16 LE.
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
