package cli

// Tests for `sci zot item read KEY1 KEY2 ...` — multi-key reads. The
// single-key form used to silently drop every key after the first
// (Args().First()), which made `item read KEY1 KEY2` look like a
// successful read of KEY1. Now N>1 keys return every item, in request
// order, and a missing key fails the whole read naming the stragglers —
// never a silent partial result. The single-key JSON shape (bare item,
// no wrapper) is pinned by TestItemRead_ByPositionalKey_StillWorks.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestItemRead_MultipleKeys_ReturnsAllInOrder(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "KEY3", "KEY1")
	if err != nil {
		t.Fatalf("item read KEY3 KEY1: %v\n%s", err, string(out))
	}
	var result struct {
		Count int          `json:"count"`
		Items []local.Item `json:"items"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &result); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	if result.Count != 2 || len(result.Items) != 2 {
		t.Fatalf("count = %d, len(items) = %d, want 2 and 2\n%s", result.Count, len(result.Items), string(out))
	}
	// Request order, not DB order — KEY3 was asked for first.
	if result.Items[0].Key != "KEY3" || result.Items[1].Key != "KEY1" {
		t.Errorf("keys = [%q, %q], want [KEY3, KEY1] (request order)", result.Items[0].Key, result.Items[1].Key)
	}
	// Multi-key reads are full reads, same as single-key: KEY1 carries
	// tags in the fixture, and a batch that silently downgraded to
	// list-view rows would lose them.
	if len(result.Items[1].Tags) == 0 {
		t.Errorf("KEY1 should carry its tags in a multi-key read, got none")
	}
}

func TestItemRead_MultipleKeys_MissingKeyFailsNamingIt(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "read", "KEY1", "MISSING1")
	if err == nil {
		t.Fatal("expected error when one key of the batch doesn't exist — a partial result would be the same silent drop this feature fixes")
	}
	if !strings.Contains(err.Error(), "MISSING1") {
		t.Errorf("err should name the missing key: %v", err)
	}
}

func TestItemRead_MultipleKeys_AllMissingNamesEvery(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "read", "MISSING1", "MISSING2")
	if err == nil {
		t.Fatal("expected error when every key is missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "MISSING1") || !strings.Contains(msg, "MISSING2") {
		t.Errorf("err should name every missing key: %v", err)
	}
}

func TestItemRead_MultipleKeys_DOIConflictErrors(t *testing.T) {
	withOrientConfig(t)

	_, err := runItemRead(t, "--library", "personal", "item", "read", "KEY1", "KEY2", "--doi", "10.1038/nature12373")
	if err == nil {
		t.Fatal("expected error when key positionals and --doi are both supplied")
	}
	if !strings.Contains(err.Error(), "either") {
		t.Errorf("err should explain the mutex: %v", err)
	}
}
