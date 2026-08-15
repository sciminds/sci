package cli

// A redirected document must be a document.
//
// `sci zot bib paper.qmd --format csl-json > refs.json` (and the same for
// `zot export` / `zot search --export` / `zot item export`) puts human-mode
// output straight into a file. Everything sci has to SAY about the run —
// cite-key stats, skipped items, unresolved references, the missing-sidecar
// caveat — belongs on stderr, or the file it just wrote is not JSON.
//
// These run the real command tree in human mode over the orient fixture and
// parse the captured stdout. BibLaTeX is not checked here because a bibtex
// parser tolerates stray prose — the pollution is real but invisible, which
// is precisely why CSL-JSON is the canary.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamedDocuments_StdoutStaysParseable(t *testing.T) {
	withOrientConfig(t)

	// A document citing one resolvable DOI (KEY1) and one that resolves
	// nowhere, so the unresolved block — the loudest footer — is in play.
	doc := filepath.Join(t.TempDir(), "paper.qmd")
	if err := os.WriteFile(doc, []byte(
		"Cites 10.1038/nature12373 and the unresolvable 10.9999/nope-does-not-exist.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		argv []string
	}{
		{"export", []string{"export", "--library", "personal", "--format", "csl-json"}},
		{"search --export", []string{"search", "--library", "personal", "--export", "--format", "csl-json", "Paper"}},
		{"bib", []string{"bib", "--library", "personal", "--format", "csl-json", doc}},
		{"item export", []string{"item", "export", "--library", "personal", "--format", "csl-json", "KEY1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runOrient(t, tc.argv...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.argv, err, string(out))
			}
			if !json.Valid(out) {
				t.Errorf("stdout of `zot %s` is not valid JSON — a footer leaked into the document:\n%s",
					strings.Join(tc.argv, " "), string(out))
			}
		})
	}
}

// The ndjson mirror has the same contract, one object per line.
func TestStreamedDump_StdoutIsPureNDJSON(t *testing.T) {
	withOrientConfig(t)

	out, err := runOrient(t, "export", "--library", "personal", "--format", "ndjson")
	if err != nil {
		t.Fatalf("export ndjson: %v\n%s", err, string(out))
	}
	for i, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("dump line %d is not a JSON object: %q", i+1, line)
		}
	}
}

// S2: `zot guide` documents `-tag:read` as the way to exclude a tag, and
// `search --help` said the same. Typed as written it never reaches the
// search parser — urfave reads a leading `-` as a flag and refuses the
// command line. The documented form has to be the one that runs.
func TestNegatedClause_DocumentedFormParses(t *testing.T) {
	withOrientConfig(t)

	// The form the docs must teach: everything after `--` is a positional.
	out, err := runOrient(t, "--json", "search", "--library", "personal", "--", "-tag:ml")
	if err != nil {
		t.Fatalf("the documented negation form does not parse: %v\n%s", err, string(out))
	}

	// And the bare form is exactly what the docs must not present as
	// typeable on its own — pinned so the guidance can't silently rot.
	if _, err := runOrient(t, "--json", "search", "--library", "personal", "-tag:ml"); err == nil {
		t.Skip("urfave now accepts a leading-dash positional; the `--` guidance can be relaxed")
	}
}

func TestNegatedClause_DocsShowTheSeparator(t *testing.T) {
	t.Parallel()
	desc := searchCommand().Description
	if !strings.Contains(desc, "sci zot search -- '-tag:") {
		t.Errorf("search --help does not show the typeable negation form:\n%s", desc)
	}

	var mentions bool
	for _, line := range guideContent().Contract {
		if strings.Contains(line, "-tag:") && strings.Contains(line, "search -- ") {
			mentions = true
		}
	}
	if !mentions {
		t.Errorf("zot guide documents -tag: negation without the `--` separator it needs:\n%v",
			guideContent().Contract)
	}
}
