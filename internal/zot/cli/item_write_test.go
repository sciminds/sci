package cli

// `sci zot item update / delete / attach`, driven through the production
// tree against the fake Zotero. Package globals are swapped, so no
// t.Parallel().

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
)

// --- update ---------------------------------------------------------------

func TestItemUpdate_SinglePatchesAndReturnsTheServerCopy(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle", "title": "Old", "extra": "Keep me?", "volume": "1",
	})

	out, err := runWrite(t, "--json", "item", "update", "ITEM0001",
		"--title", "New title", "--extra", "", "--field", "volume=3", "--publication", "Nature")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "updated" || res.Kind != "item" || res.Target != "ITEM0001" {
		t.Errorf("got %+v", res)
	}
	got := fx.fake.item("ITEM0001").Data
	if got["title"] != "New title" || got["extra"] != "" || got["volume"] != "3" || got["publicationTitle"] != "Nature" {
		t.Errorf("server data = %v", got)
	}
	data := writeData(t, out)
	item, _ := data["item"].(map[string]any)
	inner, _ := item["data"].(map[string]any)
	if inner["title"] != "New title" {
		t.Errorf("data.item should be the item after the patch: %v", data)
	}
	if _, ok := data["type_change"]; ok {
		t.Errorf("no --type, so no type_change: %v", data)
	}
}

// TestItemUpdate_EmptyAuthorClearsCreators: Zotero has no "remove all
// creators" flag, so an explicit empty --author is the way to say it.
func TestItemUpdate_EmptyAuthorClearsCreators(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle",
		"creators": []any{map[string]any{"creatorType": "author", "lastName": "Gone", "firstName": "A"}},
	})

	if _, err := runWrite(t, "--json", "item", "update", "ITEM0001", "--author", ""); err != nil {
		t.Fatalf("update --author \"\": %v", err)
	}
	creators, ok := fx.fake.item("ITEM0001").Data["creators"].([]any)
	if !ok || len(creators) != 0 {
		t.Errorf("creators = %v, want an empty list, never a creator with an empty name", fx.fake.item("ITEM0001").Data["creators"])
	}
}

// TestItemUpdate_TypeChangeClearsFieldsTheNewTypeLacks: Zotero rejects a
// PATCH carrying a field the new type does not declare, so the fields the
// old type held and the new one lacks are cleared in the same write and
// reported.
func TestItemUpdate_TypeChangeClearsFieldsTheNewTypeLacks(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle", "title": "T", "publicationTitle": "Nature", "issue": "3", "DOI": "", "volume": "",
	})

	out, err := runWrite(t, "--json", "item", "update", "ITEM0001", "--type", "book")
	if err != nil {
		t.Fatalf("update --type: %v\n%s", err, out)
	}
	got := fx.fake.item("ITEM0001").Data
	if got["itemType"] != "book" || got["publicationTitle"] != "" || got["issue"] != "" || got["title"] != "T" {
		t.Errorf("server data = %v, want the journal-only fields cleared and title kept", got)
	}
	data := writeData(t, out)
	change, _ := data["type_change"].(map[string]any)
	if change["from"] != "journalArticle" || change["to"] != "book" {
		t.Errorf("type_change = %v", change)
	}
	if dropped := anyStrings(change["dropped_fields"]); !slices.Equal(dropped, []string{"publicationTitle", "issue"}) {
		t.Errorf("dropped_fields = %v, want the non-empty fields book lacks, in schema order (empty DOI/volume not reported)", dropped)
	}
	if !strings.Contains(decodeWrite(t, out).Message, "publicationTitle") {
		t.Errorf("Message should name what was dropped: %q", decodeWrite(t, out).Message)
	}
}

func TestItemUpdate_AuthorsReplaceTheCreatorList(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle",
		"creators": []any{map[string]any{"creatorType": "author", "lastName": "Gone", "firstName": "A"}},
	})

	if _, err := runWrite(t, "--json", "item", "update", "ITEM0001", "--author", "Smith, Alice", "--creator", "editor:Doe, Jane"); err != nil {
		t.Fatalf("update --author: %v", err)
	}
	creators, _ := fx.fake.item("ITEM0001").Data["creators"].([]any)
	if len(creators) != 2 {
		t.Fatalf("creators = %v, want the two given and nothing else", creators)
	}
	first, _ := creators[0].(map[string]any)
	if first["lastName"] != "Smith" || first["firstName"] != "Alice" {
		t.Errorf("creators[0] = %v, want Smith, Alice as one author", first)
	}
}

