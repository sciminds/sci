package local

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// sharedFixtureDir is lazily populated once per `go test` invocation.
// The fixture is opened read-only/immutable by every test, so there is
// no mutation risk in sharing it across subtests. Rebuilding it per test
// adds ~70ms of DDL + seed work that dominates total local/ suite time.
var (
	sharedFixtureOnce sync.Once
	sharedFixtureDir  string
	sharedFixtureErr  error
)

// buildFixture returns a directory containing a populated zotero.sqlite
// (see seed data below). The same fixture is shared across every test in
// this package — t.TempDir cleanup runs at process exit via TestMain.
//
// The fixture intentionally uses the same table and column names as real
// Zotero so our queries run unmodified. It does NOT claim to cover every
// constraint — we only create what we query.
func buildFixture(t *testing.T) string {
	t.Helper()
	sharedFixtureOnce.Do(func() {
		dir, err := os.MkdirTemp("", "zot-local-fixture-*")
		if err != nil {
			sharedFixtureErr = err
			return
		}
		sharedFixtureDir = dir
		sharedFixtureErr = seedFixture(dir)
	})
	if sharedFixtureErr != nil {
		t.Fatalf("shared fixture: %v", sharedFixtureErr)
	}
	return sharedFixtureDir
}

// TestMain cleans up the shared fixture directory after all tests finish.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedFixtureDir != "" {
		_ = os.RemoveAll(sharedFixtureDir)
	}
	os.Exit(code)
}

