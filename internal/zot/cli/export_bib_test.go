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
	"os"
	"path/filepath"
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

func TestExport_BibLaTeXOmitsNonBibliographicItems(t *testing.T) {
	dataDir := withOrientConfig(t)
	seedNonBibliographicItems(t, dataDir)
	t.Cleanup(func() { libExportFormat, libExportOut = "", "" })

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
