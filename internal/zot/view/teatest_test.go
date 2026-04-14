package view

import (
	"bytes"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
	"github.com/sciminds/cli/internal/zot/local"
)

const (
	teatestTermW = 160
	teatestTermH = 40
	teatestWait  = 5 * time.Second
	teatestFinal = 8 * time.Second
)

// newViewTeatestModel builds a *dbtui.Model driving a read-only view.Store
// over the synthetic fixture. The store owns the local.DB and is closed via
// t.Cleanup — *after* the teatest model tears itself down — so we don't
// yank the connection out from under an in-flight Update.
func newViewTeatestModel(t *testing.T) *dbtui.Model {
	t.Helper()
	dir := seedViewFixture(t)
	ldb, err := local.Open(dir)
	if err != nil {
		t.Fatalf("local.Open: %v", err)
	}
	store := New(ldb, time.UTC)
	t.Cleanup(func() { _ = store.Close() })

	// readOnly=true mirrors what cli/view.go's WithReadOnly does.
	m, err := dbtui.NewModel(store, "zot library", true)
	if err != nil {
		t.Fatalf("dbtui.NewModel: %v", err)
	}
	return m
}

func viewWaitFor(t *testing.T, r io.Reader, substr string) {
	t.Helper()
	teatest.WaitFor(t, r, func(bts []byte) bool {
		return bytes.Contains(bts, []byte(substr))
	}, teatest.WithDuration(teatestWait), teatest.WithCheckInterval(time.Millisecond))
}

func viewQuit(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	_ = tm.FinalModel(t, teatest.WithFinalTimeout(teatestFinal))
}

// TestTeatestViewRendersColumnHeadersAndRows walks the rendered output and
// asserts every expected column header and every row's marquee string (title
// or author) appears. Uses an io.TeeReader so the transcript is kept intact
// across successive WaitFor calls — WaitFor drains tm.Output() in place.
func TestTeatestViewRendersColumnHeadersAndRows(t *testing.T) {
	m := newViewTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(teatestTermW, teatestTermH))

	var transcript bytes.Buffer
	tee := io.TeeReader(tm.Output(), &transcript)

	// Wait for the first row's title to render so we know the initial paint is done.
	viewWaitFor(t, tee, "Transformers")

	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	rest, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(teatestFinal)))
	if err != nil {
		t.Fatal(err)
	}
	transcript.Write(rest)

	got := transcript.Bytes()

	// Column headers (dbtui renders Title in full, but wraps some panel
	// labels — we just want presence).
	headers := []string{"Author(s)", "Year", "Journal/Publication", "Title", "Date Added", "Extra", "Notes"}
	for _, h := range headers {
		if !bytes.Contains(got, []byte(h)) {
			t.Errorf("transcript missing column header %q", h)
		}
	}

	// One distinctive substring per row — we can't assert the full row text
	// because dbtui column widths truncate with "…" when content overflows.
	distinctive := []string{
		"Transformers",      // row 0 title
		"NASA",              // row 1 authors (institutional)
		"Deep Space",        // row 1 title
		"Radium",            // row 2 title (book)
		"Curie",             // row 2 authors
		"Citation Key: xyz", // row 0 extra (wide col, should fit at 160w)
	}
	for _, s := range distinctive {
		if !bytes.Contains(got, []byte(s)) {
			t.Errorf("transcript missing row marker %q", s)
		}
	}

	// Editor from item 10 must not leak — that's the guarantee formatViewAuthor
	// gives us by only joining creatorType='author'. If this fires, the SQL
	// filter drifted.
	if bytes.Contains(got, []byte("Editor, Eve")) {
		t.Error("editor creator leaked into Author(s) column")
	}
}

// TestTeatestViewBlocksEditMode presses 'i' and asserts that the status line
// shows "Read-only (view)". readOnlyReason() returns this exact string when
// the current tab's name is flagged by our ViewLister.IsView implementation,
// so this test also pins that the view.Store is being consulted.
func TestTeatestViewBlocksEditMode(t *testing.T) {
	m := newViewTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(teatestTermW, teatestTermH))

	var transcript bytes.Buffer
	tee := io.TeeReader(tm.Output(), &transcript)
	viewWaitFor(t, tee, "Transformers")

	tm.Send(tea.KeyPressMsg{Code: 'i', Text: "i"})

	viewWaitFor(t, tee, "Read-only (view)")

	viewQuit(t, tm)
}

// TestTeatestViewEnterOpensPreview presses Enter on the initial cell and
// verifies the note-preview overlay renders. The overlay title is the
// column name and the body is the raw cell value, so we match on an
// author substring that would not appear in a plain table row that was
// truncated by dbtui's column width limits.
func TestTeatestViewEnterOpensPreview(t *testing.T) {
	m := newViewTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(teatestTermW, teatestTermH))

	var transcript bytes.Buffer
	tee := io.TeeReader(tm.Output(), &transcript)
	viewWaitFor(t, tee, "Transformers")

	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

	// The overlay has a dedicated "esc close" footer. That hint does not
	// appear anywhere in the base viewport, so its presence is a clean
	// signal that notePreview is active.
	viewWaitFor(t, tee, "esc close")

	viewQuit(t, tm)
}

// TestTeatestViewNotesColumnMarkdownOverlay navigates to the Notes column
// on a row with an "Extracted" indicator and presses Enter. The overlay
// should render the docling note's markdown content via MarkdownOverlay
// (glamour-rendered), not the raw "Extracted" text.
func TestTeatestViewNotesColumnMarkdownOverlay(t *testing.T) {
	m := newViewTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(teatestTermW, teatestTermH))

	var transcript bytes.Buffer
	tee := io.TeeReader(tm.Output(), &transcript)
	viewWaitFor(t, tee, "Transformers")

	// Navigate right to the Notes column (index 6).
	for range 6 {
		tm.Send(tea.KeyPressMsg{Code: 'l', Text: "l"})
	}

	// Press Enter to open the overlay.
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

	// The markdown overlay should render the docling note content — check for
	// the "esc close" hint which signals the overlay is active, and the note
	// content ("bold" from "Some **bold** content." in the fixture).
	viewWaitFor(t, tee, "esc close")

	// Read the rest and verify markdown content rendered.
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	rest, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(teatestFinal)))
	if err != nil {
		t.Fatal(err)
	}
	transcript.Write(rest)
	got := transcript.String()

	// The overlay should contain rendered markdown from the docling note,
	// not the raw "Extracted" indicator text.
	if !bytes.Contains([]byte(got), []byte("bold")) {
		t.Errorf("expected markdown note content in overlay, transcript does not contain 'bold'")
	}
}