func TestItemUpdate_TypeChangeValidatesAgainstTheNewType(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{"itemType": "journalArticle", "title": "T"})

	if _, err := runWrite(t, "--json", "item", "update", "ITEM0001", "--type", "book", "--field", "ISBN=123"); err != nil {
		t.Fatalf("update --type: %v", err)
	}
	got := fx.fake.item("ITEM0001").Data
	if got["itemType"] != "book" || got["ISBN"] != "123" {
		t.Errorf("server data = %v", got)
	}
	// A venue on a type without one is refused before anything is sent.
	_, err := runWrite(t, "--json", "item", "update", "ITEM0001", "--type", "book", "--publication", "Nature")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage: %v", coded.Code, err)
	}
}

func TestItemUpdate_UsageErrors(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{"itemType": "journalArticle"})
	cases := map[string][]string{
		"nothing to change": {"item", "update", "ITEM0001"},
		"no key":            {"item", "update", "--title", "X"},
		"unknown field":     {"item", "update", "ITEM0001", "--field", "bogus=1"},
		"owned field":       {"item", "update", "ITEM0001", "--field", "title=X"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runWrite(t, slices.Concat([]string{"--json"}, args)...)
			if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want usage: %v", coded.Code, err)
			}
			if n := len(fx.fake.requestsMatching("PATCH ")); n != 0 {
				t.Errorf("a usage error must not patch, saw %d", n)
			}
		})
	}
}

func TestItemUpdate_BatchReportsPerKey(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{"itemType": "journalArticle"})
	fx.fake.seedItemData("ITEM0002", map[string]any{"itemType": "book"})

	out, err := runWrite(t, "--json", "item", "update", "ITEM0001", "GONE0000", "ITEM0002", "--date", "2021")
	if err != nil {
		t.Fatalf("batch update: %v\n%s", err, out)
	}
	res := decodeBulk(t, out)
	if res.Action != "updated" || res.Total != 3 || !slices.Equal(res.Success, []string{"ITEM0001", "ITEM0002"}) {
		t.Errorf("got %+v", res)
	}
	if msg, ok := res.Failed["GONE0000"]; !ok || !strings.Contains(msg, "not found") {
		t.Errorf("Failed = %v", res.Failed)
	}
	if fx.fake.item("ITEM0002").Data["date"] != "2021" {
		t.Error("ITEM0002 was not patched")
	}
}

// --- delete ---------------------------------------------------------------

func TestItemDelete_JSONWithoutYesNeedsConsent(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")

	_, err := runWrite(t, "--json", "item", "delete", "ITEM0001")
	if !errors.Is(err, cmdutil.ErrNeedsConsent) {
		t.Fatalf("want ErrNeedsConsent, got %T: %v", err, err)
	}
	if fx.fake.item("ITEM0001").Data["deleted"] != nil {
		t.Error("item was trashed without consent")
	}
}

func TestItemDelete_WithYesTrashes(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "item", "delete", "ITEM0001", "--yes")
	if err != nil {
		t.Fatalf("delete --yes: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "trashed" || res.Kind != "item" || res.Target != "ITEM0001" {
		t.Errorf("got %+v", res)
	}
	if got := fx.fake.item("ITEM0001").Data["deleted"]; got != float64(1) {
		t.Errorf("deleted = %v, want 1 (the trash is item state, never a DELETE)", got)
	}
	if n := len(fx.fake.requestsMatching("DELETE ")); n != 0 {
		t.Errorf("saw %d DELETE requests; delete must be the trash PATCH", n)
	}
}

func TestItemDelete_TrashAliasAndAlreadyTrashed(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{"itemType": "journalArticle", "deleted": 1})

	out, err := runWrite(t, "--json", "item", "trash", "ITEM0001", "--yes")
	if err != nil {
		t.Fatalf("trash: %v", err)
	}
	if res := decodeWrite(t, out); res.Action != "unchanged" || !strings.Contains(res.Message, "already") {
		t.Errorf("got %+v, want an unchanged result", res)
	}
}

func TestItemDelete_PromptNamesTitlesFromTheMirror(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalItemWithDOI(t, 7, "ITEM0007", "Deep sleep", "10.1038/x")
	fx.fake.seedItem("ITEM0007")
	fx.fake.seedItem("ITEM0008")

	_, err := runWrite(t, "--json", "item", "delete", "ITEM0007", "ITEM0008")
	coded := codedErr(t, err)
	if !strings.Contains(coded.Message, "Deep sleep") || !strings.Contains(coded.Message, "ITEM0008") {
		t.Errorf("prompt should name each item, with the title when the mirror knows it: %q", coded.Message)
	}
}

