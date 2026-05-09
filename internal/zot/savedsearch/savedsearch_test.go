package savedsearch

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

// helper: build a SearchCondition.
func cond(field, op, val string) client.SearchCondition {
	return client.SearchCondition{Condition: field, Operator: op, Value: val}
}

func TestTranslate_MissingPDFSavedSearch(t *testing.T) {
	t.Parallel()
	// The user's actual missing-pdf saved search:
	//   tag isNot has-markdown
	//   noChildren true
	conds := []client.SearchCondition{
		cond("tag", "isNot", "has-markdown"),
		cond("noChildren", "true", ""),
	}
	got, unsupported := Translate(conds)
	if len(unsupported) != 0 {
		t.Fatalf("unsupported = %+v, want none", unsupported)
	}
	if got.NotTag != "has-markdown" {
		t.Errorf("NotTag = %q, want has-markdown", got.NotTag)
	}
	if !got.TopOnly {
		t.Error("TopOnly should be true (noChildren=true)")
	}
}

func TestTranslate_AllSupportedConditions(t *testing.T) {
	t.Parallel()
	conds := []client.SearchCondition{
		cond("tag", "is", "ml"),
		cond("tag", "isNot", "draft"),
		cond("itemType", "is", "journalArticle"),
		cond("itemType", "isNot", "attachment"),
		cond("collection", "is", "ABCD1234"),
		cond("noChildren", "true", ""),
		// modifiers we deliberately ignore
		cond("joinMode", "all", ""),
		cond("includeParentsAndChildren", "false", ""),
	}
	got, unsupported := Translate(conds)
	if len(unsupported) != 0 {
		t.Fatalf("unsupported = %+v, want none", unsupported)
	}
	if got.Tag != "ml" || got.NotTag != "draft" {
		t.Errorf("tag/notTag = %q/%q", got.Tag, got.NotTag)
	}
	if got.ItemType != "journalArticle" || got.NotItemType != "attachment" {
		t.Errorf("itemType/notItemType = %q/%q", got.ItemType, got.NotItemType)
	}
	if got.CollectionKey != "ABCD1234" {
		t.Errorf("CollectionKey = %q", got.CollectionKey)
	}
	if !got.TopOnly {
		t.Error("TopOnly should be true")
	}
}

func TestTranslate_UnsupportedCondition(t *testing.T) {
	t.Parallel()
	// title/contains has no API equivalent — record as unsupported and keep
	// translating the rest so callers can decide whether to abort or proceed.
	conds := []client.SearchCondition{
		cond("title", "contains", "neuroscience"),
		cond("tag", "isNot", "draft"),
	}
	got, unsupported := Translate(conds)
	if got.NotTag != "draft" {
		t.Errorf("supported part should still translate; NotTag = %q", got.NotTag)
	}
	if len(unsupported) != 1 {
		t.Fatalf("unsupported = %d, want 1", len(unsupported))
	}
	u := unsupported[0]
	if u.Condition != "title" || u.Operator != "contains" {
		t.Errorf("unsupported = %+v, want title/contains", u)
	}
	if u.Reason == "" {
		t.Error("Unsupported.Reason must be non-empty")
	}
}

func TestTranslate_UnsupportedOperator(t *testing.T) {
	t.Parallel()
	// "tag/contains" exists in Zotero saved-searches but the API only
	// understands literal tag is/isNot — record as unsupported.
	conds := []client.SearchCondition{
		cond("tag", "contains", "ml"),
	}
	_, unsupported := Translate(conds)
	if len(unsupported) != 1 {
		t.Fatalf("unsupported = %d, want 1", len(unsupported))
	}
	if !strings.Contains(strings.ToLower(unsupported[0].Reason), "operator") {
		t.Errorf("Reason should mention operator, got %q", unsupported[0].Reason)
	}
}

func TestTranslate_DuplicateTagFlagsAsUnsupported(t *testing.T) {
	t.Parallel()
	// API supports one `tag=` and one `tag=-` per request (in our wrapper).
	// A second `tag is` clause is recorded as unsupported.
	conds := []client.SearchCondition{
		cond("tag", "is", "ml"),
		cond("tag", "is", "nlp"),
	}
	got, unsupported := Translate(conds)
	if got.Tag != "ml" {
		t.Errorf("first tag should win, got %q", got.Tag)
	}
	if len(unsupported) != 1 {
		t.Fatalf("unsupported = %d, want 1 (extra tag is)", len(unsupported))
	}
}

func TestTranslate_NoChildrenFalseIsIgnored(t *testing.T) {
	t.Parallel()
	// noChildren=false means "include children in results" — for our API
	// surface this is the default (we never recurse into children), so it's
	// effectively a no-op rather than unsupported.
	conds := []client.SearchCondition{
		cond("noChildren", "false", ""),
		cond("tag", "isNot", "draft"),
	}
	got, unsupported := Translate(conds)
	if got.TopOnly {
		t.Error("noChildren=false must not force TopOnly")
	}
	if len(unsupported) != 0 {
		t.Errorf("unsupported = %+v, want none", unsupported)
	}
}

func TestTranslate_EmptyConditions(t *testing.T) {
	t.Parallel()
	got, unsupported := Translate(nil)
	if len(unsupported) != 0 {
		t.Errorf("unsupported = %+v, want none", unsupported)
	}
	if got.Tag != "" || got.TopOnly || got.CollectionKey != "" {
		t.Errorf("zero-input should yield zero filters, got %+v", got)
	}
}

func TestUnsupportedString_HumanReadable(t *testing.T) {
	t.Parallel()
	u := Unsupported{Condition: "title", Operator: "contains", Value: "x", Reason: "no API equivalent"}
	s := u.String()
	for _, want := range []string{"title", "contains", "no API equivalent"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}
