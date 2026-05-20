package app

// cell_preview.go — Enter-key preview overlay for the focused cell.
// Three rendering paths:
//
//   1. Rich markdown from a [store.NoteContentProvider] (e.g. .md notes
//      attached to a row).
//   2. Lazy fetch of a heavy column's full value via [store.CellFetcher]
//      — for cells whose Value is a server-side placeholder like
//      `<FLOAT[768]>` (see internal/store/duck/heavy.go).
//   3. Fall-back: pretty-printed JSON or plain text from the in-memory
//      cell value.
//
// Errors in the lazy-fetch path fall back to previewing the placeholder
// text and surface a status-bar message — the overlay still opens so
// the user is not left wondering what happened.

import (
	"fmt"

	"github.com/sciminds/cli/internal/store"
	"github.com/sciminds/cli/internal/uikit"
)

// openCellPreview opens the preview overlay for the selected cell. tab
// must be non-nil and c must be a non-empty cell (callers in
// keys_dispatch enforce both).
func (m *Model) openCellPreview(tab *Tab, c *cell) {
	title := ""
	var spec columnSpec
	if tab.ColCursor >= 0 && tab.ColCursor < len(tab.Specs) {
		spec = tab.Specs[tab.ColCursor]
		title = spec.Title
	}
	cursor := tab.Table.Cursor()

	// Seed the overlay query when a row search is active so the preview
	// lands on matched words instead of the top of the value.
	var overlayOpts []uikit.OverlayOption
	if m.search != nil && m.search.Query != "" {
		overlayOpts = append(overlayOpts, uikit.WithInitialQuery(m.search.Query))
	}

	// Path 1: rich note content for the row.
	if ncp, ok := m.store.(store.NoteContentProvider); ok && cursor >= 0 && cursor < len(tab.Rows) {
		if md := ncp.NoteContent(tab.Rows[cursor].RowID); md != "" {
			m.notePreview = &notePreviewState{
				Text:    c.Value,
				Title:   title,
				Overlay: uikit.NewMarkdownOverlay(title, md, m.width, m.height, overlayOpts...),
			}
			return
		}
	}

	// Path 2: heavy column → lazy fetch the real value.
	body := c.Value
	if spec.Heavy {
		if fetched, ok := m.fetchHeavyCellValue(tab, spec, cursor); ok {
			body = fetched
		}
	}

	// Path 3: pretty-printed JSON or plain text.
	var overlay uikit.ScrollableOverlay
	if pretty, isJSON := prettyPrintJSON(body); isJSON {
		md := compoundTypeHeader(spec.DBType) + "```json\n" + pretty + "\n```"
		overlay = uikit.NewMarkdownOverlay(title, md, m.width, m.height, overlayOpts...)
	} else {
		overlay = uikit.NewOverlay(title, body, m.width, m.height, overlayOpts...)
	}
	m.notePreview = &notePreviewState{
		Text:    body,
		Title:   title,
		Overlay: overlay,
	}
}

// fetchHeavyCellValue pulls the full value of a heavy cell via the
// store's CellFetcher. Returns the value and true on success; returns
// "" and false (with a status-bar error) when the store doesn't
// implement the interface or the fetch fails. Callers should fall back
// to the in-memory placeholder so the overlay still opens.
func (m *Model) fetchHeavyCellValue(tab *Tab, spec columnSpec, cursor int) (string, bool) {
	fetcher, ok := m.store.(store.CellFetcher)
	if !ok {
		return "", false
	}
	if cursor < 0 || cursor >= len(tab.Rows) {
		return "", false
	}
	rowID := tab.Rows[cursor].RowID
	value, isNull, err := fetcher.FetchCell(tab.Name, spec.DBName, rowID)
	if err != nil {
		m.setStatusError(fmt.Sprintf("Preview %s: %v", spec.Title, err))
		return "", false
	}
	if isNull {
		return "", true
	}
	return value, true
}
