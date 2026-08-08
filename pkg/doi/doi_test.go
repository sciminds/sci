package doi

import "testing"

func TestStripSubobject(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Frontiers article-section deep links — both /abstract and /full
		// point at parent-paper subobjects that 404 on OpenAlex.
		{"frontiers abstract", "10.3389/fnhum.2013.00015/abstract", "10.3389/fnhum.2013.00015"},
		{"frontiers full", "10.3389/fpsyg.2014.01427/full", "10.3389/fpsyg.2014.01427"},

		// PLOS subobject suffixes: .tNNN (table), .gNNN (figure), .sNNN (supplement).
		{"plos table", "10.1371/journal.pcbi.1000808.t001", "10.1371/journal.pcbi.1000808"},
		{"plos figure", "10.1371/journal.pone.0034567.g002", "10.1371/journal.pone.0034567"},
		{"plos supplement", "10.1371/journal.pone.0002597.s007", "10.1371/journal.pone.0002597"},

		// PNAS supplements — bare /-/DCSupplemental and the deep-linked file form.
		{"pnas supplement", "10.1073/pnas.0908104107/-/DCSupplemental", "10.1073/pnas.0908104107"},
		{"pnas supplement file", "10.1073/pnas.1005062107/-/DCSupplemental/pnas.201005062SI.pdf", "10.1073/pnas.1005062107"},

		// Controls — must NOT strip. These shapes mimic subobject patterns
		// but live under non-target publisher prefixes or lack the suffix.
		{"non-plos t-suffix", "10.1234/foo.t001", "10.1234/foo.t001"},
		{"frontiers no suffix", "10.3389/fpsyg.2024.123", "10.3389/fpsyg.2024.123"},
		{"nature untouched", "10.1038/nature12373", "10.1038/nature12373"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripSubobject(tc.in)
			if got != tc.want {
				t.Errorf("StripSubobject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsSubobject(t *testing.T) {
	t.Parallel()
	// IsSubobject is defined as StripSubobject(raw) != raw, so anything that
	// changes under the stripper must report true and vice versa.
	cases := []struct {
		raw  string
		want bool
	}{
		{"10.3389/fnhum.2013.00015/abstract", true},
		{"10.1371/journal.pcbi.1000808.t001", true},
		{"10.1073/pnas.0908104107/-/DCSupplemental", true},
		{"10.1038/nature12373", false},
		{"10.1234/foo.t001", false},
		{"", false},
	}
	for _, tc := range cases {
		got := IsSubobject(tc.raw)
		if got != tc.want {
			t.Errorf("IsSubobject(%q) = %v, want %v", tc.raw, got, tc.want)
		}
		// Symmetry guard: IsSubobject must agree with the stripper.
		if got != (StripSubobject(tc.raw) != tc.raw) {
			t.Errorf("IsSubobject(%q) disagrees with StripSubobject", tc.raw)
		}
	}
}
