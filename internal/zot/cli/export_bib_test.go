package cli

// Tests for the bibliography half of `zot export` — the citation formats,
// as opposed to the NDJSON mirror covered in export_dump_test.go.
//
// The contract these pin: ListAll is deliberately lossless (it feeds the
// mirror, so it returns standalone attachments, standalone notes, and
// annotations), and the bibliography path must filter them back out. They
// are real Zotero items; they are not references.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// seedNonBibliographicItems adds a standalone attachment and a PDF
// annotation to the orient fixture, which already carries the standalone
// note NOTE0001. None of the three has a title row in itemData — that is
// how they reached the live .bib as titleless `@misc` entries.
func seedNonBibliographicItems(t *testing.T, dataDir string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "zotero.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmts := []string{
		`INSERT INTO itemTypes VALUES (3, 'attachment'), (4, 'annotation')`,
		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified) VALUES
			(8, 3, 1, 'ATTACH01', 1, '2024-07-01 10:00:00', '2024-07-01 10:00:00', '2024-07-01 10:00:00'),
			(9, 4, 1, 'ANNOT001', 1, '2024-07-02 10:00:00', '2024-07-02 10:00:00', '2024-07-02 10:00:00')`,
		// parentItemID NULL — a standalone attachment, the case the mirror
		// filter deliberately keeps and a bibliography must still drop.
		`INSERT INTO itemAttachments (itemID, parentItemID, linkMode, contentType, path, storageHash) VALUES
			(8, NULL, 1, 'application/pdf', 'storage:standalone.pdf', NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

// TestFormatFlagDefaults_StayPerCommand pins the three same-named
// --format flags apart. `zot export` and `zot search --export` default to
// biblatex; `zot item export` defaults to csl-json. Each Action reads the
// value off its own *cli.Command, and urfave resolves a name up the
// command lineage — so a flag renamed onto a parent, or a lookup spelled
// with an alias, would silently hand one command another's default.
func TestFormatFlagDefaults_StayPerCommand(t *testing.T) {
	withOrientConfig(t)

	cases := []struct {
		name string
		argv []string
		want string // the format each result reports having used
	}{
		{"library export", []string{"export", "--library", "personal"}, "biblatex"},
		{"search export", []string{"search", "--library", "personal", "--export", "Paper"}, "biblatex"},
		{"item export", []string{"item", "export", "--library", "personal", "KEY1"}, "csl-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runOrient(t, slices.Concat([]string{"--json"}, tc.argv)...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.argv, err, string(out))
			}
			var result struct {
				Format string `json:"format"`
			}
			if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
				t.Fatalf("parse: %v\n%s", err, string(out))
			}
			if result.Format != tc.want {
				t.Errorf("default --format for %v = %q, want %q", tc.argv, result.Format, tc.want)
			}
		})
	}
}

func TestExport_BibLaTeXOmitsNonBibliographicItems(t *testing.T) {
	dataDir := withOrientConfig(t)
	seedNonBibliographicItems(t, dataDir)

	out := filepath.Join(t.TempDir(), "refs.bib")
	if _, err := runOrient(t, "--json", "export", "--library", "personal",
		"--format", "biblatex", "--out", out); err != nil {
		t.Fatalf("export: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	for _, key := range []string{"NOTE0001", "ATTACH01", "ANNOT001"} {
		if strings.Contains(body, key) {
			t.Errorf("non-bibliographic item %s reached the .bib:\n%s", key, body)
		}
	}
	// The four seeded papers (KEY1..KEY4), and nothing else.
	if n := strings.Count(body, "@"); n != 4 {
		t.Errorf("entry count = %d, want 4 papers:\n%s", n, body)
	}
	// Every entry a consumer sees must be renderable and verifiable.
	if strings.Contains(body, "@misc{") {
		t.Errorf("titleless @misc entry in the .bib:\n%s", body)
	}
}
