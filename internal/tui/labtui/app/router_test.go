package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/lab"
)

// keyPress builds a single-rune key press the way tuitest.SendKey does: both
// Code and Text set, since labtui's key handlers read msg.Text for letters.
func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// TestRouterViewDispatch guards the router.go registration table: each screen
// must route View to its own body, not a neighbor's. A copy-paste slip (e.g.
// screenError → viewDone) would surface here even though the existing teatest
// suite — which only ever renders the *current* screen — might not.
func TestRouterViewDispatch(t *testing.T) {
	m, _ := newTestModel(t)
	m.firstLoadDone = true
	m.entries = []lab.Entry{{Name: "data", IsDir: true}, {Name: "results.csv"}}
	m.queue = []string{"/labs/sciminds/data/results.csv"}

	cases := []struct {
		name   string
		screen screen
		want   string
	}{
		{"browse", screenBrowse, m.viewBrowse()},
		{"confirm", screenConfirm, m.viewConfirm()},
		{"transfer", screenTransfer, m.viewTransfer()},
		{"error", screenError, m.viewError()},
		{"done", screenDone, m.viewDone()},
	}

	// The dispatch check only catches a mis-wired table if the bodies differ;
	// confirm they're pairwise distinct so the test isn't vacuous.
	seen := map[string]string{}
	for _, c := range cases {
		if other, dup := seen[c.want]; dup {
			t.Fatalf("screens %s and %s render identical bodies; dispatch test can't distinguish them", other, c.name)
		}
		seen[c.want] = c.name
	}

	for _, c := range cases {
		if got := router.View(c.screen, m, m.width, m.height); got != c.want {
			t.Errorf("router.View(%s) routed to the wrong screen:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

// TestRouterKeysDispatch confirms router.Keys actually invokes the registered
// per-screen handler, using transitions unique to one handler.
func TestRouterKeysDispatch(t *testing.T) {
	// Down arrow advances the cursor — only keyBrowse does this.
	browse, _ := newTestModel(t)
	browse.entries = []lab.Entry{{Name: "a"}, {Name: "b"}}
	browse.cursor = 0
	router.Keys(screenBrowse, browse, tea.KeyPressMsg{Code: tea.KeyDown})
	if browse.cursor != 1 {
		t.Errorf("router.Keys(browse, down): cursor = %d, want 1 (not routed to keyBrowse?)", browse.cursor)
	}

	// "n" cancels back to browse — only keyConfirm does this.
	confirm, _ := newTestModel(t)
	confirm.screen = screenConfirm
	router.Keys(screenConfirm, confirm, keyPress("n"))
	if confirm.screen != screenBrowse {
		t.Errorf("router.Keys(confirm, n): screen = %v, want screenBrowse (not routed to keyConfirm?)", confirm.screen)
	}
}
