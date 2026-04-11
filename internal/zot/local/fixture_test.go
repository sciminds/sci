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
		`CREATE TABLE itemTypes (itemTypeID INTEGER PRIMARY KEY, typeName TEXT UNIQUE)`,
		`CREATE TABLE fields (fieldID INTEGER PRIMARY KEY, fieldName TEXT UNIQUE)`,
		`CREATE TABLE itemDataValues (valueID INTEGER PRIMARY KEY, value TEXT UNIQUE)`,
		`CREATE TABLE items (
			itemID INTEGER PRIMARY KEY,
			itemTypeID INTEGER,
			libraryID INTEGER,
			key TEXT,
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
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ddl %q: %w", stmt, err)
		}
	}

	// Seed data.
	seed := []string{
		`INSERT INTO version VALUES ('userdata', 125)`,
		`INSERT INTO libraries VALUES (1, 'user', 1000)`,

		`INSERT INTO itemTypes VALUES (1,'journalArticle'),(2,'book'),(3,'attachment'),(4,'note'),(5,'conferencePaper')`,

		// Minimal field set we actually query.
		`INSERT INTO fields VALUES (1,'title'),(2,'date'),(3,'DOI'),(4,'publicationTitle'),(5,'url'),(6,'abstractNote')`,

		// Creators and types.
		`INSERT INTO creatorTypes VALUES (1,'author'),(2,'editor')`,
		`INSERT INTO creators VALUES
			(1,'Alice','Smith',0),
			(2,'Bob','Jones',0),
			(3,'','NASA',1)`,

		// 3 content items + 1 attachment child + 1 trashed item.
		`INSERT INTO items VALUES
			(10, 1, 1, 'AAAA1111', '2024-01-01 10:00:00', '2024-01-01 10:00:00', '2024-01-01 10:00:00'),
			(20, 1, 1, 'BBBB2222', '2024-02-01 10:00:00', '2024-02-02 10:00:00', '2024-02-02 10:00:00'),
			(30, 2, 1, 'CCCC3333', '2024-03-01 10:00:00', '2024-03-01 10:00:00', '2024-03-01 10:00:00'),
			(40, 3, 1, 'DDDD4444', '2024-01-01 10:05:00', '2024-01-01 10:05:00', '2024-01-01 10:05:00'),
			(50, 5, 1, 'EEEE5555', '2024-04-01 10:00:00', '2024-04-01 10:00:00', '2024-04-01 10:00:00')`,

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
			(11,'https://example.org/abc')`,

		// Item 10 (journalArticle): title, date, DOI, pub, url, abstract.
		`INSERT INTO itemData VALUES
			(10,1,1),(10,2,4),(10,3,6),(10,4,8),(10,5,11),(10,6,10),
			(20,1,3),(20,2,4),(20,4,8),
			(30,1,2),(30,2,5)`,

		`INSERT INTO itemCreators VALUES
			(10,1,1,0),
			(10,2,1,1),
			(20,3,1,0),
			(30,1,1,0)`,

		`INSERT INTO tags VALUES (1,'neuroimaging',0),(2,'deep-learning',0),(3,'cats',0)`,
		`INSERT INTO itemTags VALUES (10,1,0),(10,2,0),(30,3,0)`,

		`INSERT INTO collections VALUES
			(100,'Brain Papers',1,NULL,'COLLAAA1'),
			(101,'Favorites',1,100,'COLLBBB2')`,
		`INSERT INTO collectionItems VALUES (100,10),(100,20),(101,10)`,

		// Item 40 is an attachment child of item 10.
		`INSERT INTO itemAttachments VALUES (40,10,1,'application/pdf','storage:deeplearning.pdf')`,
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
