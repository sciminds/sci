package cli

// Unit tests for the pure helpers behind `zot item note read`. The live
// CLI Action is not mocked — it reads through the Zotero Web API, and the
// value here is in the type guard, which is what turns "you read an
// article as a note" into a usage error instead of an empty body.

import (
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot/client"
)

// --- assertNoteType ---

func TestAssertNoteType_acceptsNote(t *testing.T) {
	t.Parallel()
	if err := assertNoteType(string(client.Note)); err != nil {
		t.Errorf("expected note type to be accepted: %v", err)
	}
}

func TestAssertNoteType_rejectsJournalArticle(t *testing.T) {
	t.Parallel()
	err := assertNoteType(string(client.JournalArticle))
	if err == nil {
		t.Fatal("expected error for journalArticle type")
	}
	// Error should point the user at `zot item read` for bibliographic items.
	if !strings.Contains(err.Error(), "item read") {
		t.Errorf("err=%v should mention `item read` as the right command", err)
	}
}

func TestAssertNoteType_rejectsEmpty(t *testing.T) {
	t.Parallel()
	if err := assertNoteType(""); err == nil {
		t.Error("expected error on empty type")
	}
}

// --- buildNoteUpdatePatch ---
