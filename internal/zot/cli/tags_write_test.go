package cli

// `sci zot tags add / remove`, driven through the production tree against
// the fake Zotero. Package globals are swapped, so no t.Parallel().

import (
	"slices"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
)

// itemTags reads the tag names on a fake item.
func (f *fakeZotero) itemTags(key string) []string {
	obj := f.item(key)
	if obj == nil {
		return nil
	}
	raw, _ := obj.Data["tags"].([]any)
	return lo.FilterMap(raw, func(v any, _ int) (string, bool) {
		entry, ok := v.(map[string]any)
		if !ok {
			return "", false
		}
		name, ok := entry["tag"].(string)
		return name, ok
	})
}

func TestTagsAdd_SingleTag(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "tags", "add", "ITEM0001", "to-read")
	if err != nil {
		t.Fatalf("tags add: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "added" || res.Kind != "tag" || res.Target != "to-read" {
		t.Errorf("got %+v", res)
	}
	if !strings.Contains(res.Message, "ITEM0001") {
		t.Errorf("Message should name the item: %q", res.Message)
	}
	if got := fx.fake.itemTags("ITEM0001"); !slices.Equal(got, []string{"to-read"}) {
		t.Errorf("server tags = %v", got)
	}
}

func TestTagsAdd_AlreadyPresentSaysSo(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle", "tags": []any{map[string]any{"tag": "to-read"}},
	})

	out, err := runWrite(t, "--json", "tags", "add", "ITEM0001", "to-read")
	if err != nil {
		t.Fatalf("tags add: %v", err)
	}
	if res := decodeWrite(t, out); !strings.Contains(res.Message, "already") {
		t.Errorf("Message should say nothing changed: %q", res.Message)
	}
	if fx.fake.item("ITEM0001").Version != 10 {
		t.Error("an already-present tag must not write")
	}
}

func TestTagsAdd_SeveralTagsIsABatchWithUnchanged(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle", "tags": []any{map[string]any{"tag": "b"}},
	})

	out, err := runWrite(t, "--json", "tags", "add", "ITEM0001", "a", "b", "c")
	if err != nil {
		t.Fatalf("tags add batch: %v\n%s", err, out)
	}
	res := decodeBulk(t, out)
	if res.Action != "added" || res.Kind != "tag" || res.Total != 3 || !slices.Equal(res.Success, []string{"a", "b", "c"}) {
		t.Errorf("got %+v", res)
	}
	data, _ := res.Data.(map[string]any)
	if unchanged, _ := data["unchanged"].([]any); len(unchanged) != 1 || unchanged[0] != "b" {
		t.Errorf("data.unchanged = %v, want [b]", data["unchanged"])
	}
	if got := fx.fake.itemTags("ITEM0001"); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Errorf("server tags = %v", got)
	}
}

func TestTagsAdd_ByCiteKey(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalItem(t, 1, "ITEM0001", "smith2024-paper")
	fx.fake.seedItem("ITEM0001")

	out, err := runWrite(t, "--json", "tags", "add", "smith2024-paper", "x")
	if err != nil {
		t.Fatalf("tags add by cite key: %v", err)
	}
	if !strings.Contains(decodeWrite(t, out).Message, "ITEM0001") {
		t.Error("the cite key should resolve to ITEM0001")
	}
}

func TestTagsRemove(t *testing.T) {
	fx := newWriteFixture(t)
	fx.fake.seedItemData("ITEM0001", map[string]any{
		"itemType": "journalArticle",
		"tags":     []any{map[string]any{"tag": "a"}, map[string]any{"tag": "b", "type": float64(1)}},
	})

	out, err := runWrite(t, "--json", "tags", "remove", "ITEM0001", "a", "zzz")
	if err != nil {
		t.Fatalf("tags remove: %v\n%s", err, out)
	}
	res := decodeBulk(t, out)
	if res.Action != "removed" || !slices.Equal(res.Success, []string{"a", "zzz"}) {
		t.Errorf("got %+v", res)
	}
	data, _ := res.Data.(map[string]any)
	if unchanged, _ := data["unchanged"].([]any); len(unchanged) != 1 || unchanged[0] != "zzz" {
		t.Errorf("data.unchanged = %v, want [zzz]", data["unchanged"])
	}
	if got := fx.fake.itemTags("ITEM0001"); !slices.Equal(got, []string{"b"}) {
		t.Errorf("server tags = %v, want the automatic tag b kept verbatim", got)
	}
}

func TestTagsWrite_UsageErrors(t *testing.T) {
	fx := newWriteFixture(t)
	for name, args := range map[string][]string{
		"no tag":            {"tags", "add", "ITEM0001"},
		"nothing at all":    {"tags", "remove"},
		"unresolvable item": {"tags", "add", "not-a-key", "x"},
	} {
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