func TestItemDelete_BatchReportsUnchanged(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")
	fx.fake.seedItemData("ITEM0002", map[string]any{"itemType": "journalArticle", "deleted": 1})

	out, err := runWrite(t, "--json", "item", "delete", "ITEM0001", "ITEM0002", "GONE0000", "--yes")
	if err != nil {
		t.Fatalf("batch delete: %v\n%s", err, out)
	}
	res := decodeBulk(t, out)
	if res.Action != "trashed" || !slices.Equal(res.Success, []string{"ITEM0001", "ITEM0002"}) || len(res.Failed) != 1 {
		t.Errorf("got %+v", res)
	}
	data, _ := res.Data.(map[string]any)
	if unchanged, _ := data["unchanged"].([]any); len(unchanged) != 1 || unchanged[0] != "ITEM0002" {
		t.Errorf("data.unchanged = %v, want [ITEM0002]", data["unchanged"])
	}
}

// --- attach ---------------------------------------------------------------

func TestItemAttach_CreatesTheChildAndUploads(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")
	path := writePDF(t, t.TempDir(), "paper.pdf")

	out, err := runWrite(t, "--json", "item", "attach", "ITEM0001", path)
	if err != nil {
		t.Fatalf("attach: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "attached" || res.Kind != "attachment" || res.Target != "ITEM0001" {
		t.Errorf("got %+v, want Target = the item key (attachment_key rides data, as for item add)", res)
	}
	att := fx.fake.item("NEW00001")
	if att == nil || att.Data["parentItem"] != "ITEM0001" || att.Data["filename"] != "paper.pdf" || att.Data["contentType"] != "application/pdf" {
		t.Errorf("attachment = %+v", att)
	}
	if len(fx.fake.uploadedBodies()) != 1 {
		t.Errorf("S3 saw %d uploads, want 1", len(fx.fake.uploadedBodies()))
	}
	data := writeData(t, out)
	if data["uploaded"] != true || data["attachment_key"] != "NEW00001" {
		t.Errorf("data = %v, want uploaded:true and attachment_key", data)
	}
	if att, _ := data["attachment"].(map[string]any); att["key"] != "NEW00001" {
		t.Errorf("data.attachment = %v, want the server object", data["attachment"])
	}
}

func TestItemAttach_SkipExistingIsUnchanged(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")
	path := writePDF(t, t.TempDir(), "paper.pdf")
	sum := md5.Sum([]byte("%PDF-1.4 paper.pdf"))
	fx.fake.seedAttachment("ATTACH01", "ITEM0001", strings.ToUpper(hex.EncodeToString(sum[:])))

	out, err := runWrite(t, "--json", "item", "attach", "ITEM0001", path, "--skip-existing")
	if err != nil {
		t.Fatalf("attach --skip-existing: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "unchanged" || !strings.Contains(res.Message, "already attached") {
		t.Errorf("got %+v", res)
	}
	if n := len(fx.fake.requestsMatching("POST ")); n != 0 {
		t.Errorf("--skip-existing must not write when the md5 is already there, saw %d POSTs", n)
	}
}

func TestItemAttach_ServerDedupReportsUploadedFalse(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")
	fx.fake.uploadExists = true
	path := writePDF(t, t.TempDir(), "paper.pdf")

	out, err := runWrite(t, "--json", "item", "attach", "ITEM0001", path)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if data := writeData(t, out); data["uploaded"] != false {
		t.Errorf("data.uploaded = %v, want false on Zotero's dedup short-circuit", data["uploaded"])
	}
	if len(fx.fake.uploadedBodies()) != 0 {
		t.Error("S3 must see nothing on the short-circuit")
	}
}

func TestItemAttach_UsageErrors(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")
	for name, args := range map[string][]string{
		"missing file":  {"item", "attach", "ITEM0001"},
		"no such file":  {"item", "attach", "ITEM0001", "/nowhere/paper.pdf"},
		"too many args": {"item", "attach", "ITEM0001", "a.pdf", "b.pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runWrite(t, slices.Concat([]string{"--json"}, args)...)
			if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want usage: %v", coded.Code, err)
			}
		})
	}
}
