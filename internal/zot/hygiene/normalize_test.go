package hygiene

import "testing"

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Deep Learning for Neuroimaging", "deep learning for neuroimaging"},
		{"Deep Learning: A Review", "deep learning a review"},
		{"  Leading and trailing  ", "leading and trailing"},
		{"Multiple   spaces\tand\ntabs", "multiple spaces and tabs"},
		{"Punctuation!? (parens) [brackets]", "punctuation parens brackets"},
		{"Unicode: café à la mode", "unicode café à la mode"},
		{"Numbers 123 stay", "numbers 123 stay"},
		{"", ""},
		{"!!!", ""},
		{"Hyphen-ated and em—dash", "hyphen ated and em dash"},
	}
	for _, c := range cases {
		if got := NormalizeTitle(c.in); got != c.want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
