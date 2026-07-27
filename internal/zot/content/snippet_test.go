package content

import "testing"

func TestEchoesTitle(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		title   string
		want    bool
	}{
		{
			// The live case: docling opens the body with the title as an H1,
			// so FTS5's best match for a title word is the title itself.
			name:    "snippet is a verbatim slice of the title",
			snippet: "…A review of current methods used in haemodynamic and electrophysiological hyperscanning studies…",
			title:   "Quantification of inter-brain coupling: A review of current methods used in haemodynamic and electrophysiological hyperscanning studies",
			want:    true,
		},
		{
			// Same failure wearing front-matter: a DOI and a markdown heading
			// marker ride along, but every prose word is still the title.
			name:    "title echo carrying DOI and heading markers",
			snippet: "…10.3389/fnhum.2020.00039 ## [Hyperscanning: A Valid Method to Study Neural…",
			title:   "Hyperscanning: A Valid Method to Study Neural Inter-brain Underpinnings of Social Interaction",
			want:    true,
		},
		{
			name:    "genuine body prose is not an echo",
			snippet: "…Here, we used an intersubject correlation approach in fMRI to test the…",
			title:   "On the same wavelength: predictable language enhances speaker-listener brain-to-brain synchrony in posterior superior temporal gyrus",
			want:    false,
		},
		{
			name:    "methods sentence sharing a few title words stays",
			snippet: "…These intersubject correlations (ISCs) occur when the BOLD signal time course from…",
			title:   "Inscapes: A movie paradigm to improve compliance in functional magnetic resonance imaging",
			want:    false,
		},
		{
			name:    "empty snippet is not an echo (nothing to suppress)",
			snippet: "",
			title:   "Anything",
			want:    false,
		},
		{
			name:    "no title means we cannot judge — keep the snippet",
			snippet: "…some body text…",
			title:   "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EchoesTitle(tt.snippet, tt.title); got != tt.want {
				t.Errorf("EchoesTitle(%q, %q) = %v, want %v", tt.snippet, tt.title, got, tt.want)
			}
		})
	}
}

// The suppression must survive the marks callers wrap matches in — a snippet
// is no less an echo for having <b> around the matched term.
func TestEchoesTitle_IgnoresMatchMarks(t *testing.T) {
	snippet := "…A review of current <b>hyperscanning</b> studies…"
	title := "A review of current hyperscanning studies"
	if !EchoesTitle(snippet, title) {
		t.Error("match marks defeated echo detection")
	}
}
