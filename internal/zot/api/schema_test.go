package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestItemTypeFields_ReturnsFieldNamesInAPIOrder(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.fields["book"] = []string{"title", "abstractNote", "series", "date"}
	c, _ := newTestClient(t, f)

	got, err := c.ItemTypeFields(context.Background(), "book")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"title", "abstractNote", "series", "date"}; !equalStrings(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
	req := f.seen()[0]
	if req.Path != "/itemTypeFields" || req.Query != "itemType=book" {
		t.Errorf("request = %s?%s, want /itemTypeFields?itemType=book (no library prefix)", req.Path, req.Query)
	}
}

func TestItemTypeFields_IsMemoizedPerItemType(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.fields["book"] = []string{"title"}
	f.fields["note"] = []string{"note"}
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	for range 3 {
		if _, err := c.ItemTypeFields(ctx, "book"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.ItemTypeFields(ctx, "note"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /itemTypeFields", "GET /itemTypeFields"}
	if !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want one fetch per item type: %v", f.methods(), want)
	}
}

func TestItemTypeFields_CallerCannotMutateCache(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.fields["book"] = []string{"title", "date"}
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	first, _ := c.ItemTypeFields(ctx, "book")
	first[0] = "corrupted"
	second, _ := c.ItemTypeFields(ctx, "book")
	if second[0] != "title" {
		t.Errorf("cached fields = %v, want the cache untouched by the caller's edit", second)
	}
}

func TestItemTypeFields_UnknownTypeIsHTTPError(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Invalid item type", http.StatusBadRequest)
	})
	c, _ := newTestClient(t, h)

	_, err := c.ItemTypeFields(context.Background(), "nope")
	herr, ok := errors.AsType[*HTTPError](err)
	if !ok || herr.Status != http.StatusBadRequest {
		t.Fatalf("error = %v, want *HTTPError 400", err)
	}
}

func TestItemTypeCreatorTypes_ReturnsCreatorTypesInAPIOrder(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.creatorTypes["book"] = []string{"author", "contributor", "editor"}
	c, _ := newTestClient(t, f)

	got, err := c.ItemTypeCreatorTypes(context.Background(), "book")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"author", "contributor", "editor"}; !equalStrings(got, want) {
		t.Errorf("creator types = %v, want %v", got, want)
	}
	req := f.seen()[0]
	if req.Path != "/itemTypeCreatorTypes" || req.Query != "itemType=book" {
		t.Errorf("request = %s?%s, want /itemTypeCreatorTypes?itemType=book (no library prefix)", req.Path, req.Query)
	}
}

func TestItemTypeCreatorTypes_IsMemoizedSeparatelyFromFields(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.fields["book"] = []string{"title"}
	f.creatorTypes["book"] = []string{"author"}
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	for range 2 {
		if _, err := c.ItemTypeCreatorTypes(ctx, "book"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ItemTypeFields(ctx, "book"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"GET /itemTypeCreatorTypes", "GET /itemTypeFields"}
	if !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want one fetch per endpoint: %v", f.methods(), want)
	}
}
