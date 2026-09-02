package cli

// The collection write verbs, driven through the production command tree
// against a fake Zotero (fakezotero_test.go) and a seeded local mirror. Every
// test here swaps package globals (the API client seam, the online probe,
// stdin), so none of them may t.Parallel().

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

// writeFixture is a configured, credentialed, "online" personal library
// with a fake server behind it.
type writeFixture struct {
	dataDir string
	fake    *fakeZotero
}

func newWriteFixture(t *testing.T) *writeFixture {
	t.Helper()
	dataDir := withTestConfig(t, "42", "6506098")
	saveWithCredential(t, dataDir)
	withOnlineProbe(t)
	t.Setenv("SCI_ASSUME", "")
	return &writeFixture{dataDir: dataDir, fake: installFakeZotero(t)}
}

// seedLocalCollections mirrors a few collections into the local database:
// two named alike (case apart) so name resolution has an ambiguity to
// report, one nested under another so the sub-collection count and the
// parent-by-name paths have something to find, and one whose NAME is shaped
// like a key (MYPAPERS) so the key-or-name rule has a real collision.
func (fx *writeFixture) seedLocalCollections(t *testing.T) {
	t.Helper()
	fx.exec(t,
		`INSERT INTO collections (collectionID, collectionName, libraryID, parentCollectionID, key) VALUES
			(1, 'Reading List', 1, NULL, 'COLL0001'),
			(2, 'Projects', 1, NULL, 'COLL0002'),
			(3, 'Archive', 1, 2, 'COLL0003'),
			(4, 'archive', 1, NULL, 'COLL0004'),
			(5, 'Old', 1, 3, 'COLL0005'),
			(6, 'MYPAPERS', 1, NULL, 'COLL0006')`,
	)
}

