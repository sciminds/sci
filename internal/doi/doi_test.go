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

		// eLife asset DOIs. Every figure, table and peer-review artifact of
		// an eLife paper gets its own .NNN child: doi.org types
		// 10.7554/eLife.17089.001 as a "component" titled "Abstract", and
		// 10.7554/elife.01867.014 as a "peer-review" author response.
		// Neither is the paper, and OpenAlex indexes neither.
		{"elife asset", "10.7554/eLife.17089.001", "10.7554/eLife.17089"},
		{"elife peer review artifact", "10.7554/elife.01867.014", "10.7554/elife.01867"},

		// OUP supplementary data. Same /-/ convention as PNAS, different
		// spelling: Cerebral Cortex writes /-/DC1. doi.org 404s on it while
		// the parent resolves, so the stored DOI names nothing at all.
		{"oup supplement", "10.1093/cercor/bhs367/-/DC1", "10.1093/cercor/bhs367"},
		{"oup supplement bare", "10.1093/cercor/bhw393/-/DC", "10.1093/cercor/bhw393"},

		// PeerJ subobjects — /supp-N, /fig-N, /table-N.
		{"peerj supplement", "10.7717/peerj.1058/supp-1", "10.7717/peerj.1058"},
		{"peerj figure", "10.7717/peerj.4375/fig-2", "10.7717/peerj.4375"},

		// /abstract and /full are a PLATFORM path segment, not a Frontiers
		// one. Wiley's onlinelibrary hosts the same shape, and 15 of the 49
		// in this corpus are Wiley — the anchored Frontiers-only pattern
		// walked straight past them. Both 404 while their parents resolve.
		{"wiley abstract", "10.1111/1468-0297.00399/abstract", "10.1111/1468-0297.00399"},
		{"wiley full", "10.1111/j.1468-0297.2009.02267.x/full", "10.1111/j.1468-0297.2009.02267.x"},
		{"wiley interscience abstract", "10.1002/mrm.1910340409/abstract", "10.1002/mrm.1910340409"},

		// DOIs are case-insensitive by spec and Zotero stores whatever the
		// publisher's page said, so a lowercase PNAS suffix is the same
		// subobject as a capitalized one.
		{"pnas supplement lowercased", "10.1073/pnas.1217854110/-/dcsupplemental", "10.1073/pnas.1217854110"},

		// Controls — must NOT strip. These shapes mimic subobject patterns
		// but live under non-target publisher prefixes or lack the suffix.
		{"non-plos t-suffix", "10.1234/foo.t001", "10.1234/foo.t001"},
		{"elife parent untouched", "10.7554/eLife.17089", "10.7554/eLife.17089"},
		{"peerj parent untouched", "10.7717/peerj.1058", "10.7717/peerj.1058"},
		{"non-platform abstract suffix", "10.1234/journal/abstract", "10.1234/journal/abstract"},
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
		{"10.7554/eLife.17089.001", true},
		{"10.1093/cercor/bhs367/-/DC1", true},
		{"10.7717/peerj.1058/supp-1", true},
		{"10.1111/1468-0297.00399/abstract", true},
		{"10.1038/nature12373", false},
		{"10.7554/eLife.17089", false},
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