// seedFixture builds the zotero.sqlite file inside dir. Split out from
// buildFixture so the sync.Once initializer has a plain func to call.
func seedFixture(dir string) error {
	path := filepath.Join(dir, "zotero.sqlite")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ddl := []string{
		`CREATE TABLE version (schema TEXT PRIMARY KEY, version INTEGER)`,
		`CREATE TABLE libraries (libraryID INTEGER PRIMARY KEY, type TEXT, version INTEGER)`,
		`CREATE TABLE groups (libraryID INTEGER PRIMARY KEY, groupID INTEGER UNIQUE, name TEXT)`,
		`CREATE TABLE itemTypes (itemTypeID INTEGER PRIMARY KEY, typeName TEXT UNIQUE)`,
		`CREATE TABLE fields (fieldID INTEGER PRIMARY KEY, fieldName TEXT UNIQUE)`,
		`CREATE TABLE itemDataValues (valueID INTEGER PRIMARY KEY, value TEXT UNIQUE)`,
		`CREATE TABLE items (
			itemID INTEGER PRIMARY KEY,
			itemTypeID INTEGER,
			libraryID INTEGER,
			key TEXT,
			version INTEGER NOT NULL DEFAULT 0,
			dateAdded TEXT,
			dateModified TEXT,
			clientDateModified TEXT
		)`,
		`CREATE TABLE itemData (
			itemID INTEGER,
			fieldID INTEGER,
			valueID INTEGER,
			PRIMARY KEY (itemID, fieldID)
		)`,
		`CREATE TABLE deletedItems (itemID INTEGER PRIMARY KEY)`,
		`CREATE TABLE creators (
			creatorID INTEGER PRIMARY KEY,
			firstName TEXT,
			lastName TEXT,
			fieldMode INTEGER
		)`,
		`CREATE TABLE creatorTypes (creatorTypeID INTEGER PRIMARY KEY, creatorType TEXT)`,
		`CREATE TABLE itemCreators (
			itemID INTEGER,
			creatorID INTEGER,
			creatorTypeID INTEGER,
			orderIndex INTEGER,
			PRIMARY KEY (itemID, orderIndex)
		)`,
		`CREATE TABLE tags (tagID INTEGER PRIMARY KEY, name TEXT UNIQUE, type INTEGER)`,
		`CREATE TABLE itemTags (itemID INTEGER, tagID INTEGER, type INTEGER, PRIMARY KEY (itemID, tagID))`,
		`CREATE TABLE collections (
			collectionID INTEGER PRIMARY KEY,
			collectionName TEXT,
			libraryID INTEGER,
			parentCollectionID INTEGER,
			key TEXT
		)`,
		`CREATE TABLE collectionItems (
			collectionID INTEGER,
			itemID INTEGER,
			PRIMARY KEY (collectionID, itemID)
		)`,
		`CREATE TABLE itemAttachments (
			itemID INTEGER PRIMARY KEY,
			parentItemID INTEGER,
			linkMode INTEGER,
			contentType TEXT,
			path TEXT
		)`,
		`CREATE TABLE itemNotes (
			itemID INTEGER PRIMARY KEY,
			parentItemID INTEGER,
			note TEXT,
			title TEXT
		)`,

		// Fulltext index tables (Zotero's manual word-level FTS).
		`CREATE TABLE fulltextWords (wordID INTEGER PRIMARY KEY, word TEXT UNIQUE)`,
		`CREATE TABLE fulltextItemWords (wordID INT, itemID INT, PRIMARY KEY (wordID, itemID))`,
		`CREATE TABLE fulltextItems (itemID INTEGER PRIMARY KEY, indexedPages INT, totalPages INT, indexedChars INT, totalChars INT)`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ddl %q: %w", stmt, err)
		}
	}

	// Seed data.
	seed := []string{
		`INSERT INTO version VALUES ('userdata', 125)`,
		`INSERT INTO libraries VALUES (1, 'user', 1000), (2, 'group', 500)`,
		// Group membership — local libraryID=2 corresponds to the Zotero
		// Web API groupID 6506098 (sciminds in the real install). Used by
		// ForGroupByAPIID when the CLI --library shared flag is passed.
		`INSERT INTO groups VALUES (2, 6506098, 'sciminds')`,

		`INSERT INTO itemTypes VALUES (1,'journalArticle'),(2,'book'),(3,'attachment'),(4,'note'),(5,'conferencePaper')`,

		// Minimal field set we actually query.
		`INSERT INTO fields VALUES (1,'title'),(2,'date'),(3,'DOI'),(4,'publicationTitle'),(5,'url'),(6,'abstractNote'),(7,'citationKey'),(8,'extra')`,

		// Creators and types.
		`INSERT INTO creatorTypes VALUES (1,'author'),(2,'editor')`,
		`INSERT INTO creators VALUES
			(1,'Alice','Smith',0),
			(2,'Bob','Jones',0),
			(3,'','NASA',1)`,

		// Content items (10, 20, 30) + attachment child (40) + trashed item
		// (50) + standalone attachment (60) + standalone note (70) +
		// note child of item 10 (90).
		// Item 30 is intentionally left uncollected to exercise the
		// uncollected-item orphan check.
		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified) VALUES
			(10, 1, 1, 'AAAA1111', 42, '2024-01-01 10:00:00', '2024-01-01 10:00:00', '2024-01-01 10:00:00'),
			(20, 1, 1, 'BBBB2222', 15, '2024-02-01 10:00:00', '2024-02-02 10:00:00', '2024-02-02 10:00:00'),
			(30, 2, 1, 'CCCC3333', 8,  '2024-03-01 10:00:00', '2024-03-01 10:00:00', '2024-03-01 10:00:00'),
			(40, 3, 1, 'DDDD4444', 3,  '2024-01-01 10:05:00', '2024-01-01 10:05:00', '2024-01-01 10:05:00'),
			(50, 5, 1, 'EEEE5555', 7,  '2024-04-01 10:00:00', '2024-04-01 10:00:00', '2024-04-01 10:00:00'),
			(60, 3, 1, 'ORPHANATT', 2, '2024-05-01 10:00:00', '2024-05-01 10:00:00', '2024-05-01 10:00:00'),
			(70, 4, 1, 'ORPHNNOTE', 1, '2024-05-02 10:00:00', '2024-05-02 10:00:00', '2024-05-02 10:00:00'),
			(80, 1, 1, 'GGGG7777', 5,  '2024-06-01 10:00:00', '2024-06-01 10:00:00', '2024-06-01 10:00:00'),
			(81, 3, 1, 'HHHH8888', 4,  '2024-06-01 10:05:00', '2024-06-01 10:05:00', '2024-06-01 10:05:00'),
			(90, 4, 1, 'NOTECH10', 6,  '2024-01-02 10:00:00', '2024-01-02 10:00:00', '2024-01-02 10:00:00'),
			-- Group-library items (libraryID=2) seed the dual-scope tests.
			-- Keys use a distinct prefix so leak assertions are easy to read.
			(200, 1, 2, 'GRPITEM01', 1, '2024-07-01 10:00:00', '2024-07-01 10:00:00', '2024-07-01 10:00:00'),
			(210, 1, 2, 'GRPITEM02', 1, '2024-07-02 10:00:00', '2024-07-02 10:00:00', '2024-07-02 10:00:00')`,

		// Item 50 is trashed.
		`INSERT INTO deletedItems VALUES (50)`,

		// Values (valueIDs must be unique). Dates use Zotero's authentic
		// "YYYY-MM-DD originalText" dual-encoding for value 4, and a bare
		// year for value 5 — both forms occur in real libraries.
		`INSERT INTO itemDataValues VALUES
			(1,'Deep Learning for Neuroimaging'),
			(2,'A Book About Cats'),
			(3,'Transformers in fMRI Analysis'),
			(4,'2024-03-15 March 15, 2024'),
			(5,'2023'),
			(6,'10.1000/abc123'),
			(7,'10.1000/def456'),
			(8,'NeuroImage'),
			(9,'Nature'),
			(10,'Abstract about brains.'),
			(11,'https://example.org/abc'),
			(12,'smith2024-deeplearneur-AAAA1111'),
			(13,'jonesTransformersFMRIAnalysis2024'),
			(14,'tldr: loose note\nCitation Key: legacyBookKey1900\n'),
			(15,'Attention Mechanisms in Cortical Networks'),
			(100,'Shared Paper One'),
			(101,'Shared Paper Two')`,

		// Item 10 (journalArticle): title, date, DOI, pub, url, abstract,
		// plus a native citationKey matching our v2 spec (canonical).
		// Item 20 (journalArticle): also has a native citationKey but in
		// BBT camelCase style — ScanCiteKeys should surface it so the
		// check layer can flag it as non-canonical.
		// Item 30 (book): no native citationKey, but legacy BBT Citation
		// Key line inside `extra` — exercises the extra-field path.
		`INSERT INTO itemData VALUES
			(10,1,1),(10,2,4),(10,3,6),(10,4,8),(10,5,11),(10,6,10),(10,7,12),
			(20,1,3),(20,2,4),(20,4,8),(20,7,13),
			(30,1,2),(30,2,5),(30,8,14),
			(80,1,15),(80,2,5),
			-- Group-library item titles.
			(200,1,100),
			(210,1,101)`,

		`INSERT INTO itemCreators VALUES
			(10,1,1,0),
			(10,2,1,1),
			(20,3,1,0),
			(30,1,1,0)`,

		// Tag 4 ("orphan-tag") has no itemTags row — unused.
		`INSERT INTO tags VALUES
			(1,'neuroimaging',0),
			(2,'deep-learning',0),
			(3,'cats',0),
			(4,'orphan-tag',0)`,
		`INSERT INTO itemTags VALUES (10,1,0),(10,2,0),(30,3,0)`,

		// Collection 102 ("Empty Box") has no items and no children — orphan.
		`INSERT INTO collections VALUES
			(100,'Brain Papers',1,NULL,'COLLAAA1'),
			(101,'Favorites',1,100,'COLLBBB2'),
			(102,'Empty Box',1,NULL,'EMPTYCOL')`,
		`INSERT INTO collectionItems VALUES (100,10),(100,20),(101,10)`,

		// Item 40 is an attachment child of item 10. Item 60 is a
		// standalone attachment (parentItemID NULL).
		`INSERT INTO itemAttachments VALUES
			(40,10,1,'application/pdf','storage:deeplearning.pdf'),
			(60,NULL,1,'application/pdf','storage:standalone.pdf'),
			(81,80,1,'application/pdf','storage:transformers.pdf')`,

		// Item 70 is a standalone note with no parent.
		// Item 90 is a note child of item 10 (tagged "docling").
		`INSERT INTO itemNotes VALUES
			(70,NULL,'<p>Loose thoughts on attention.</p>','Attention Notes'),
			(90,10,'<p>Extracted via docling.</p>','Extraction Note')`,

		// Fulltext index: words linked to PDF attachment items.
		// Attachment 40 (parent 10): "neuroimaging", "brain", "network", "analysis"
		// Attachment 81 (parent 80): "brain", "cortical", "attention"
		// "brain" appears on both — useful for testing multi-word AND.
		`INSERT INTO fulltextWords VALUES
			(1,'neuroimaging'),(2,'brain'),(3,'network'),(4,'analysis'),
			(5,'cortical'),(6,'attention')`,
		`INSERT INTO fulltextItemWords VALUES
			(1,40),(2,40),(3,40),(4,40),
			(2,81),(5,81),(6,81)`,
		`INSERT INTO fulltextItems VALUES
			(40,10,10,NULL,NULL),
			(81,5,5,NULL,NULL)`,

		// Tag item 90 with "docling" (exercises ListChildren tag path +
		// DoclingNoteKeys).
		`INSERT INTO itemTags VALUES (90,5,0)`,
		`INSERT INTO tags VALUES (5,'docling',0)`,
	}
	for _, stmt := range seed {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("seed %q: %w", stmt, err)
		}
	}

	return nil
}

// sanityCheckFixture reports a brief summary — useful when debugging test drift.
func sanityCheckFixture(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "zotero.sqlite")); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
}