// seedLocalItem adds one item with a cite key so a positional that is not
// an 8-char key has something to resolve against.
func (fx *writeFixture) seedLocalItem(t *testing.T, itemID int, key, citekey string) {
	t.Helper()
	fx.exec(t,
		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified)
			VALUES (`+strconv.Itoa(itemID)+`, 1, 1, '`+key+`', 1, '2024-01-01 10:00:00', '2024-01-01 10:00:00', '2024-01-01 10:00:00')`,
	)
	seedCitekey(t, fx.dataDir, itemID, citekey)
}

func (fx *writeFixture) exec(t *testing.T, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(fx.dataDir, "zotero.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
}

// runWrite runs argv through the production tree the way cmd/sci mounts it,
// including the Before that arms quiet mode under --json — that wiring is
// what makes a confirmation refuse instead of reaching for a TTY.
func runWrite(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	var jsonFlag bool
	root := &cli.Command{
		Name:  "zot",
		Flags: append([]cli.Flag{cmdutil.JSONFlag(&jsonFlag)}, PersistentFlags()...),
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			return ValidateLibraryBefore(ctx, cmd)
		},
		Commands: Commands(),
	}
	t.Cleanup(func() { uikit.SetQuiet(false) })
	read, restore := captureStdout(t)
	defer restore()
	runErr := root.Run(context.Background(), slices.Concat([]string{"zot", "--library", "personal"}, args))
	return []byte(read()), runErr
}

func decodeWrite(t *testing.T, out []byte) zot.WriteResult {
	t.Helper()
	var res zot.WriteResult
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("decode WriteResult: %v\nraw: %s", err, out)
	}
	return res
}

func decodeBulk(t *testing.T, out []byte) zot.BulkWriteResult {
	t.Helper()
	var res zot.BulkWriteResult
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("decode BulkWriteResult: %v\nraw: %s", err, out)
	}
	return res
}

// --- resolveCollectionRef -------------------------------------------------

func TestResolveCollectionRef(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	db, err := local.Open(fx.dataDir, local.ForPersonal())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	t.Run("key-shaped string matching neither key nor name is an unsynced key", func(t *testing.T) {
		key, coll, err := resolveCollectionRef(db, "NOTSYNC1")
		if err != nil || key != "NOTSYNC1" || coll != nil {
			t.Errorf("got (%q, %v, %v)", key, coll, err)
		}
	})
	t.Run("key-shaped NAME resolves by name when no collection has it as a key", func(t *testing.T) {
		key, coll, err := resolveCollectionRef(db, "MYPAPERS")
		if err != nil || key != "COLL0006" || coll == nil || coll.Name != "MYPAPERS" {
			t.Errorf("got (%q, %+v, %v) — a collection named like a key must still be addressable by name", key, coll, err)
		}
	})
	t.Run("key-shaped name matches case-insensitively too", func(t *testing.T) {
		key, _, err := resolveCollectionRef(db, "mypapers")
		if err != nil || key != "COLL0006" {
			t.Errorf("got (%q, %v)", key, err)
		}
	})
	t.Run("key present in the mirror carries its row", func(t *testing.T) {
		key, coll, err := resolveCollectionRef(db, "COLL0001")
		if err != nil || key != "COLL0001" || coll == nil || coll.Name != "Reading List" {
			t.Errorf("got (%q, %+v, %v)", key, coll, err)
		}
	})
	t.Run("name matches case-insensitively when unique", func(t *testing.T) {
		key, coll, err := resolveCollectionRef(db, "reading list")
		if err != nil || key != "COLL0001" || coll == nil {
			t.Errorf("got (%q, %+v, %v)", key, coll, err)
		}
	})
	t.Run("unknown name is not-found and points at the listing", func(t *testing.T) {
		_, _, err := resolveCollectionRef(db, "Nowhere")
		coded := codedErr(t, err)
		if coded.Code != cmdutil.CodeNotFound {
			t.Errorf("Code = %q, want not-found", coded.Code)
		}
		if !strings.Contains(coded.Try, "sci zot collection list") || !strings.Contains(coded.Try, "8-character key") {
			t.Errorf("Try = %q", coded.Try)
		}
	})
	t.Run("several matches is ambiguous and lists each candidate", func(t *testing.T) {
		_, _, err := resolveCollectionRef(db, "ARCHIVE")
		coded := codedErr(t, err)
		if coded.Code != cmdutil.CodeAmbiguous {
			t.Errorf("Code = %q, want ambiguous", coded.Code)
		}
		for _, want := range []string{"COLL0003", "COLL0004", "COLL0002"} {
			if !strings.Contains(coded.Message, want) {
				t.Errorf("Message should name %s (keys and parents): %q", want, coded.Message)
			}
		}
	})
}

// --- create ---------------------------------------------------------------

func TestCollectionCreate_TopLevel(t *testing.T) {
	fx := newWriteFixture(t)

	out, err := runWrite(t, "--json", "collection", "create", "New Papers")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "created" || res.Kind != "collection" || res.Target != "NEW00001" {
		t.Errorf("got %+v", res)
	}
	created := fx.fake.collection("NEW00001")
	if created == nil || created.Data["name"] != "New Papers" || created.Data["parentCollection"] != false {
		t.Errorf("server copy = %+v", created)
	}
	// The write's answer is the server's copy, in the API's lowercase shape.
	data, _ := res.Data.(map[string]any)
	if data["key"] != "NEW00001" || data["version"] == nil {
		t.Errorf("data should be the server object: %+v", res.Data)
	}
}

func TestCollectionCreate_ParentByName(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)

	out, err := runWrite(t, "--json", "collection", "create", "Drafts", "--parent", "projects")
	if err != nil {
		t.Fatalf("create --parent: %v\n%s", err, out)
	}
	created := fx.fake.collection("NEW00001")
	if created == nil || created.Data["parentCollection"] != "COLL0002" {
		t.Errorf("server copy = %+v, want parent COLL0002", created)
	}
}

func TestCollectionCreate_EmptyNameIsUsage(t *testing.T) {
	newWriteFixture(t)
	_, err := runWrite(t, "--json", "collection", "create", "  ")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage", coded.Code)
	}
}

// TestCollectionCreate_SeveralPositionalsIsUsage: an unquoted two-word name
// is a usage error, not a silently joined "Drafts Projects".
func TestCollectionCreate_SeveralPositionalsIsUsage(t *testing.T) {
	fx := newWriteFixture(t)
	_, err := runWrite(t, "--json", "collection", "create", "Drafts", "Projects")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage: %v", coded.Code, err)
	}
	if fx.fake.requestCount() != 0 {
		t.Error("a usage error must not reach the server")
	}
}

// TestCollectionCreate_RefusedIsCodeRuntime: a failed slot in the POST
// response is Zotero refusing the object, reported with its own message.
func TestCollectionCreate_RefusedIsCodeRuntime(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.failNextWrite = "Collection name too long"

	_, err := runWrite(t, "--json", "collection", "create", "X")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeRuntime || !strings.Contains(coded.Message, "Collection name too long") {
		t.Errorf("got %+v", coded)
	}
}

// --- update ---------------------------------------------------------------

func TestCollectionUpdate_Rename(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0001", "Reading List", "")

	out, err := runWrite(t, "--json", "collection", "update", "Reading List", "--name", "To Read")
	if err != nil {
		t.Fatalf("update --name: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "updated" || res.Target != "COLL0001" {
		t.Errorf("got %+v", res)
	}
	if res.Message != "updated collection To Read (COLL0001)" {
		t.Errorf("Message = %q, want the collection labelled by its NEW name", res.Message)
	}
	if got := fx.fake.collection("COLL0001").Data["name"]; got != "To Read" {
		t.Errorf("server name = %v, want To Read", got)
	}
	data, _ := res.Data.(map[string]any)
	inner, _ := data["data"].(map[string]any)
	if inner["name"] != "To Read" {
		t.Errorf("data should be the collection after the patch: %+v", res.Data)
	}
}

func TestCollectionUpdate_ReparentByKey(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0001", "Reading List", "")

	if _, err := runWrite(t, "--json", "collection", "update", "COLL0001", "--parent", "COLL0002"); err != nil {
		t.Fatalf("update --parent: %v", err)
	}
	if got := fx.fake.collection("COLL0001").Data["parentCollection"]; got != "COLL0002" {
		t.Errorf("server parent = %v, want COLL0002", got)
	}
}

func TestCollectionUpdate_TopLevel(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0003", "Archive", "COLL0002")

	if _, err := runWrite(t, "--json", "collection", "update", "COLL0003", "--top-level"); err != nil {
		t.Fatalf("update --top-level: %v", err)
	}
	if got := fx.fake.collection("COLL0003").Data["parentCollection"]; got != false {
		t.Errorf("server parent = %v, want false", got)
	}
}

func TestCollectionUpdate_UsageErrors(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0001", "Reading List", "")

	cases := map[string][]string{
		"nothing to change":        {"collection", "update", "COLL0001"},
		"parent and top-level":     {"collection", "update", "COLL0001", "--parent", "COLL0002", "--top-level"},
		"collection as own parent": {"collection", "update", "COLL0001", "--parent", "reading list"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runWrite(t, slices.Concat([]string{"--json"}, args)...)
			if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want usage: %v", coded.Code, err)
			}
			if fx.fake.requestCount() != 0 {
				t.Error("a usage error must not reach the server")
			}
		})
	}
}

// TestCollectionUpdate_ConflictIsCodeConflict: a 412 that survives the
// client's one re-read is contention, reported as a conflict to re-run.
func TestCollectionUpdate_ConflictIsCodeConflict(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0001", "Reading List", "")
	fx.fake.setConflicts(2)

	_, err := runWrite(t, "--json", "collection", "update", "COLL0001", "--name", "X")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeConflict || !strings.Contains(coded.Message, "re-run") {
		t.Errorf("got %+v", coded)
	}
}

// --- delete ---------------------------------------------------------------

func TestCollectionDelete_WithYes(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0002", "Projects", "")

	out, err := runWrite(t, "--json", "collection", "delete", "Projects", "--yes")
	if err != nil {
		t.Fatalf("delete --yes: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "deleted" || res.Kind != "collection" || res.Target != "COLL0002" {
		t.Errorf("got %+v", res)
	}
	if fx.fake.collection("COLL0002") != nil {
		t.Error("collection still on the server")
	}
}

// TestCollectionDelete_JSONWithoutYesNeedsConsent: --json is quiet mode, and
// a prompt nobody can answer is a refusal (exit 2), never a delete.
func TestCollectionDelete_JSONWithoutYesNeedsConsent(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0002", "Projects", "")

	_, err := runWrite(t, "--json", "collection", "delete", "COLL0002")
	if !errors.Is(err, cmdutil.ErrNeedsConsent) {
		t.Fatalf("want ErrNeedsConsent, got %T: %v", err, err)
	}
	if fx.fake.collection("COLL0002") == nil {
		t.Error("collection was deleted without consent")
	}
}

func TestCollectionDelete_PromptNamesSubCollections(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedCollection("COLL0002", "Projects", "")

	_, err := runWrite(t, "--json", "collection", "delete", "COLL0002")
	coded := codedErr(t, err)
	// COLL0002 → COLL0003 → COLL0005: two descendants go with it.
	if !strings.Contains(coded.Message, "2 sub-collection") {
		t.Errorf("prompt should count the sub-collections Zotero deletes with it: %q", coded.Message)
	}
}

// --- add / remove ---------------------------------------------------------

func TestCollectionAdd_SingleByKey(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "collection", "add", "ITEM0001", "COLL0001")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "added" || res.Kind != "item" || res.Target != "ITEM0001" {
		t.Errorf("got %+v", res)
	}
	if res.Message != "added item ITEM0001 to collection Reading List (COLL0001)" {
		t.Errorf("Message = %q", res.Message)
	}
	if got := fx.fake.itemCollections("ITEM0001"); !slices.Equal(got, []string{"COLL0001"}) {
		t.Errorf("server collections = %v", got)
	}
}

func TestCollectionAdd_AlreadyMemberSaysSo(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001", "COLL0001")

	out, err := runWrite(t, "--json", "collection", "add", "ITEM0001", "COLL0001")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	res := decodeWrite(t, out)
	if !strings.Contains(res.Message, "already") {
		t.Errorf("Message should say nothing changed: %q", res.Message)
	}
	if fx.fake.item("ITEM0001").Version != 10 {
		t.Error("an already-member add must not write")
	}
}

func TestCollectionAdd_ByCiteKey(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.seedLocalItem(t, 1, "ITEM0001", "smith2024-paper")
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "collection", "add", "smith2024-paper", "Reading List")
	if err != nil {
		t.Fatalf("add by cite key: %v\n%s", err, out)
	}
	if res := decodeWrite(t, out); res.Target != "ITEM0001" {
		t.Errorf("Target = %q, want the resolved key", res.Target)
	}
}

func TestCollectionAdd_UnresolvableItemIsUsage(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)

	_, err := runWrite(t, "--json", "collection", "add", "not-a-key", "COLL0001")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage: %v", coded.Code, err)
	}
}

// TestCollectionAdd_FromStdin_PartialBatch: the batch form keeps going past
// a failing item and reports per key; an already-member is a success that
// changed nothing, and the run exits 0.
func TestCollectionAdd_FromStdin_PartialBatch(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001", "COLL0001")
	fx.fake.seedItem("ITEM0002")

	prev := collAddStdin
	collAddStdin = strings.NewReader("ITEM0001\n# a comment\n\nITEM0002\nGONE0000\n")
	t.Cleanup(func() { collAddStdin = prev })

	out, err := runWrite(t, "--json", "collection", "add", "--from-file", "-", "COLL0001")
	if err != nil {
		t.Fatalf("batch add: %v\n%s", err, out)
	}
	res := decodeBulk(t, out)
	if res.Action != "added" || res.Kind != "item" || res.Total != 3 {
		t.Errorf("got %+v", res)
	}
	if !slices.Equal(res.Success, []string{"ITEM0001", "ITEM0002"}) {
		t.Errorf("Success = %v", res.Success)
	}
	if msg, ok := res.Failed["GONE0000"]; !ok || !strings.Contains(msg, "not found") {
		t.Errorf("Failed = %v, want GONE0000 reported as not found", res.Failed)
	}
	data, _ := res.Data.(map[string]any)
	if unchanged, _ := data["unchanged"].([]any); len(unchanged) != 1 || unchanged[0] != "ITEM0001" {
		t.Errorf("data.unchanged = %v, want [ITEM0001]", data["unchanged"])
	}
	if got := fx.fake.itemCollections("ITEM0002"); !slices.Equal(got, []string{"COLL0001"}) {
		t.Errorf("ITEM0002 collections = %v", got)
	}
}

// TestCollectionAdd_BatchUnknownCollectionIsOneNotFound: a collection key
// the mirror lacks is checked on the server ONCE before the batch, so a
// wrong key is a single not-found rather than N per-item failures.
func TestCollectionAdd_BatchUnknownCollectionIsOneNotFound(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001")
	fx.fake.seedItem("ITEM0002")

	prev := collAddStdin
	collAddStdin = strings.NewReader("ITEM0001\nITEM0002\n")
	t.Cleanup(func() { collAddStdin = prev })

	_, err := runWrite(t, "--json", "collection", "add", "--from-file", "-", "NOTSYNC1")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotFound || !strings.Contains(coded.Message, "NOTSYNC1") {
		t.Errorf("got %+v", coded)
	}
	if n := fx.fake.requestCount(); n != 1 {
		t.Errorf("server saw %d requests, want exactly the one collection lookup", n)
	}
	if fx.fake.item("ITEM0001").Version != 10 || fx.fake.item("ITEM0002").Version != 10 {
		t.Error("no item may be written when the collection does not exist")
	}
}

func TestCollectionAdd_FromFileWithPositionalItemsIsUsage(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	prev := collAddStdin
	collAddStdin = strings.NewReader("ITEM0001\n")
	t.Cleanup(func() { collAddStdin = prev })

	_, err := runWrite(t, "--json", "collection", "add", "--from-file", "-", "ITEM0001", "COLL0001")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage: %v", coded.Code, err)
	}
}

func TestCollectionRemove(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001", "COLL0001", "COLL0002")

	out, err := runWrite(t, "--json", "collection", "remove", "ITEM0001", "COLL0001")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "removed" || res.Target != "ITEM0001" {
		t.Errorf("got %+v", res)
	}
	if got := fx.fake.itemCollections("ITEM0001"); !slices.Equal(got, []string{"COLL0002"}) {
		t.Errorf("server collections = %v, want only COLL0002 left", got)
	}
}

func TestCollectionRemove_NotAMemberSaysSo(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "collection", "remove", "ITEM0001", "COLL0001")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if res := decodeWrite(t, out); !strings.Contains(res.Message, "not in") {
		t.Errorf("Message should say the item was not a member: %q", res.Message)
	}
}

// --- the door and the error mapping --------------------------------------

func TestCollectionWrite_NameNotFound(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)

	_, err := runWrite(t, "--json", "collection", "delete", "Nowhere", "--yes")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeNotFound {
		t.Errorf("Code = %q, want not-found: %v", coded.Code, err)
	}
}

func TestCollectionWrite_NameAmbiguous(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001")

	_, err := runWrite(t, "--json", "collection", "add", "ITEM0001", "archive")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeAmbiguous {
		t.Errorf("Code = %q, want ambiguous: %v", coded.Code, err)
	}
	if fx.fake.requestCount() != 0 {
		t.Error("an ambiguous target must not reach the server")
	}
}

func TestCollectionWrite_LibraryAllRefused(t *testing.T) {
	newWriteFixture(t)
	_, err := runWrite(t, "--json", "collection", "create", "X", "--library", "all")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeUsage || !strings.Contains(coded.Try, "--library personal") {
		t.Errorf("got %+v", coded)
	}
}

func TestCollectionWrite_ServerNotFoundIsCodeNotFound(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)

	_, err := runWrite(t, "--json", "collection", "update", "NOTSYNC1", "--name", "X")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotFound || !strings.Contains(coded.Message, "NOTSYNC1") {
		t.Errorf("got %+v", coded)
	}
}

func TestCollectionWrite_ForbiddenNamesTheSetupFix(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.forbidden = true

	_, err := runWrite(t, "--json", "collection", "create", "X")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotConfigured {
		t.Errorf("Code = %q, want not-configured", coded.Code)
	}
	// The remedy carries a <key> placeholder, so it is prose (Try), never a
	// Fix — a Fix must be resubmittable verbatim.
	if coded.Fix != "" || !strings.Contains(coded.Try, "--api-key") {
		t.Errorf("want the --api-key setup command in Try and no Fix, got %+v", coded)
	}
	if !strings.Contains(coded.Message, "cannot write") {
		t.Errorf("Message = %q", coded.Message)
	}
}

// TestCollectionWrite_JSONEnvelopeShape pins the wire shape of a WriteResult
// under --json: the scoped envelope with the library, then action / kind /
// target / message / data.
func TestCollectionWrite_JSONEnvelopeShape(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "collection", "add", "ITEM0001", "COLL0001")
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(unwrapData(t, out), &data); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"library", "action", "kind", "target", "message"} {
		if _, ok := data[key]; !ok {
			t.Errorf("envelope data lacks %q: %s", key, out)
		}
	}
	if data["library"] != "personal" {
		t.Errorf("library = %v", data["library"])
	}
}
